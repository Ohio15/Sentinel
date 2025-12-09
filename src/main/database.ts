import { Pool, PoolConfig, QueryResult, QueryResultRow } from 'pg';
import * as fs from 'fs';
import * as path from 'path';
import { v4 as uuidv4 } from 'uuid';

export interface DatabaseConfig {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
  ssl?: boolean;
  max?: number; // Max pool connections
}

export class Database {
  private pool: Pool | null = null;
  private config: DatabaseConfig;

  constructor(config?: Partial<DatabaseConfig>) {
    // Default configuration - can be overridden by environment variables or passed config
    this.config = {
      host: process.env.DB_HOST || config?.host || 'localhost',
      port: parseInt(process.env.DB_PORT || '') || config?.port || 5432,
      database: process.env.DB_NAME || config?.database || 'sentinel',
      user: process.env.DB_USER || config?.user || 'sentinel',
      password: process.env.DB_PASSWORD || config?.password || 'sentinel_dev_password_32chars!!',
      ssl: process.env.DB_SSL === 'true' || config?.ssl || false,
      max: parseInt(process.env.DB_POOL_MAX || '') || config?.max || 20,
    };
  }

  async initialize(): Promise<void> {
    const poolConfig: PoolConfig = {
      host: this.config.host,
      port: this.config.port,
      database: this.config.database,
      user: this.config.user,
      password: this.config.password,
      max: this.config.max,
      idleTimeoutMillis: 30000,
      connectionTimeoutMillis: 5000,
    };

    if (this.config.ssl) {
      poolConfig.ssl = { rejectUnauthorized: false };
    }

    this.pool = new Pool(poolConfig);

    // Test connection
    try {
      const client = await this.pool.connect();
      console.log('Connected to PostgreSQL database');
      client.release();
    } catch (error) {
      console.error('Failed to connect to PostgreSQL:', error);
      throw error;
    }

    // Run migrations
    await this.runMigrations();
    await this.initializeDefaults();
  }

  private async runMigrations(): Promise<void> {
    if (!this.pool) return;

    // Read and execute migration file
    const migrationsDir = path.join(__dirname, 'migrations');
    const migrationFile = path.join(migrationsDir, '001_initial_schema.sql');

    if (fs.existsSync(migrationFile)) {
      const sql = fs.readFileSync(migrationFile, 'utf-8');
      try {
        await this.pool.query(sql);
        console.log('Database migrations completed');
      } catch (error: any) {
        // Ignore errors for objects that already exist
        if (!error.message.includes('already exists')) {
          console.error('Migration error:', error);
          throw error;
        }
      }
    }
  }

  private async initializeDefaults(): Promise<void> {
    if (!this.pool) return;

    // Generate enrollment token if not exists
    const tokenResult = await this.pool.query(
      "SELECT value FROM settings WHERE key = 'enrollmentToken'"
    );
    if (tokenResult.rows.length === 0) {
      await this.pool.query(
        'INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING',
        ['enrollmentToken', uuidv4()]
      );
    }
  }

  private async query<T extends QueryResultRow = any>(sql: string, params?: any[]): Promise<QueryResult<T>> {
    if (!this.pool) {
      throw new Error('Database not initialized');
    }
    return this.pool.query<T>(sql, params);
  }

