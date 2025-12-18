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
    const password = process.env.DB_PASSWORD || config?.password;

    if (!password) {
      throw new Error(
        'Database password is required. Please set DB_PASSWORD environment variable.\n' +
        'For production: Set a strong password in your environment configuration.\n' +
        'For development: Create a .env file with DB_PASSWORD=your_secure_password'
      );
    }

    this.config = {
      host: process.env.DB_HOST || config?.host || 'localhost',
      port: parseInt(process.env.DB_PORT || '') || config?.port || 5432,
      database: process.env.DB_NAME || config?.database || 'sentinel',
      user: process.env.DB_USER || config?.user || 'sentinel',
      password: password,
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
      const sslConfig: any = { rejectUnauthorized: true };

      // Load CA certificate if provided
      const caCertPath = process.env.DB_CA_CERT;
      if (caCertPath) {
        try {
          sslConfig.ca = fs.readFileSync(caCertPath, 'utf-8');
          console.log('SSL CA certificate loaded from:', caCertPath);
        } catch (error) {
          console.error('Failed to load CA certificate:', error);
          throw new Error(`Failed to load CA certificate from ${caCertPath}: ${error}`);
        }
      }

      poolConfig.ssl = sslConfig;
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

    const migrationsDir = path.join(__dirname, 'migrations');

    // Get all migration files and sort them
    const migrationFiles = fs.readdirSync(migrationsDir)
      .filter((f: string) => f.endsWith('.sql'))
      .sort();

    for (const file of migrationFiles) {
      const migrationFile = path.join(migrationsDir, file);
      const sql = fs.readFileSync(migrationFile, 'utf-8');
      try {
        await this.pool.query(sql);
        console.log('Migration ' + file + ' completed');
      } catch (error: any) {
        // Ignore errors for objects that already exist
        if (!error.message.includes('already exists') &&
            !error.message.includes('duplicate key')) {
          console.error('Migration ' + file + ' error:', error);
          throw error;
        }
      }
    }
    console.log('All database migrations completed');
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
  async getDevices(clientId?: string): Promise<any[]> {
    let sql = `
      SELECT
        id, agent_id as "agentId", hostname, display_name as "displayName",
        os_type as "osType", os_version as "osVersion", os_build as "osBuild",
        platform, platform_family as "platformFamily", architecture,
        cpu_model as "cpuModel", cpu_cores as "cpuCores", cpu_threads as "cpuThreads",
        cpu_speed as "cpuSpeed", total_memory as "totalMemory", boot_time as "bootTime",
        gpu, storage, serial_number as "serialNumber", manufacturer, model, domain,
        agent_version as "agentVersion", last_seen as "lastSeen",
        status, ip_address as "ipAddress", public_ip as "publicIp", mac_address as "macAddress",
        tags, metadata, client_id as "clientId", created_at as "createdAt", updated_at as "updatedAt"
      FROM devices
    `;
    const values: any[] = [];

    if (clientId) {
      sql += ' WHERE client_id = $1';
      values.push(clientId);
    }

    sql += ' ORDER BY hostname';

    const result = await this.query(sql, values);
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
        tags, metadata, client_id as "clientId", created_at as "createdAt", updated_at as "updatedAt"
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
          status = $9, ip_address = $10, mac_address = $11, metadata = $12,
          platform = $14, platform_family = $15, cpu_model = $16, cpu_cores = $17,
          cpu_threads = $18, cpu_speed = $19, total_memory = $20, boot_time = $21,
          gpu = $22, storage = $23, serial_number = $24, manufacturer = $25,
          model = $26, domain = $27
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
          device.platform || null,
          device.platformFamily || null,
          device.cpuModel || null,
          device.cpuCores || null,
          device.cpuThreads || null,
          device.cpuSpeed || null,
          device.totalMemory || null,
          device.bootTime || null,
          device.gpu ? JSON.stringify(device.gpu) : null,
          device.storage ? JSON.stringify(device.storage) : null,
          device.serialNumber || null,
          device.manufacturer || null,
          device.model || null,
          device.domain || null,
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
          ip_address, mac_address, tags, metadata, platform, platform_family,
          cpu_model, cpu_cores, cpu_threads, cpu_speed, total_memory, boot_time,
          gpu, storage, serial_number, manufacturer, model, domain
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
                  $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
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
          device.platform || null,
          device.platformFamily || null,
          device.cpuModel || null,
          device.cpuCores || null,
          device.cpuThreads || null,
          device.cpuSpeed || null,
          device.totalMemory || null,
          device.bootTime || null,
          device.gpu ? JSON.stringify(device.gpu) : null,
          device.storage ? JSON.stringify(device.storage) : null,
          device.serialNumber || null,
          device.manufacturer || null,
          device.model || null,
          device.domain || null,
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
      fields.push(`display_name = $${paramIndex++}`);
      values.push(updates.displayName);
    }
    if (updates.tags !== undefined) {
      fields.push(`tags = $${paramIndex++}`);
      values.push(JSON.stringify(updates.tags));
    }

    if (fields.length === 0) {
      return this.getDevice(id);
    }

    fields.push(`updated_at = CURRENT_TIMESTAMP`);
    values.push(id);

    await this.query(
      `UPDATE devices SET ${fields.join(', ')} WHERE id = $${paramIndex}`,
      values
    );
    return this.getDevice(id);
  }

  async deleteDevice(id: string): Promise<void> {
    await this.query('DELETE FROM devices WHERE id = $1', [id]);
  }

  // Metrics methods
  async insertMetrics(deviceId: string, metrics: any): Promise<void> {
    // Support both snake_case (from agent) and camelCase field names
    await this.query(
      `
      INSERT INTO device_metrics (
        device_id, cpu_percent, memory_percent, memory_used_bytes,
        disk_percent, disk_used_bytes, network_rx_bytes, network_tx_bytes, process_count,
        uptime_seconds, disk_read_bytes_sec, disk_write_bytes_sec,
        memory_committed, memory_cached, memory_paged_pool, memory_non_paged_pool,
        gpu_metrics, network_interfaces
      ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
    `,
      [
        deviceId,
        metrics.cpu_percent ?? metrics.cpuPercent ?? 0,
        metrics.memory_percent ?? metrics.memoryPercent ?? 0,
        metrics.memory_used ?? metrics.memoryUsed ?? metrics.memoryUsedBytes ?? 0,
        metrics.disk_percent ?? metrics.diskPercent ?? 0,
        metrics.disk_used ?? metrics.diskUsed ?? metrics.diskUsedBytes ?? 0,
        metrics.network_rx_bytes ?? metrics.networkRxBytes ?? 0,
        metrics.network_tx_bytes ?? metrics.networkTxBytes ?? 0,
        metrics.process_count ?? metrics.processCount ?? 0,
        metrics.uptime ?? 0,
        metrics.disk_read_bytes_sec ?? metrics.diskReadBytesPerSec ?? 0,
        metrics.disk_write_bytes_sec ?? metrics.diskWriteBytesPerSec ?? 0,
        metrics.memory_committed ?? metrics.memoryCommitted ?? 0,
        metrics.memory_cached ?? metrics.memoryCached ?? 0,
        metrics.memory_paged_pool ?? metrics.memoryPagedPool ?? 0,
        metrics.memory_non_paged_pool ?? metrics.memoryNonPagedPool ?? 0,
        JSON.stringify(metrics.gpu_metrics ?? metrics.gpuMetrics ?? []),
        JSON.stringify(metrics.network_interfaces ?? metrics.networkInterfaces ?? []),
      ]
    );

    // Update storage if provided in metrics
    if (metrics.storage) {
      await this.updateDeviceStorage(deviceId, metrics.storage);
    }
  }

  async updateDeviceStorage(deviceId: string, storage: any[]): Promise<void> {
    await this.query(
      "UPDATE devices SET storage = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
      [JSON.stringify(storage), deviceId]
    );
  }

  async getDeviceMetrics(deviceId: string, hours: number): Promise<any[]> {
    const result = await this.query(
      `
      SELECT
        timestamp, cpu_percent as "cpuPercent", memory_percent as "memoryPercent",
        memory_used_bytes as "memoryUsedBytes", disk_percent as "diskPercent",
        disk_used_bytes as "diskUsedBytes", network_rx_bytes as "networkRxBytes",
        network_tx_bytes as "networkTxBytes", process_count as "processCount",
        uptime_seconds as "uptime",
        disk_read_bytes_sec as "diskReadBytesPerSec",
        disk_write_bytes_sec as "diskWriteBytesPerSec",
        memory_committed as "memoryCommitted",
        memory_cached as "memoryCached",
        memory_paged_pool as "memoryPagedPool",
        memory_non_paged_pool as "memoryNonPagedPool",
        gpu_metrics as "gpuMetrics",
        network_interfaces as "networkInterfaces"
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


  // Ticket methods
  async getTickets(filters?: { status?: string; priority?: string; assignedTo?: string; deviceId?: string }): Promise<any[]> {
    let sql = `
      SELECT
        t.id, t.ticket_number as "ticketNumber", t.subject, t.description,
        t.status, t.priority, t.type, t.device_id as "deviceId",
        d.hostname as "deviceName", d.display_name as "deviceDisplayName",
        t.requester_name as "requesterName", t.requester_email as "requesterEmail",
        t.assigned_to as "assignedTo", t.tags, t.due_date as "dueDate",
        t.resolved_at as "resolvedAt", t.closed_at as "closedAt",
        t.created_at as "createdAt", t.updated_at as "updatedAt"
      FROM tickets t
      LEFT JOIN devices d ON t.device_id = d.id
      WHERE 1=1
    `;
    const values: any[] = [];
    let paramIndex = 1;

    if (filters?.status) {
      sql += ` AND t.status = $${paramIndex++}`;
      values.push(filters.status);
    }
    if (filters?.priority) {
      sql += ` AND t.priority = $${paramIndex++}`;
      values.push(filters.priority);
    }
    if (filters?.assignedTo) {
      sql += ` AND t.assigned_to = $${paramIndex++}`;
      values.push(filters.assignedTo);
    }
    if (filters?.deviceId) {
      sql += ` AND t.device_id = $${paramIndex++}`;
      values.push(filters.deviceId);
    }

    sql += ' ORDER BY t.created_at DESC';
    const result = await this.query(sql, values);
    return result.rows;
  }

  async getTicket(id: string): Promise<any | null> {
    const result = await this.query(
      `
      SELECT
        t.id, t.ticket_number as "ticketNumber", t.subject, t.description,
        t.status, t.priority, t.type, t.device_id as "deviceId",
        t.device_name as "userDeviceName",
        d.hostname as "deviceName", d.display_name as "deviceDisplayName",
        t.requester_name as "requesterName", t.requester_email as "requesterEmail",
        t.submitter_name as "submitterName", t.submitter_email as "submitterEmail",
        t.assigned_to as "assignedTo", t.tags, t.due_date as "dueDate",
        t.resolved_at as "resolvedAt", t.closed_at as "closedAt",
        t.created_at as "createdAt", t.updated_at as "updatedAt",
        t.source, t.client_id as "clientId"
      FROM tickets t
      LEFT JOIN devices d ON t.device_id = d.id
      WHERE t.id = $1
    `,
      [id]
    );
    return result.rows[0] || null;
  }

  async createTicket(ticket: any): Promise<any> {
    const id = uuidv4();
    await this.query(
      `
      INSERT INTO tickets (
        id, subject, description, status, priority, type, device_id,
        requester_name, requester_email, assigned_to, tags, due_date
      ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
    `,
      [
        id,
        ticket.subject,
        ticket.description,
        ticket.status || 'open',
        ticket.priority || 'medium',
        ticket.type || 'incident',
        ticket.deviceId || null,
        ticket.requesterName,
        ticket.requesterEmail,
        ticket.assignedTo,
        JSON.stringify(ticket.tags || []),
        ticket.dueDate || null,
      ]
    );

    // Log activity
    await this.createTicketActivity(id, 'created', null, null, null, ticket.requesterName || 'System');

    return this.getTicket(id);
  }

  async updateTicket(id: string, updates: any): Promise<any> {
    const currentTicket = await this.getTicket(id);
    if (!currentTicket) return null;

    const fields: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    const fieldMap: { [key: string]: string } = {
      subject: 'subject',
      description: 'description',
      status: 'status',
      priority: 'priority',
      type: 'type',
      deviceId: 'device_id',
      requesterName: 'requester_name',
      requesterEmail: 'requester_email',
      assignedTo: 'assigned_to',
      dueDate: 'due_date',
    };

    for (const [jsField, dbField] of Object.entries(fieldMap)) {
      if (updates[jsField] !== undefined) {
        fields.push(`${dbField} = $${paramIndex++}`);
        values.push(updates[jsField]);

        // Log activity for important field changes
        if (['status', 'priority', 'assignedTo'].includes(jsField)) {
          const oldVal = currentTicket[jsField];
          const newVal = updates[jsField];
          if (oldVal !== newVal) {
            await this.createTicketActivity(id, 'field_changed', jsField, oldVal, newVal, updates.actorName || 'System');
          }
        }
      }
    }

    if (updates.tags !== undefined) {
      fields.push(`tags = $${paramIndex++}`);
      values.push(JSON.stringify(updates.tags));
    }

    // Handle status changes
    if (updates.status === 'resolved' && currentTicket.status !== 'resolved') {
      fields.push(`resolved_at = CURRENT_TIMESTAMP`);
    }
    if (updates.status === 'closed' && currentTicket.status !== 'closed') {
      fields.push(`closed_at = CURRENT_TIMESTAMP`);
    }

    if (fields.length > 0) {
      values.push(id);
      await this.query(
        `UPDATE tickets SET ${fields.join(', ')} WHERE id = $${paramIndex}`,
        values
      );
    }
    return this.getTicket(id);
  }

  async deleteTicket(id: string): Promise<void> {
    await this.query('DELETE FROM tickets WHERE id = $1', [id]);
  }

  // Ticket comments
  async getTicketComments(ticketId: string): Promise<any[]> {
    const result = await this.query(
      `
      SELECT
        id, ticket_id as "ticketId", content, is_internal as "isInternal",
        author_name as "authorName", author_email as "authorEmail",
        attachments, created_at as "createdAt"
      FROM ticket_comments
      WHERE ticket_id = $1
      ORDER BY created_at ASC
    `,
      [ticketId]
    );
    return result.rows;
  }

  async createTicketComment(comment: any): Promise<any> {
    const id = uuidv4();
    await this.query(
      `
      INSERT INTO ticket_comments (id, ticket_id, content, is_internal, author_name, author_email, attachments)
      VALUES ($1, $2, $3, $4, $5, $6, $7)
    `,
      [
        id,
        comment.ticketId,
        comment.content,
        comment.isInternal || false,
        comment.authorName,
        comment.authorEmail,
        JSON.stringify(comment.attachments || []),
      ]
    );

    // Log activity
    await this.createTicketActivity(
      comment.ticketId,
      comment.isInternal ? 'internal_note_added' : 'comment_added',
      null, null, null,
      comment.authorName
    );

    const result = await this.query('SELECT id, ticket_id as "ticketId", content, is_internal as "isInternal", author_name as "authorName", author_email as "authorEmail", attachments, created_at as "createdAt" FROM ticket_comments WHERE id = $1', [id]);
    return result.rows[0];
  }

  // Ticket activity
  async getTicketActivity(ticketId: string): Promise<any[]> {
    const result = await this.query(
      `
      SELECT
        id, ticket_id as "ticketId", action, field_name as "fieldName",
        old_value as "oldValue", new_value as "newValue",
        actor_name as "actorName", created_at as "createdAt"
      FROM ticket_activity
      WHERE ticket_id = $1
      ORDER BY created_at DESC
    `,
      [ticketId]
    );
    return result.rows;
  }

  async createTicketActivity(
    ticketId: string,
    action: string,
    fieldName: string | null,
    oldValue: string | null,
    newValue: string | null,
    actorName: string
  ): Promise<void> {
    const id = uuidv4();
    await this.query(
      `INSERT INTO ticket_activity (id, ticket_id, action, field_name, old_value, new_value, actor_name) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
      [id, ticketId, action, fieldName, oldValue, newValue, actorName]
    );
  }

  // Ticket templates
  async getTicketTemplates(): Promise<any[]> {
    const result = await this.query(`
      SELECT id, name, subject, content, is_active as "isActive", created_at as "createdAt", updated_at as "updatedAt"
      FROM ticket_templates
      WHERE is_active = TRUE
      ORDER BY name
    `);
    return result.rows;
  }

  async createTicketTemplate(template: any): Promise<any> {
    const id = uuidv4();
    await this.query(
      `INSERT INTO ticket_templates (id, name, subject, content) VALUES ($1, $2, $3, $4)`,
      [id, template.name, template.subject, template.content]
    );
    const result = await this.query('SELECT id, name, subject, content, is_active as "isActive", created_at as "createdAt", updated_at as "updatedAt" FROM ticket_templates WHERE id = $1', [id]);
    return result.rows[0];
  }

  async updateTicketTemplate(id: string, template: any): Promise<any> {
    const updates: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (template.name !== undefined) {
      updates.push(`name = $${paramIndex++}`);
      values.push(template.name);
    }
    if (template.subject !== undefined) {
      updates.push(`subject = $${paramIndex++}`);
      values.push(template.subject);
    }
    if (template.content !== undefined) {
      updates.push(`content = $${paramIndex++}`);
      values.push(template.content);
    }
    if (template.isActive !== undefined) {
      updates.push(`is_active = $${paramIndex++}`);
      values.push(template.isActive);
    }

    if (updates.length > 0) {
      values.push(id);
      await this.query(
        `UPDATE ticket_templates SET ${updates.join(', ')} WHERE id = $${paramIndex}`,
        values
      );
    }
    const result = await this.query('SELECT id, name, subject, content, is_active as "isActive", created_at as "createdAt", updated_at as "updatedAt" FROM ticket_templates WHERE id = $1', [id]);
    return result.rows[0];
  }

  async deleteTicketTemplate(id: string): Promise<void> {
    await this.query('DELETE FROM ticket_templates WHERE id = $1', [id]);
  }

  // Get ticket counts for dashboard
  async getTicketStats(): Promise<any> {
    const result = await this.query(`
      SELECT
        COUNT(*) FILTER (WHERE status = 'open') as "openCount",
        COUNT(*) FILTER (WHERE status = 'in_progress') as "inProgressCount",
        COUNT(*) FILTER (WHERE status = 'waiting') as "waitingCount",
        COUNT(*) FILTER (WHERE status = 'resolved') as "resolvedCount",
        COUNT(*) FILTER (WHERE status = 'closed') as "closedCount",
        COUNT(*) as "totalCount"
      FROM tickets
    `);
    return result.rows[0];
  }


  // ============================================================================
  // AGENT UPDATE METHODS
  // ============================================================================

  // Get the latest agent release version
  async getLatestAgentRelease(platform?: string): Promise<any | null> {
    let sql = `
      SELECT version, release_date as "releaseDate", changelog, is_required as "isRequired",
             platforms, min_version as "minVersion", created_at as "createdAt"
      FROM agent_releases
    `;
    const values: any[] = [];

    if (platform) {
      sql += ` WHERE $1 = ANY(platforms)`;
      values.push(platform);
    }

    sql += ` ORDER BY string_to_array(version, '.')::int[] DESC LIMIT 1`;

    const result = await this.query(sql, values);
    return result.rows[0] || null;
  }

  // Get agent release by version
  async getAgentRelease(version: string): Promise<any | null> {
    const result = await this.query(
      `SELECT version, release_date as "releaseDate", changelog, is_required as "isRequired",
              platforms, min_version as "minVersion", created_at as "createdAt"
       FROM agent_releases WHERE version = $1`,
      [version]
    );
    return result.rows[0] || null;
  }

  // Create a new agent release
  async createAgentRelease(release: any): Promise<any> {
    await this.query(
      `INSERT INTO agent_releases (version, release_date, changelog, is_required, platforms, min_version)
       VALUES ($1, $2, $3, $4, $5, $6)
       ON CONFLICT (version) DO UPDATE SET
         changelog = EXCLUDED.changelog,
         is_required = EXCLUDED.is_required,
         platforms = EXCLUDED.platforms,
         min_version = EXCLUDED.min_version`,
      [
        release.version,
        release.releaseDate || new Date().toISOString(),
        release.changelog || '',
        release.isRequired || false,
        release.platforms || ['windows', 'linux', 'darwin'],
        release.minVersion || null
      ]
    );
    return this.getAgentRelease(release.version);
  }

  // Log an update attempt
  async logAgentUpdate(update: {
    agentId: string;
    fromVersion?: string;
    toVersion: string;
    platform?: string;
    architecture?: string;
    ipAddress?: string;
    status: string;
  }): Promise<any> {
    const id = uuidv4();
    await this.query(
      `INSERT INTO agent_updates (id, agent_id, from_version, to_version, platform, architecture, ip_address, status)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
      [id, update.agentId, update.fromVersion, update.toVersion, update.platform, update.architecture, update.ipAddress, update.status]
    );
    const result = await this.query('SELECT * FROM agent_updates WHERE id = $1', [id]);
    return result.rows[0];
  }

  // Update agent update status
  async updateAgentUpdateStatus(id: string, status: string, errorMessage?: string): Promise<void> {
    const updates = ['status = $1'];
    const values: any[] = [status];
    let paramIndex = 2;

    if (status === 'completed' || status === 'failed' || status === 'rolled_back') {
      updates.push('completed_at = CURRENT_TIMESTAMP');
    }
    if (errorMessage) {
      updates.push(`error_message = ${paramIndex++}`);
      values.push(errorMessage);
    }
    values.push(id);

    await this.query(
      `UPDATE agent_updates SET ${updates.join(', ')} WHERE id = ${paramIndex}`,
      values
    );
  }

  // Get agent update history
  async getAgentUpdateHistory(agentId: string, limit: number = 20): Promise<any[]> {
    const result = await this.query(
      `SELECT id, agent_id as "agentId", from_version as "fromVersion", to_version as "toVersion",
              platform, architecture, ip_address as "ipAddress", status, error_message as "errorMessage",
              created_at as "createdAt", completed_at as "completedAt"
       FROM agent_updates
       WHERE agent_id = $1
       ORDER BY created_at DESC
       LIMIT $2`,
      [agentId, limit]
    );
    return result.rows;
  }

  // Update device agent version after successful update
  async updateDeviceAgentVersion(agentId: string, newVersion: string, oldVersion?: string): Promise<void> {
    await this.query(
      `UPDATE devices SET
         previous_agent_version = COALESCE($3, agent_version),
         agent_version = $2,
         last_update_check = CURRENT_TIMESTAMP,
         updated_at = CURRENT_TIMESTAMP
       WHERE agent_id = $1`,
      [agentId, newVersion, oldVersion]
    );
  }

  // Update last update check timestamp
  async updateDeviceLastUpdateCheck(agentId: string): Promise<void> {
    await this.query(
      `UPDATE devices SET last_update_check = CURRENT_TIMESTAMP WHERE agent_id = $1`,
      [agentId]
    );
  }

  // Get all agent releases
  async getAgentReleases(): Promise<any[]> {
    const result = await this.query(`
      SELECT version, release_date as "releaseDate", changelog, is_required as "isRequired",
             platforms, min_version as "minVersion", created_at as "createdAt"
      FROM agent_releases
      ORDER BY string_to_array(version, '.')::int[] DESC
    `);
    return result.rows;
  }


  // ============================================================================
  // GRPC DATA PLANE METHODS
  // ============================================================================

  // Insert a log entry from agent
  async insertAgentLog(deviceId: string, log: {
    timestamp?: Date;
    level: string;
    source?: string;
    message: string;
    metadata?: Record<string, string>;
  }): Promise<void> {
    await this.query(
      `INSERT INTO agent_logs (device_id, timestamp, level, source, message, metadata)
       VALUES ($1, $2, $3, $4, $5, $6)`,
      [
        deviceId,
        log.timestamp || new Date(),
        log.level,
        log.source || null,
        log.message,
        JSON.stringify(log.metadata || {})
      ]
    );
  }

  // Get agent logs for a device
  async getAgentLogs(deviceId: string, options?: {
    level?: string;
    source?: string;
    limit?: number;
    offset?: number;
    since?: Date;
  }): Promise<any[]> {
    let sql = `
      SELECT id, device_id as "deviceId", timestamp, level, source, message, metadata, created_at as "createdAt"
      FROM agent_logs
      WHERE device_id = $1
    `;
    const values: any[] = [deviceId];
    let paramIndex = 2;

    if (options?.level) {
      sql += ` AND level = \$${paramIndex++}`;
      values.push(options.level);
    }
    if (options?.source) {
      sql += ` AND source = \$${paramIndex++}`;
      values.push(options.source);
    }
    if (options?.since) {
      sql += ` AND timestamp >= \$${paramIndex++}`;
      values.push(options.since);
    }

    sql += ' ORDER BY timestamp DESC';

    if (options?.limit) {
      sql += ` LIMIT \$${paramIndex++}`;
      values.push(options.limit);
    }
    if (options?.offset) {
      sql += ` OFFSET \$${paramIndex++}`;
      values.push(options.offset);
    }

    const result = await this.query(sql, values);
    return result.rows;
  }

  // Clean old agent logs
  async cleanOldAgentLogs(retentionDays: number = 30): Promise<number> {
    const result = await this.query(
      `DELETE FROM agent_logs WHERE timestamp < NOW() - INTERVAL '1 day' * $1`,
      [retentionDays]
    );
    return result.rowCount || 0;
  }

  // Update or insert software inventory for a device
  async upsertSoftwareInventory(deviceId: string, software: {
    name: string;
    version?: string;
    publisher?: string;
    installDate?: string;
    installLocation?: string;
    sizeBytes?: number;
    isSystemComponent?: boolean;
  }): Promise<void> {
    await this.query(
      `INSERT INTO software_inventory (device_id, name, version, publisher, install_date, install_location, size_bytes, is_system_component)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
       ON CONFLICT (device_id, name, version) DO UPDATE SET
         publisher = EXCLUDED.publisher,
         install_date = EXCLUDED.install_date,
         install_location = EXCLUDED.install_location,
         size_bytes = EXCLUDED.size_bytes,
         is_system_component = EXCLUDED.is_system_component,
         updated_at = CURRENT_TIMESTAMP`,
      [
        deviceId,
        software.name,
        software.version || null,
        software.publisher || null,
        software.installDate || null,
        software.installLocation || null,
        software.sizeBytes || null,
        software.isSystemComponent || false
      ]
    );
  }

  // Replace entire software inventory for a device
  async replaceSoftwareInventory(deviceId: string, softwareList: Array<{
    name: string;
    version?: string;
    publisher?: string;
    installDate?: string;
    installLocation?: string;
    sizeBytes?: number;
    isSystemComponent?: boolean;
  }>): Promise<void> {
    await this.query('DELETE FROM software_inventory WHERE device_id = $1', [deviceId]);
    for (const software of softwareList) {
      await this.upsertSoftwareInventory(deviceId, software);
    }
  }

  // Get software inventory for a device
  async getSoftwareInventory(deviceId: string): Promise<any[]> {
    const result = await this.query(
      `SELECT id, device_id as "deviceId", name, version, publisher,
              install_date as "installDate", install_location as "installLocation",
              size_bytes as "sizeBytes", is_system_component as "isSystemComponent",
              created_at as "createdAt", updated_at as "updatedAt"
       FROM software_inventory
       WHERE device_id = $1
       ORDER BY name`,
      [deviceId]
    );
    return result.rows;
  }

  // Log bulk data upload
  async logBulkDataUpload(deviceId: string, dataType: string, sizeBytes: number, requestId?: string, metadata?: Record<string, any>): Promise<void> {
    await this.query(
      `INSERT INTO bulk_data_uploads (device_id, data_type, request_id, size_bytes, metadata)
       VALUES ($1, $2, $3, $4, $5)`,
      [deviceId, dataType, requestId || null, sizeBytes, JSON.stringify(metadata || {})]
    );
  }

  // Update device gRPC connection status
  async updateDeviceGrpcStatus(deviceId: string, connected: boolean): Promise<void> {
    if (connected) {
      await this.query(
        `UPDATE devices SET grpc_connected = TRUE, grpc_last_seen = CURRENT_TIMESTAMP WHERE id = $1`,
        [deviceId]
      );
    } else {
      await this.query(
        `UPDATE devices SET grpc_connected = FALSE WHERE id = $1`,
        [deviceId]
      );
    }
  }


  // ============================================================================
  // CLIENT METHODS
  // ============================================================================

  async getClients(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        id, name, description, color, logo_url as "logoUrl",
        logo_width as "logoWidth", logo_height as "logoHeight",
        created_at as "createdAt", updated_at as "updatedAt"
      FROM clients
      ORDER BY name
    `);
    return result.rows;
  }

  async getClient(id: string): Promise<any | null> {
    const result = await this.query(
      `SELECT id, name, description, color, logo_url as "logoUrl",
              logo_width as "logoWidth", logo_height as "logoHeight",
              created_at as "createdAt", updated_at as "updatedAt"
       FROM clients WHERE id = $1`,
      [id]
    );
    return result.rows[0] || null;
  }

  async createClient(client: { name: string; description?: string; color?: string; logoUrl?: string; logoWidth?: number; logoHeight?: number }): Promise<any> {
    const id = uuidv4();
    await this.query(
      `INSERT INTO clients (id, name, description, color, logo_url, logo_width, logo_height)
       VALUES ($1, $2, $3, $4, $5, $6, $7)`,
      [id, client.name, client.description || null, client.color || null, client.logoUrl || null, client.logoWidth || 32, client.logoHeight || 32]
    );
    return this.getClient(id);
  }

  async updateClient(id: string, client: { name?: string; description?: string; color?: string; logoUrl?: string; logoWidth?: number; logoHeight?: number }): Promise<any | null> {
    const updates: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (client.name !== undefined) {
      updates.push(`name = $${paramIndex++}`);
      values.push(client.name);
    }
    if (client.description !== undefined) {
      updates.push(`description = $${paramIndex++}`);
      values.push(client.description);
    }
    if (client.color !== undefined) {
      updates.push(`color = $${paramIndex++}`);
      values.push(client.color);
    }
    if (client.logoUrl !== undefined) {
      updates.push(`logo_url = $${paramIndex++}`);
      values.push(client.logoUrl);
    }
    if (client.logoWidth !== undefined) {
      updates.push(`logo_width = $${paramIndex++}`);
      values.push(client.logoWidth);
    }
    if (client.logoHeight !== undefined) {
      updates.push(`logo_height = $${paramIndex++}`);
      values.push(client.logoHeight);
    }

    if (updates.length > 0) {
      values.push(id);
      await this.query(
        `UPDATE clients SET ${updates.join(', ')} WHERE id = $${paramIndex}`,
        values
      );
    }
    return this.getClient(id);
  }

  async deleteClient(id: string): Promise<void> {
    await this.query('DELETE FROM clients WHERE id = $1', [id]);
  }

  async getClientDeviceCount(clientId: string): Promise<number> {
    const result = await this.query(
      'SELECT COUNT(*) as count FROM devices WHERE client_id = $1',
      [clientId]
    );
    return parseInt(result.rows[0].count, 10);
  }

  async getClientsWithCounts(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        c.id, c.name, c.description, c.color, c.logo_url as "logoUrl",
        c.logo_width as "logoWidth", c.logo_height as "logoHeight",
        c.created_at as "createdAt", c.updated_at as "updatedAt",
        COUNT(DISTINCT d.id) as "deviceCount",
        COUNT(DISTINCT t.id) FILTER (WHERE t.status NOT IN ('closed', 'resolved')) as "openTicketCount"
      FROM clients c
      LEFT JOIN devices d ON d.client_id = c.id
      LEFT JOIN tickets t ON t.client_id = c.id
      GROUP BY c.id
      ORDER BY c.name
    `);
    return result.rows;
  }

  async assignDeviceToClient(deviceId: string, clientId: string | null): Promise<void> {
    await this.query(
      'UPDATE devices SET client_id = $1 WHERE id = $2',
      [clientId, deviceId]
    );
  }

  async bulkAssignDevicesToClient(deviceIds: string[], clientId: string | null): Promise<void> {
    await this.query(
      'UPDATE devices SET client_id = $1 WHERE id = ANY($2)',
      [clientId, deviceIds]
    );
  }

  // ============================================================================
  // CERTIFICATE STATUS METHODS
  // ============================================================================

  async setAgentCertStatus(agentId: string, caCertHash: string, distributed: boolean, confirmed: boolean): Promise<void> {
    const updates: string[] = ['ca_cert_hash = $2'];
    const values: any[] = [agentId, caCertHash];

    if (distributed) {
      updates.push('distributed_at = CURRENT_TIMESTAMP');
    }
    if (confirmed) {
      updates.push('confirmed_at = CURRENT_TIMESTAMP');
    }

    await this.query(
      `INSERT INTO agent_cert_status (agent_id, ca_cert_hash, distributed_at, confirmed_at)
       VALUES ($1, $2, ${distributed ? 'CURRENT_TIMESTAMP' : 'NULL'}, ${confirmed ? 'CURRENT_TIMESTAMP' : 'NULL'})
       ON CONFLICT (agent_id) DO UPDATE SET ${updates.join(', ')}`,
      values
    );
  }

  async getAgentCertStatuses(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        acs.id, acs.agent_id as "agentId", acs.ca_cert_hash as "caCertHash",
        acs.distributed_at as "distributedAt", acs.confirmed_at as "confirmedAt",
        acs.created_at as "createdAt", acs.updated_at as "updatedAt",
        d.hostname, d.display_name as "displayName", d.status as "deviceStatus"
      FROM agent_cert_status acs
      LEFT JOIN devices d ON d.agent_id = acs.agent_id
      ORDER BY acs.updated_at DESC
    `);
    return result.rows;
  }

  async getAgentCertStatus(agentId: string): Promise<any | null> {
    const result = await this.query(
      `SELECT
        id, agent_id as "agentId", ca_cert_hash as "caCertHash",
        distributed_at as "distributedAt", confirmed_at as "confirmedAt",
        created_at as "createdAt", updated_at as "updatedAt"
       FROM agent_cert_status WHERE agent_id = $1`,
      [agentId]
    );
    return result.rows[0] || null;
  }

  async clearAgentCertStatuses(): Promise<void> {
    await this.query('DELETE FROM agent_cert_status');
  }

  async deleteAgentCertStatus(agentId: string): Promise<void> {
    await this.query('DELETE FROM agent_cert_status WHERE agent_id = $1', [agentId]);
  }

  // ============================================================================
  // DEVICE UPDATE STATUS METHODS
  // ============================================================================

  async upsertDeviceUpdateStatus(deviceId: string, status: {
    pendingCount: number;
    securityUpdateCount: number;
    rebootRequired: boolean;
    lastChecked: string;
    lastUpdateInstalled?: string;
    pendingUpdates?: any[];
  }): Promise<void> {
    await this.query(
      `INSERT INTO device_updates (device_id, pending_count, security_update_count, reboot_required, last_checked, last_update_installed, pending_updates)
       VALUES ($1, $2, $3, $4, $5, $6, $7)
       ON CONFLICT (device_id) DO UPDATE SET
         pending_count = EXCLUDED.pending_count,
         security_update_count = EXCLUDED.security_update_count,
         reboot_required = EXCLUDED.reboot_required,
         last_checked = EXCLUDED.last_checked,
         last_update_installed = COALESCE(EXCLUDED.last_update_installed, device_updates.last_update_installed),
         pending_updates = EXCLUDED.pending_updates,
         updated_at = CURRENT_TIMESTAMP`,
      [
        deviceId,
        status.pendingCount,
        status.securityUpdateCount,
        status.rebootRequired,
        status.lastChecked,
        status.lastUpdateInstalled || null,
        JSON.stringify(status.pendingUpdates || [])
      ]
    );
  }

  async getDeviceUpdateStatus(deviceId: string): Promise<any | null> {
    const result = await this.query(
      `SELECT
        id, device_id as "deviceId", pending_count as "pendingCount",
        security_update_count as "securityUpdateCount", reboot_required as "rebootRequired",
        last_checked as "lastChecked", last_update_installed as "lastUpdateInstalled",
        pending_updates as "pendingUpdates", created_at as "createdAt", updated_at as "updatedAt"
       FROM device_updates WHERE device_id = $1`,
      [deviceId]
    );
    return result.rows[0] || null;
  }

  async getAllDeviceUpdateStatuses(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        du.id, du.device_id as "deviceId", du.pending_count as "pendingCount",
        du.security_update_count as "securityUpdateCount", du.reboot_required as "rebootRequired",
        du.last_checked as "lastChecked", du.last_update_installed as "lastUpdateInstalled",
        du.pending_updates as "pendingUpdates", du.created_at as "createdAt", du.updated_at as "updatedAt",
        d.hostname, d.display_name as "displayName", d.status as "deviceStatus"
      FROM device_updates du
      LEFT JOIN devices d ON d.id = du.device_id
      ORDER BY du.security_update_count DESC, du.pending_count DESC
    `);
    return result.rows;
  }

  async getDevicesWithPendingUpdates(minCount: number = 1): Promise<any[]> {
    const result = await this.query(`
      SELECT
        du.device_id as "deviceId", du.pending_count as "pendingCount",
        du.security_update_count as "securityUpdateCount", du.reboot_required as "rebootRequired",
        du.last_checked as "lastChecked",
        d.hostname, d.display_name as "displayName", d.status as "deviceStatus"
      FROM device_updates du
      JOIN devices d ON d.id = du.device_id
      WHERE du.pending_count >= $1
      ORDER BY du.security_update_count DESC, du.pending_count DESC
    `, [minCount]);
    return result.rows;
  }

  async getDevicesWithSecurityUpdates(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        du.device_id as "deviceId", du.pending_count as "pendingCount",
        du.security_update_count as "securityUpdateCount", du.reboot_required as "rebootRequired",
        du.last_checked as "lastChecked", du.pending_updates as "pendingUpdates",
        d.hostname, d.display_name as "displayName", d.status as "deviceStatus"
      FROM device_updates du
      JOIN devices d ON d.id = du.device_id
      WHERE du.security_update_count > 0
      ORDER BY du.security_update_count DESC
    `);
    return result.rows;
  }

  async getDevicesRequiringReboot(): Promise<any[]> {
    const result = await this.query(`
      SELECT
        du.device_id as "deviceId", du.pending_count as "pendingCount",
        du.security_update_count as "securityUpdateCount", du.reboot_required as "rebootRequired",
        du.last_checked as "lastChecked",
        d.hostname, d.display_name as "displayName", d.status as "deviceStatus"
      FROM device_updates du
      JOIN devices d ON d.id = du.device_id
      WHERE du.reboot_required = TRUE
      ORDER BY d.hostname
    `);
    return result.rows;
  }

  // ============================================
  // Agent Health Methods
  // ============================================

  async upsertAgentHealth(deviceId: string, health: {
    healthScore: number;
    status: string;
    factors: any;
    components: any;
    updatedAt: Date;
  }): Promise<void> {
    await this.query(`
      INSERT INTO agent_health (device_id, health_score, status, factors, components, updated_at)
      VALUES ($1, $2, $3, $4, $5, $6)
      ON CONFLICT (device_id) DO UPDATE SET
        health_score = EXCLUDED.health_score,
        status = EXCLUDED.status,
        factors = EXCLUDED.factors,
        components = EXCLUDED.components,
        updated_at = EXCLUDED.updated_at
    `, [deviceId, health.healthScore, health.status, JSON.stringify(health.factors), JSON.stringify(health.components), health.updatedAt]);
  }

  async getAgentHealth(deviceId: string): Promise<any | null> {
    const result = await this.query(`
      SELECT device_id as "deviceId", health_score as "healthScore", status,
             factors, components, updated_at as "updatedAt"
      FROM agent_health WHERE device_id = $1
    `, [deviceId]);
    return result.rows[0] || null;
  }

  async getAllAgentHealth(): Promise<any[]> {
    const result = await this.query(`
      SELECT ah.device_id as "deviceId", ah.health_score as "healthScore", ah.status,
             ah.factors, ah.components, ah.updated_at as "updatedAt",
             d.hostname, d.display_name as "displayName"
      FROM agent_health ah
      JOIN devices d ON d.id = ah.device_id
      ORDER BY ah.health_score ASC
    `);
    return result.rows;
  }

  async recordHealthSnapshot(): Promise<number> {
    const result = await this.query(`SELECT record_health_snapshot() as count`);
    return result.rows[0]?.count || 0;
  }

  async getAgentHealthHistory(deviceId: string, hours: number = 24): Promise<any[]> {
    const result = await this.query(`
      SELECT device_id as "deviceId", health_score as "healthScore", status,
             factors, recorded_at as "recordedAt"
      FROM agent_health_history
      WHERE device_id = $1 AND recorded_at > NOW() - ($2 || ' hours')::INTERVAL
      ORDER BY recorded_at DESC
    `, [deviceId, hours]);
    return result.rows;
  }

  async cleanHealthHistory(retentionDays: number = 30): Promise<number> {
    const result = await this.query(`SELECT clean_health_history($1) as count`, [retentionDays]);
    return result.rows[0]?.count || 0;
  }

  // ============================================
  // Command Queue Methods (for offline agents)
  // ============================================

  async queueCommand(command: {
    id: string;
    deviceId: string;
    commandType: string;
    payload: any;
    priority?: number;
    expiresAt?: Date;
    maxAttempts?: number;
  }): Promise<void> {
    await this.query(`
      INSERT INTO command_queue (id, device_id, command_type, payload, priority, expires_at, max_attempts)
      VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, [
      command.id,
      command.deviceId,
      command.commandType,
      JSON.stringify(command.payload),
      command.priority || 50,
      command.expiresAt,
      command.maxAttempts || 3
    ]);
  }

  async getQueuedCommand(commandId: string): Promise<any | null> {
    const result = await this.query(`
      SELECT id, device_id as "deviceId", command_type as "commandType", payload, priority,
             status, created_at as "createdAt", expires_at as "expiresAt",
             attempts, max_attempts as "maxAttempts", delivered_at as "deliveredAt",
             completed_at as "completedAt", result, error
      FROM command_queue WHERE id = $1
    `, [commandId]);
    return result.rows[0] || null;
  }

  async getQueuedCommandsForDevice(deviceId: string): Promise<any[]> {
    const result = await this.query(`
      SELECT id, device_id as "deviceId", command_type as "commandType", payload, priority,
             status, created_at as "createdAt", expires_at as "expiresAt",
             attempts, max_attempts as "maxAttempts"
      FROM command_queue
      WHERE device_id = $1
      ORDER BY created_at DESC
    `, [deviceId]);
    return result.rows;
  }

  async getPendingCommandsForDevice(deviceId: string): Promise<any[]> {
    const result = await this.query(`
      SELECT id, device_id as "deviceId", command_type as "commandType", payload, priority,
             status, created_at as "createdAt", expires_at as "expiresAt",
             attempts, max_attempts as "maxAttempts"
      FROM command_queue
      WHERE device_id = $1 AND status IN ('queued', 'pending')
        AND (expires_at IS NULL OR expires_at > NOW())
        AND attempts < max_attempts
      ORDER BY priority DESC, created_at ASC
    `, [deviceId]);
    return result.rows;
  }

  async markCommandDelivered(commandId: string): Promise<void> {
    await this.query(`
      UPDATE command_queue
      SET status = 'delivered', delivered_at = NOW(), attempts = attempts + 1
      WHERE id = $1
    `, [commandId]);
  }

  async markCommandCompleted(commandId: string, result: any): Promise<void> {
    await this.query(`
      UPDATE command_queue
      SET status = 'completed', completed_at = NOW(), result = $2
      WHERE id = $1
    `, [commandId, JSON.stringify(result)]);
  }

  async markCommandFailed(commandId: string, error: string): Promise<void> {
    await this.query(`
      UPDATE command_queue
      SET status = 'failed', completed_at = NOW(), error = $2
      WHERE id = $1
    `, [commandId, error]);
  }

  async expireOldCommands(): Promise<number> {
    const result = await this.query(`
      UPDATE command_queue
      SET status = 'expired'
      WHERE status IN ('queued', 'pending')
        AND expires_at IS NOT NULL AND expires_at < NOW()
      RETURNING id
    `);
    return result.rowCount || 0;
  }

  async storeMetricsBacklog(deviceId: string, collectedAt: Date, metrics: any): Promise<string> {
    const result = await this.query(`
      INSERT INTO metrics_backlog (device_id, collected_at, metrics)
      VALUES ($1, $2, $3)
      RETURNING id
    `, [deviceId, collectedAt, JSON.stringify(metrics)]);
    return result.rows[0].id;
  }

  async getMetricsBacklog(deviceId: string, limit: number = 100): Promise<any[]> {
    const result = await this.query(`
      SELECT id, device_id as "deviceId", collected_at as "collectedAt", metrics, synced
      FROM metrics_backlog
      WHERE device_id = $1 AND synced = FALSE
      ORDER BY collected_at ASC
      LIMIT $2
    `, [deviceId, limit]);
    return result.rows;
  }

  async markMetricsBacklogSynced(ids: string[]): Promise<void> {
    if (ids.length === 0) return;
    await this.query(`
      UPDATE metrics_backlog SET synced = TRUE WHERE id = ANY($1)
    `, [ids]);
  }

  // ============================================
  // Update Groups Methods
  // ============================================

  async getUpdateGroups(): Promise<any[]> {
    const result = await this.query(`
      SELECT id, name, priority, auto_promote as "autoPromote",
             success_threshold_percent as "successThresholdPercent",
             failure_threshold_percent as "failureThresholdPercent",
             min_devices_for_decision as "minDevicesForDecision",
             wait_time_minutes as "waitTimeMinutes",
             created_at as "createdAt"
      FROM update_groups
      ORDER BY priority ASC
    `);
    return result.rows;
  }

  async getUpdateGroup(id: string): Promise<any | null> {
    const result = await this.query(`
      SELECT id, name, priority, auto_promote as "autoPromote",
             success_threshold_percent as "successThresholdPercent",
             failure_threshold_percent as "failureThresholdPercent",
             min_devices_for_decision as "minDevicesForDecision",
             wait_time_minutes as "waitTimeMinutes",
             created_at as "createdAt"
      FROM update_groups WHERE id = $1
    `, [id]);
    return result.rows[0] || null;
  }

  async createUpdateGroup(group: {
    id: string;
    name: string;
    priority?: number;
    autoPromote?: boolean;
    successThresholdPercent?: number;
    failureThresholdPercent?: number;
    minDevicesForDecision?: number;
    waitTimeMinutes?: number;
  }): Promise<void> {
    await this.query(`
      INSERT INTO update_groups (id, name, priority, auto_promote, success_threshold_percent,
                                  failure_threshold_percent, min_devices_for_decision, wait_time_minutes)
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `, [
      group.id, group.name, group.priority || 0, group.autoPromote ?? false,
      group.successThresholdPercent || 95, group.failureThresholdPercent || 10,
      group.minDevicesForDecision || 3, group.waitTimeMinutes || 60
    ]);
  }

  async updateUpdateGroup(id: string, updates: Partial<{
    name: string;
    priority: number;
    autoPromote: boolean;
    successThresholdPercent: number;
    failureThresholdPercent: number;
    minDevicesForDecision: number;
    waitTimeMinutes: number;
  }>): Promise<void> {
    const fields: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (updates.name !== undefined) { fields.push(`name = $${paramIndex++}`); values.push(updates.name); }
    if (updates.priority !== undefined) { fields.push(`priority = $${paramIndex++}`); values.push(updates.priority); }
    if (updates.autoPromote !== undefined) { fields.push(`auto_promote = $${paramIndex++}`); values.push(updates.autoPromote); }
    if (updates.successThresholdPercent !== undefined) { fields.push(`success_threshold_percent = $${paramIndex++}`); values.push(updates.successThresholdPercent); }
    if (updates.failureThresholdPercent !== undefined) { fields.push(`failure_threshold_percent = $${paramIndex++}`); values.push(updates.failureThresholdPercent); }
    if (updates.minDevicesForDecision !== undefined) { fields.push(`min_devices_for_decision = $${paramIndex++}`); values.push(updates.minDevicesForDecision); }
    if (updates.waitTimeMinutes !== undefined) { fields.push(`wait_time_minutes = $${paramIndex++}`); values.push(updates.waitTimeMinutes); }

    if (fields.length === 0) return;
    values.push(id);
    await this.query(`UPDATE update_groups SET ${fields.join(', ')} WHERE id = $${paramIndex}`, values);
  }

  async deleteUpdateGroup(id: string): Promise<void> {
    await this.query(`DELETE FROM update_groups WHERE id = $1`, [id]);
  }

  async assignDeviceToUpdateGroup(deviceId: string, groupId: string | null): Promise<void> {
    await this.query(`UPDATE devices SET update_group_id = $2 WHERE id = $1`, [deviceId, groupId]);
  }

  async getDevicesInUpdateGroup(groupId: string): Promise<any[]> {
    const result = await this.query(`
      SELECT id, hostname, display_name as "displayName", status, agent_version as "agentVersion",
             last_seen as "lastSeen"
      FROM devices WHERE update_group_id = $1
      ORDER BY hostname
    `, [groupId]);
    return result.rows;
  }

  // ============================================
  // Rollout Methods
  // ============================================

  async getRollouts(limit: number = 50): Promise<any[]> {
    const result = await this.query(`
      SELECT id, release_version as "releaseVersion", name, status,
             started_at as "startedAt", completed_at as "completedAt",
             created_at as "createdAt"
      FROM rollouts
      ORDER BY created_at DESC
      LIMIT $1
    `, [limit]);
    return result.rows;
  }

  async getRollout(id: string): Promise<any | null> {
    const result = await this.query(`
      SELECT id, release_version as "releaseVersion", name, status,
             started_at as "startedAt", completed_at as "completedAt",
             created_at as "createdAt"
      FROM rollouts WHERE id = $1
    `, [id]);
    return result.rows[0] || null;
  }

  async createRollout(rollout: {
    id: string;
    releaseVersion: string;
    name: string;
    downloadUrl?: string;
    checksum?: string;
  }): Promise<void> {
    await this.query(`
      INSERT INTO rollouts (id, release_version, name, download_url, checksum) VALUES ($1, $2, $3, $4, $5)
    `, [rollout.id, rollout.releaseVersion, rollout.name, rollout.downloadUrl || null, rollout.checksum || null]);
  }

  async updateRolloutStatus(id: string, status: string): Promise<void> {
    const updates = status === 'in_progress'
      ? `status = $2, started_at = COALESCE(started_at, NOW())`
      : status === 'completed' || status === 'failed' || status === 'cancelled'
        ? `status = $2, completed_at = NOW()`
        : `status = $2`;
    await this.query(`UPDATE rollouts SET ${updates} WHERE id = $1`, [id, status]);
  }

  async getRolloutStages(rolloutId: string): Promise<any[]> {
    const result = await this.query(`
      SELECT rs.id, rs.rollout_id as "rolloutId", rs.group_id as "groupId",
             rs.status, rs.started_at as "startedAt", rs.completed_at as "completedAt",
             rs.total_devices as "totalDevices", rs.completed_devices as "completedDevices",
             rs.failed_devices as "failedDevices",
             ug.name as "groupName", ug.priority as "groupPriority"
      FROM rollout_stages rs
      JOIN update_groups ug ON ug.id = rs.group_id
      WHERE rs.rollout_id = $1
      ORDER BY ug.priority ASC
    `, [rolloutId]);
    return result.rows;
  }

  async createRolloutStage(stage: {
    id: string;
    rolloutId: string;
    groupId: string;
    totalDevices: number;
  }): Promise<void> {
    await this.query(`
      INSERT INTO rollout_stages (id, rollout_id, group_id, total_devices)
      VALUES ($1, $2, $3, $4)
    `, [stage.id, stage.rolloutId, stage.groupId, stage.totalDevices]);
  }

  async updateRolloutStageStatus(stageId: string, status: string): Promise<void> {
    const updates = status === 'in_progress'
      ? `status = $2, started_at = COALESCE(started_at, NOW())`
      : status === 'completed' || status === 'failed'
        ? `status = $2, completed_at = NOW()`
        : `status = $2`;
    await this.query(`UPDATE rollout_stages SET ${updates} WHERE id = $1`, [stageId, status]);
  }

  async updateRolloutStageDeviceCounts(stageId: string, completed: number, failed: number): Promise<void> {
    await this.query(`
      UPDATE rollout_stages
      SET completed_devices = $2, failed_devices = $3
      WHERE id = $1
    `, [stageId, completed, failed]);
  }

  async getRolloutDevices(rolloutId: string, stageId?: string): Promise<any[]> {
    let query = `
      SELECT rd.id, rd.rollout_id as "rolloutId", rd.stage_id as "stageId",
             rd.device_id as "deviceId", rd.status,
             rd.from_version as "fromVersion", rd.to_version as "toVersion",
             rd.started_at as "startedAt", rd.completed_at as "completedAt",
             rd.error, d.hostname, d.display_name as "displayName"
      FROM rollout_devices rd
      JOIN devices d ON d.id = rd.device_id
      WHERE rd.rollout_id = $1
    `;
    const params: any[] = [rolloutId];
    if (stageId) {
      query += ` AND rd.stage_id = $2`;
      params.push(stageId);
    }
    query += ` ORDER BY rd.started_at DESC NULLS LAST`;
    const result = await this.query(query, params);
    return result.rows;
  }

  async addRolloutDevice(device: {
    id: string;
    rolloutId: string;
    stageId: string;
    deviceId: string;
    fromVersion?: string;
    toVersion: string;
  }): Promise<void> {
    await this.query(`
      INSERT INTO rollout_devices (id, rollout_id, stage_id, device_id, from_version, to_version)
      VALUES ($1, $2, $3, $4, $5, $6)
    `, [device.id, device.rolloutId, device.stageId, device.deviceId, device.fromVersion, device.toVersion]);
  }

  async updateRolloutDeviceStatus(deviceId: string, rolloutId: string, status: string, error?: string): Promise<void> {
    const updates = status === 'in_progress'
      ? `status = $3, started_at = COALESCE(started_at, NOW())`
      : status === 'completed' || status === 'failed'
        ? `status = $3, completed_at = NOW(), error = $4`
        : `status = $3`;
    await this.query(`
      UPDATE rollout_devices SET ${updates}
      WHERE device_id = $1 AND rollout_id = $2
    `, [deviceId, rolloutId, status, error]);
  }

  async addRolloutEvent(event: {
    id: string;
    rolloutId: string;
    eventType: string;
    message: string;
    metadata?: any;
  }): Promise<void> {
    await this.query(`
      INSERT INTO rollout_events (id, rollout_id, event_type, message, metadata)
      VALUES ($1, $2, $3, $4, $5)
    `, [event.id, event.rolloutId, event.eventType, event.message, event.metadata ? JSON.stringify(event.metadata) : null]);
  }

  async getRolloutEvents(rolloutId: string): Promise<any[]> {
    const result = await this.query(`
      SELECT id, rollout_id as "rolloutId", event_type as "eventType",
             message, metadata, created_at as "createdAt"
      FROM rollout_events
      WHERE rollout_id = $1
      ORDER BY created_at DESC
    `, [rolloutId]);
    return result.rows;
  }

  // =========================================================================
  // Portal Methods - Client Tenants
  // =========================================================================

  async getClientTenants(): Promise<any[]> {
    const result = await this.query(`
      SELECT ct.id, ct.client_id as "clientId", ct.tenant_id as "tenantId",
             ct.tenant_name as "tenantName", ct.enabled,
             ct.created_at as "createdAt", ct.updated_at as "updatedAt",
             c.name as "clientName"
      FROM client_tenants ct
      LEFT JOIN clients c ON ct.client_id = c.id
      ORDER BY ct.created_at DESC
    `);
    return result.rows;
  }

  async getClientTenantByTenantId(tenantId: string): Promise<any | null> {
    const result = await this.query(`
      SELECT ct.id, ct.client_id as "clientId", ct.tenant_id as "tenantId",
             ct.tenant_name as "tenantName", ct.enabled,
             ct.created_at as "createdAt", ct.updated_at as "updatedAt",
             c.name as "clientName"
      FROM client_tenants ct
      LEFT JOIN clients c ON ct.client_id = c.id
      WHERE ct.tenant_id = $1
    `, [tenantId]);
    return result.rows[0] || null;
  }

  async createClientTenant(data: {
    clientId: string;
    tenantId: string;
    tenantName?: string;
    enabled?: boolean;
  }): Promise<any> {
    const id = uuidv4();
    await this.query(`
      INSERT INTO client_tenants (id, client_id, tenant_id, tenant_name, enabled)
      VALUES ($1, $2, $3, $4, $5)
    `, [id, data.clientId, data.tenantId, data.tenantName || null, data.enabled !== false]);
    return this.getClientTenantByTenantId(data.tenantId);
  }

  async updateClientTenant(id: string, data: {
    tenantName?: string;
    enabled?: boolean;
  }): Promise<any | null> {
    const fields: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (data.tenantName !== undefined) {
      fields.push(`tenant_name = $${paramIndex++}`);
      values.push(data.tenantName);
    }
    if (data.enabled !== undefined) {
      fields.push(`enabled = $${paramIndex++}`);
      values.push(data.enabled);
    }

    if (fields.length > 0) {
      values.push(id);
      await this.query(`
        UPDATE client_tenants SET ${fields.join(', ')} WHERE id = $${paramIndex}
      `, values);
    }

    const result = await this.query(`
      SELECT ct.id, ct.client_id as "clientId", ct.tenant_id as "tenantId",
             ct.tenant_name as "tenantName", ct.enabled,
             ct.created_at as "createdAt", ct.updated_at as "updatedAt",
             c.name as "clientName"
      FROM client_tenants ct
      LEFT JOIN clients c ON ct.client_id = c.id
      WHERE ct.id = $1
    `, [id]);
    return result.rows[0] || null;
  }

  async deleteClientTenant(id: string): Promise<void> {
    await this.query('DELETE FROM client_tenants WHERE id = $1', [id]);
  }

  // =========================================================================
  // Portal Methods - Sessions
  // =========================================================================

  async createPortalSession(data: {
    userEmail: string;
    userName?: string;
    tenantId: string;
    clientId?: string;
    accessToken?: string;
    refreshToken?: string;
    idToken?: string;
    expiresAt?: Date;
  }): Promise<any> {
    const id = uuidv4();
    await this.query(`
      INSERT INTO portal_sessions (id, user_email, user_name, tenant_id, client_id,
                                   access_token, refresh_token, id_token, expires_at)
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, [
      id,
      data.userEmail,
      data.userName || null,
      data.tenantId,
      data.clientId || null,
      data.accessToken || null,
      data.refreshToken || null,
      data.idToken || null,
      data.expiresAt || null
    ]);

    return this.getPortalSession(id);
  }

  async getPortalSession(id: string): Promise<any | null> {
    const result = await this.query(`
      SELECT id, user_email as "userEmail", user_name as "userName",
             tenant_id as "tenantId", client_id as "clientId",
             access_token as "accessToken", refresh_token as "refreshToken",
             id_token as "idToken", expires_at as "expiresAt",
             created_at as "createdAt", last_activity as "lastActivity"
      FROM portal_sessions
      WHERE id = $1
    `, [id]);
    return result.rows[0] || null;
  }

  async updatePortalSessionActivity(id: string): Promise<void> {
    await this.query(`
      UPDATE portal_sessions SET last_activity = CURRENT_TIMESTAMP WHERE id = $1
    `, [id]);
  }

  async deletePortalSession(id: string): Promise<void> {
    await this.query('DELETE FROM portal_sessions WHERE id = $1', [id]);
  }

  async cleanupExpiredSessions(): Promise<number> {
    const result = await this.query(`
      DELETE FROM portal_sessions
      WHERE expires_at < CURRENT_TIMESTAMP
         OR last_activity < CURRENT_TIMESTAMP - INTERVAL '24 hours'
    `);
    return result.rowCount || 0;
  }

  // =========================================================================
  // Portal Methods - Tickets (Extended)
  // =========================================================================

  async createPortalTicket(ticket: {
    subject: string;
    description: string;
    priority?: string;
    type?: string;
    deviceId?: string;
    deviceName?: string;
    clientId?: string;
    submitterEmail: string;
    submitterName: string;
  }): Promise<any> {
    const id = uuidv4();
    await this.query(`
      INSERT INTO tickets (
        id, subject, description, status, priority, type, device_id, device_name,
        client_id, submitter_email, submitter_name, requester_name,
        requester_email, source
      ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    `, [
      id,
      ticket.subject,
      ticket.description,
      'open',
      ticket.priority || 'medium',
      ticket.type || 'incident',
      ticket.deviceId || null,
      ticket.deviceName || null,
      ticket.clientId || null,
      ticket.submitterEmail,
      ticket.submitterName,
      ticket.submitterName, // Also set as requester
      ticket.submitterEmail,
      'portal'
    ]);

    // Log activity
    await this.createTicketActivity(id, 'created', null, null, null, ticket.submitterName);

    return this.getTicket(id);
  }

  async getTicketsBySubmitter(email: string): Promise<any[]> {
    const result = await this.query(`
      SELECT
        t.id, t.ticket_number as "ticketNumber", t.subject, t.description,
        t.status, t.priority, t.type, t.device_id as "deviceId",
        t.device_name as "userDeviceName",
        d.hostname as "deviceName", d.display_name as "deviceDisplayName",
        t.requester_name as "requesterName", t.requester_email as "requesterEmail",
        t.submitter_name as "submitterName", t.submitter_email as "submitterEmail",
        t.assigned_to as "assignedTo", t.tags, t.due_date as "dueDate",
        t.resolved_at as "resolvedAt", t.closed_at as "closedAt",
        t.created_at as "createdAt", t.updated_at as "updatedAt",
        t.source
      FROM tickets t
      LEFT JOIN devices d ON t.device_id = d.id
      WHERE t.submitter_email = $1 OR t.requester_email = $1
      ORDER BY t.created_at DESC
    `, [email]);
    return result.rows;
  }

  async getTicketsByClient(clientId: string): Promise<any[]> {
    const result = await this.query(`
      SELECT
        t.id, t.ticket_number as "ticketNumber", t.subject, t.description,
        t.status, t.priority, t.type, t.device_id as "deviceId",
        d.hostname as "deviceName", d.display_name as "deviceDisplayName",
        t.requester_name as "requesterName", t.requester_email as "requesterEmail",
        t.submitter_name as "submitterName", t.submitter_email as "submitterEmail",
        t.assigned_to as "assignedTo", t.tags, t.due_date as "dueDate",
        t.resolved_at as "resolvedAt", t.closed_at as "closedAt",
        t.created_at as "createdAt", t.updated_at as "updatedAt",
        t.source
      FROM tickets t
      LEFT JOIN devices d ON t.device_id = d.id
      WHERE t.client_id = $1
      ORDER BY t.created_at DESC
    `, [clientId]);
    return result.rows;
  }

  // =========================================================================
  // Portal Methods - Email Queue
  // =========================================================================

  async queueEmail(data: {
    toAddresses: string[];
    ccAddresses?: string[];
    subject: string;
    bodyHtml?: string;
    bodyText?: string;
    templateName?: string;
    templateData?: any;
    scheduledAt?: Date;
  }): Promise<any> {
    const id = uuidv4();
    await this.query(`
      INSERT INTO email_queue (id, to_addresses, cc_addresses, subject, body_html,
                               body_text, template_name, template_data, scheduled_at)
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, [
      id,
      JSON.stringify(data.toAddresses),
      JSON.stringify(data.ccAddresses || []),
      data.subject,
      data.bodyHtml || null,
      data.bodyText || null,
      data.templateName || null,
      data.templateData ? JSON.stringify(data.templateData) : null,
      data.scheduledAt || new Date()
    ]);

    return this.getQueuedEmail(id);
  }

  async getQueuedEmail(id: string): Promise<any | null> {
    const result = await this.query(`
      SELECT id, to_addresses as "toAddresses", cc_addresses as "ccAddresses",
             subject, body_html as "bodyHtml", body_text as "bodyText",
             template_name as "templateName", template_data as "templateData",
             status, error_message as "errorMessage", retry_count as "retryCount",
             scheduled_at as "scheduledAt", sent_at as "sentAt",
             created_at as "createdAt"
      FROM email_queue
      WHERE id = $1
    `, [id]);
    return result.rows[0] || null;
  }

  async getPendingEmails(limit: number = 50): Promise<any[]> {
    const result = await this.query(`
      SELECT id, to_addresses as "toAddresses", cc_addresses as "ccAddresses",
             subject, body_html as "bodyHtml", body_text as "bodyText",
             template_name as "templateName", template_data as "templateData",
             status, error_message as "errorMessage", retry_count as "retryCount",
             scheduled_at as "scheduledAt", created_at as "createdAt"
      FROM email_queue
      WHERE status = 'pending'
        AND scheduled_at <= CURRENT_TIMESTAMP
        AND retry_count < 3
      ORDER BY scheduled_at ASC
      LIMIT $1
    `, [limit]);
    return result.rows;
  }

  async updateEmailStatus(id: string, status: string, errorMessage?: string): Promise<void> {
    if (status === 'sent') {
      await this.query(`
        UPDATE email_queue SET status = $1, sent_at = CURRENT_TIMESTAMP WHERE id = $2
      `, [status, id]);
    } else if (status === 'failed') {
      await this.query(`
        UPDATE email_queue SET status = $1, error_message = $2, retry_count = retry_count + 1 WHERE id = $3
      `, [status, errorMessage || null, id]);
    } else {
      await this.query(`
        UPDATE email_queue SET status = $1 WHERE id = $2
      `, [status, id]);
    }
  }

  // =========================================================================
  // Portal Methods - Email Templates
  // =========================================================================

  async getEmailTemplates(): Promise<any[]> {
    const result = await this.query(`
      SELECT id, name, subject, body_html as "bodyHtml", body_text as "bodyText",
             description, is_active as "isActive",
             created_at as "createdAt", updated_at as "updatedAt"
      FROM email_templates
      ORDER BY name
    `);
    return result.rows;
  }

  async getEmailTemplate(name: string): Promise<any | null> {
    const result = await this.query(`
      SELECT id, name, subject, body_html as "bodyHtml", body_text as "bodyText",
             description, is_active as "isActive",
             created_at as "createdAt", updated_at as "updatedAt"
      FROM email_templates
      WHERE name = $1 AND is_active = true
    `, [name]);
    return result.rows[0] || null;
  }

  async updateEmailTemplate(id: string, data: {
    subject?: string;
    bodyHtml?: string;
    bodyText?: string;
    description?: string;
    isActive?: boolean;
  }): Promise<any | null> {
    const fields: string[] = [];
    const values: any[] = [];
    let paramIndex = 1;

    if (data.subject !== undefined) {
      fields.push(`subject = $${paramIndex++}`);
      values.push(data.subject);
    }
    if (data.bodyHtml !== undefined) {
      fields.push(`body_html = $${paramIndex++}`);
      values.push(data.bodyHtml);
    }
    if (data.bodyText !== undefined) {
      fields.push(`body_text = $${paramIndex++}`);
      values.push(data.bodyText);
    }
    if (data.description !== undefined) {
      fields.push(`description = $${paramIndex++}`);
      values.push(data.description);
    }
    if (data.isActive !== undefined) {
      fields.push(`is_active = $${paramIndex++}`);
      values.push(data.isActive);
    }

    if (fields.length > 0) {
      values.push(id);
      await this.query(`
        UPDATE email_templates SET ${fields.join(', ')} WHERE id = $${paramIndex}
      `, values);
    }

    const result = await this.query(`
      SELECT id, name, subject, body_html as "bodyHtml", body_text as "bodyText",
             description, is_active as "isActive",
             created_at as "createdAt", updated_at as "updatedAt"
      FROM email_templates
      WHERE id = $1
    `, [id]);
    return result.rows[0] || null;
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