  // Device methods
  async getDevices(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        id, agent_id as "agentId", hostname, display_name as "displayName",
        os_type as "osType", os_version as "osVersion", os_build as "osBuild",
        platform, platform_family as "platformFamily", architecture,
        cpu_model as "cpuModel", cpu_cores as "cpuCores", cpu_threads as "cpuThreads",
        cpu_speed as "cpuSpeed", total_memory as "totalMemory", boot_time as "bootTime",
        gpu, storage, serial_number as "serialNumber", manufacturer, model, domain,
        agent_version as "agentVersion", last_seen as "lastSeen",
        status, ip_address as "ipAddress", public_ip as "publicIp", mac_address as "macAddress",
        tags, metadata, created_at as "createdAt", updated_at as "updatedAt"
      FROM devices
      ORDER BY hostname
    `);
    return result.rows;
  }

  async getDevice(id: string): Promise<any | null> {
    const result = await this.query(
      `
      SELECT
        id, agent_id as "agentId", hostname, display_name as "displayName",
        os_type as "osType", os_version as "osVersion", os_build as "osBuild",
        platform, platform_family as "platformFamily", architecture,
        cpu_model as "cpuModel", cpu_cores as "cpuCores", cpu_threads as "cpuThreads",
        cpu_speed as "cpuSpeed", total_memory as "totalMemory", boot_time as "bootTime",
        gpu, storage, serial_number as "serialNumber", manufacturer, model, domain,
        agent_version as "agentVersion", last_seen as "lastSeen",
        status, ip_address as "ipAddress", public_ip as "publicIp", mac_address as "macAddress",
        tags, metadata, created_at as "createdAt", updated_at as "updatedAt"
      FROM devices WHERE id = $1
    `,
      [id]
    );
    return result.rows[0] || null;
  }

  async getDeviceByAgentId(agentId: string): Promise<any | null> {
    const result = await this.query(
      `SELECT * FROM devices WHERE agent_id = $1`,
      [agentId]
    );
    return result.rows[0] || null;
  }

  async createOrUpdateDevice(device: any): Promise<any> {
    const existing = await this.getDeviceByAgentId(device.agentId);
    const now = new Date().toISOString();

    if (existing) {
      await this.query(
        `
        UPDATE devices SET
          hostname = $1, display_name = $2, os_type = $3, os_version = $4,
          os_build = $5, architecture = $6, agent_version = $7, last_seen = $8,
          status = $9, ip_address = $10, mac_address = $11, metadata = $12
        WHERE agent_id = $13
      `,
        [
          device.hostname,
          device.displayName || device.hostname,
          device.osType,
          device.osVersion,
          device.osBuild,
          device.architecture,
          device.agentVersion,
          now,
          'online',
          device.ipAddress,
          device.macAddress,
          JSON.stringify(device.metadata || {}),
          device.agentId,
        ]
      );
      return this.getDeviceByAgentId(device.agentId);
    } else {
      const id = uuidv4();
      await this.query(
        `
        INSERT INTO devices (
          id, agent_id, hostname, display_name, os_type, os_version,
          os_build, architecture, agent_version, last_seen, status,
          ip_address, mac_address, tags, metadata
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
      `,
        [
          id,
          device.agentId,
          device.hostname,
          device.displayName || device.hostname,
          device.osType,
          device.osVersion,
          device.osBuild,
          device.architecture,
          device.agentVersion,
          now,
          'online',
          device.ipAddress,
          device.macAddress,
          JSON.stringify(device.tags || []),
          JSON.stringify(device.metadata || {}),
        ]
      );
      return this.getDevice(id);
    }
  }

  async updateDeviceStatus(agentId: string, status: string): Promise<void> {
    await this.query(
      `UPDATE devices SET status = $1 WHERE agent_id = $2`,
      [status, agentId]
    );
  }

  async updateDeviceLastSeen(agentId: string): Promise<void> {
    await this.query(
      `UPDATE devices SET last_seen = CURRENT_TIMESTAMP, status = 'online' WHERE agent_id = $1`,
      [agentId]
    );
  }

  
  async updateDevice(id: string, updates: { displayName?: string; tags?: string[] }): Promise<any | null> {
    const fields: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (updates.displayName !== undefined) {
      fields.push(`display_name = ${paramIndex++}`);
      values.push(updates.displayName);
    }
    if (updates.tags !== undefined) {
      fields.push(`tags = ${paramIndex++}`);
      values.push(JSON.stringify(updates.tags));
    }

    if (fields.length === 0) {
      return this.getDevice(id);
    }

    fields.push(`updated_at = CURRENT_TIMESTAMP`);
    values.push(id);

    await this.query(
      `UPDATE devices SET ${fields.join(', ')} WHERE id = ${paramIndex}`,
      values
    );
    return this.getDevice(id);
  }

  async deleteDevice(id: string): Promise<void> {
    await this.query('DELETE FROM devices WHERE id = $1', [id]);
  }

  // Metrics methods
  async insertMetrics(deviceId: string, metrics: any): Promise<void> {
    await this.query(
      `
      INSERT INTO device_metrics (
        device_id, cpu_percent, memory_percent, memory_used_bytes,
        disk_percent, disk_used_bytes, network_rx_bytes, network_tx_bytes, process_count
      ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `,
      [
        deviceId,
        metrics.cpuPercent,
        metrics.memoryPercent,
        metrics.memoryUsedBytes,
        metrics.diskPercent,
        metrics.diskUsedBytes,
        metrics.networkRxBytes,
        metrics.networkTxBytes,
        metrics.processCount,
      ]
    );
  }

  async getDeviceMetrics(deviceId: string, hours: number): Promise<any[]> {
    const result = await this.query(
      `
      SELECT
        timestamp, cpu_percent as "cpuPercent", memory_percent as "memoryPercent",
        memory_used_bytes as "memoryUsedBytes", disk_percent as "diskPercent",
        disk_used_bytes as "diskUsedBytes", network_rx_bytes as "networkRxBytes",
        network_tx_bytes as "networkTxBytes", process_count as "processCount"
      FROM device_metrics
      WHERE device_id = $1 AND timestamp > NOW() - INTERVAL '1 hour' * $2
      ORDER BY timestamp DESC
    `,
      [deviceId, hours]
    );
    return result.rows;
  }

  async cleanOldMetrics(retentionDays: number): Promise<void> {
    await this.query(
      `DELETE FROM device_metrics WHERE timestamp < NOW() - INTERVAL '1 day' * $1`,
      [retentionDays]
    );
  }

  // Command methods
  async createCommand(deviceId: string, command: string, type: string): Promise<any> {
    const id = uuidv4();
    await this.query(
      `INSERT INTO commands (id, device_id, command_type, command) VALUES ($1, $2, $3, $4)`,
      [id, deviceId, type, command]
    );
    return this.getCommand(id);
  }

  async getCommand(id: string): Promise<any | null> {
    const result = await this.query(
      `
      SELECT
        id, device_id as "deviceId", command_type as "commandType", command,
        status, output, error_message as "errorMessage",
        created_at as "createdAt", started_at as "startedAt", completed_at as "completedAt"
      FROM commands WHERE id = $1
    `,
      [id]
    );
    return result.rows[0] || null;
  }

  async updateCommandStatus(
    id: string,
    status: string,
    output?: string,
    error?: string
  ): Promise<void> {
    const updates: string[] = ['status = $1'];
    const values: any[] = [status];
    let paramIndex = 2;

    if (status === 'running') {
      updates.push('started_at = CURRENT_TIMESTAMP');
    }
    if (status === 'completed' || status === 'failed') {
      updates.push('completed_at = CURRENT_TIMESTAMP');
    }
    if (output !== undefined) {
      updates.push(`output = $${paramIndex}`);
      values.push(output);
      paramIndex++;
    }
    if (error !== undefined) {
      updates.push(`error_message = $${paramIndex}`);
      values.push(error);
      paramIndex++;
    }
    values.push(id);

    await this.query(
      `UPDATE commands SET ${updates.join(', ')} WHERE id = $${paramIndex}`,
      values
    );
  }

  async getCommandHistory(deviceId: string, limit: number = 100): Promise<any[]> {
    const result = await this.query(
      `
      SELECT
        id, device_id as "deviceId", command_type as "commandType", command,
        status, output, error_message as "errorMessage",
        created_at as "createdAt", started_at as "startedAt", completed_at as "completedAt"
      FROM commands
      WHERE device_id = $1
      ORDER BY created_at DESC
      LIMIT $2
    `,
      [deviceId, limit]
    );
    return result.rows;
  }

  // Alert methods
  async createAlert(alert: any): Promise<any> {
    const id = uuidv4();
    await this.query(
      `
      INSERT INTO alerts (id, device_id, rule_id, severity, title, message)
      VALUES ($1, $2, $3, $4, $5, $6)
    `,
      [id, alert.deviceId, alert.ruleId, alert.severity, alert.title, alert.message]
    );
    return this.getAlert(id);
  }

  async getAlert(id: string): Promise<any | null> {
    const result = await this.query(
      `
      SELECT
        a.id, a.device_id as "deviceId", d.hostname as "deviceName",
        a.rule_id as "ruleId", a.severity, a.title, a.message,
        a.status, a.acknowledged_at as "acknowledgedAt", a.resolved_at as "resolvedAt",
        a.created_at as "createdAt"
      FROM alerts a
      LEFT JOIN devices d ON a.device_id = d.id
      WHERE a.id = $1
    `,
      [id]
    );
    return result.rows[0] || null;
  }

  async getAlerts(status?: string): Promise<any[]> {
    let sql = `
      SELECT
        a.id, a.device_id as "deviceId", d.hostname as "deviceName",
        a.rule_id as "ruleId", a.severity, a.title, a.message,
        a.status, a.acknowledged_at as "acknowledgedAt", a.resolved_at as "resolvedAt",
        a.created_at as "createdAt"
      FROM alerts a
      LEFT JOIN devices d ON a.device_id = d.id
    `;
    const values: any[] = [];

    if (status) {
      sql += ' WHERE a.status = $1';
      values.push(status);
    }
    sql += ' ORDER BY a.created_at DESC';

    const result = await this.query(sql, values);
    return result.rows;
  }

  async acknowledgeAlert(id: string): Promise<void> {
    await this.query(
      `UPDATE alerts SET status = 'acknowledged', acknowledged_at = CURRENT_TIMESTAMP WHERE id = $1`,
      [id]
    );
  }

  async resolveAlert(id: string): Promise<void> {
    await this.query(
      `UPDATE alerts SET status = 'resolved', resolved_at = CURRENT_TIMESTAMP WHERE id = $1`,
      [id]
    );
  }

  // Alert rules methods
  async getAlertRules(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        id, name, description, enabled, metric, operator, threshold,
        severity, cooldown_minutes as "cooldownMinutes", created_at as "createdAt"
      FROM alert_rules
      ORDER BY name
    `);
    return result.rows;
  }

  async createAlertRule(rule: any): Promise<any> {
    const id = uuidv4();
    await this.query(
      `
      INSERT INTO alert_rules (id, name, description, enabled, metric, operator, threshold, severity, cooldown_minutes)
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `,
      [
        id,
        rule.name,
        rule.description,
        rule.enabled ?? true,
        rule.metric,
        rule.operator,
        rule.threshold,
        rule.severity,
        rule.cooldownMinutes || 15,
      ]
    );
    return this.getAlertRule(id);
  }

  async getAlertRule(id: string): Promise<any | null> {
    const result = await this.query(
      `
      SELECT
        id, name, description, enabled, metric, operator, threshold,
        severity, cooldown_minutes as "cooldownMinutes", created_at as "createdAt"
      FROM alert_rules WHERE id = $1
    `,
      [id]
    );
    return result.rows[0] || null;
  }

  async updateAlertRule(id: string, rule: any): Promise<any> {
    const updates: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (rule.name !== undefined) {
      updates.push(`name = $${paramIndex++}`);
      values.push(rule.name);
    }
    if (rule.description !== undefined) {
      updates.push(`description = $${paramIndex++}`);
      values.push(rule.description);
    }
    if (rule.enabled !== undefined) {
      updates.push(`enabled = $${paramIndex++}`);
      values.push(rule.enabled);
    }
    if (rule.metric !== undefined) {
      updates.push(`metric = $${paramIndex++}`);
      values.push(rule.metric);
    }
    if (rule.operator !== undefined) {
      updates.push(`operator = $${paramIndex++}`);
      values.push(rule.operator);
    }
    if (rule.threshold !== undefined) {
      updates.push(`threshold = $${paramIndex++}`);
      values.push(rule.threshold);
    }
    if (rule.severity !== undefined) {
      updates.push(`severity = $${paramIndex++}`);
      values.push(rule.severity);
    }
    if (rule.cooldownMinutes !== undefined) {
      updates.push(`cooldown_minutes = $${paramIndex++}`);
      values.push(rule.cooldownMinutes);
    }

    if (updates.length > 0) {
      values.push(id);
      await this.query(
        `UPDATE alert_rules SET ${updates.join(', ')} WHERE id = $${paramIndex}`,
        values
      );
    }
    return this.getAlertRule(id);
  }

  async deleteAlertRule(id: string): Promise<void> {
    await this.query('DELETE FROM alert_rules WHERE id = $1', [id]);
  }

  // Scripts methods
  async getScripts(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        id, name, description, language, content,
        os_types as "osTypes", created_at as "createdAt", updated_at as "updatedAt"
      FROM scripts
      ORDER BY name
    `);
    return result.rows;
  }

  async getScript(id: string): Promise<any | null> {
    const result = await this.query(
      `
      SELECT
        id, name, description, language, content,
        os_types as "osTypes", created_at as "createdAt", updated_at as "updatedAt"
      FROM scripts WHERE id = $1
    `,
      [id]
    );
    return result.rows[0] || null;
  }

  async createScript(script: any): Promise<any> {
    const id = uuidv4();
    await this.query(
      `
      INSERT INTO scripts (id, name, description, language, content, os_types)
      VALUES ($1, $2, $3, $4, $5, $6)
    `,
      [
        id,
        script.name,
        script.description,
        script.language,
        script.content,
        JSON.stringify(script.osTypes || []),
      ]
    );
    return this.getScript(id);
  }

  async updateScript(id: string, script: any): Promise<any> {
    const updates: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (script.name !== undefined) {
      updates.push(`name = $${paramIndex++}`);
      values.push(script.name);
    }
    if (script.description !== undefined) {
      updates.push(`description = $${paramIndex++}`);
      values.push(script.description);
    }
    if (script.language !== undefined) {
      updates.push(`language = $${paramIndex++}`);
      values.push(script.language);
    }
    if (script.content !== undefined) {
      updates.push(`content = $${paramIndex++}`);
      values.push(script.content);
    }
    if (script.osTypes !== undefined) {
      updates.push(`os_types = $${paramIndex++}`);
      values.push(JSON.stringify(script.osTypes));
    }

    if (updates.length > 0) {
      values.push(id);
      await this.query(
        `UPDATE scripts SET ${updates.join(', ')} WHERE id = $${paramIndex}`,
        values
      );
    }
    return this.getScript(id);
  }

  async deleteScript(id: string): Promise<void> {
    await this.query('DELETE FROM scripts WHERE id = $1', [id]);
  }

  // Settings methods
  async getSettings(): Promise<any> {
    const result = await this.query('SELECT key, value FROM settings');
    const settings: any = {};
    for (const row of result.rows) {
      // Parse numeric values
      if (['serverPort', 'agentCheckInterval', 'metricsRetentionDays'].includes(row.key)) {
        settings[row.key] = parseInt(row.value, 10);
      } else if (row.key === 'alertEmailEnabled') {
        settings[row.key] = row.value === 'true';
      } else {
        settings[row.key] = row.value;
      }
    }
    return settings;
  }

  async updateSettings(settings: any): Promise<any> {
    for (const [key, value] of Object.entries(settings)) {
      await this.query(
        'INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = $2',
        [key, String(value)]
      );
    }
    return this.getSettings();
  }

  async getSetting(key: string): Promise<string | null> {
    const result = await this.query(
      'SELECT value FROM settings WHERE key = $1',
      [key]
    );
    return result.rows[0]?.value || null;
  }

  async close(): Promise<void> {
    if (this.pool) {
      await this.pool.end();
      this.pool = null;
      console.log('Database connection pool closed');
    }
  }

  // Health check method
  async healthCheck(): Promise<boolean> {
    try {
      await this.query('SELECT 1');
      return true;
    } catch {
      return false;
    }
  }
}
