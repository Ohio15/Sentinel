const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

// ============================================================================
// 1. ADD DATABASE METHODS FOR UPDATE TRACKING
// ============================================================================
const databasePath = 'D:/Projects/Sentinel/src/main/database.ts';
let dbContent = fs.readFileSync(databasePath, 'utf-8');

// Add methods before the close() method
const closeMethMatch = dbContent.match(/(\n  async close\(\): Promise<void>)/);
if (closeMethMatch) {
  const updateMethods = `

  // ============================================================================
  // AGENT UPDATE METHODS
  // ============================================================================

  // Get the latest agent release version
  async getLatestAgentRelease(platform?: string): Promise<any | null> {
    let sql = \`
      SELECT version, release_date as "releaseDate", changelog, is_required as "isRequired",
             platforms, min_version as "minVersion", created_at as "createdAt"
      FROM agent_releases
    \`;
    const values: any[] = [];

    if (platform) {
      sql += \` WHERE $1 = ANY(platforms)\`;
      values.push(platform);
    }

    sql += \` ORDER BY string_to_array(version, '.')::int[] DESC LIMIT 1\`;

    const result = await this.query(sql, values);
    return result.rows[0] || null;
  }

  // Get agent release by version
  async getAgentRelease(version: string): Promise<any | null> {
    const result = await this.query(
      \`SELECT version, release_date as "releaseDate", changelog, is_required as "isRequired",
              platforms, min_version as "minVersion", created_at as "createdAt"
       FROM agent_releases WHERE version = $1\`,
      [version]
    );
    return result.rows[0] || null;
  }

  // Create a new agent release
  async createAgentRelease(release: any): Promise<any> {
    await this.query(
      \`INSERT INTO agent_releases (version, release_date, changelog, is_required, platforms, min_version)
       VALUES ($1, $2, $3, $4, $5, $6)
       ON CONFLICT (version) DO UPDATE SET
         changelog = EXCLUDED.changelog,
         is_required = EXCLUDED.is_required,
         platforms = EXCLUDED.platforms,
         min_version = EXCLUDED.min_version\`,
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
      \`INSERT INTO agent_updates (id, agent_id, from_version, to_version, platform, architecture, ip_address, status)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8)\`,
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
      updates.push(\`error_message = $\${paramIndex++}\`);
      values.push(errorMessage);
    }
    values.push(id);

    await this.query(
      \`UPDATE agent_updates SET \${updates.join(', ')} WHERE id = $\${paramIndex}\`,
      values
    );
  }

  // Get agent update history
  async getAgentUpdateHistory(agentId: string, limit: number = 20): Promise<any[]> {
    const result = await this.query(
      \`SELECT id, agent_id as "agentId", from_version as "fromVersion", to_version as "toVersion",
              platform, architecture, ip_address as "ipAddress", status, error_message as "errorMessage",
              created_at as "createdAt", completed_at as "completedAt"
       FROM agent_updates
       WHERE agent_id = $1
       ORDER BY created_at DESC
       LIMIT $2\`,
      [agentId, limit]
    );
    return result.rows;
  }

  // Update device agent version after successful update
  async updateDeviceAgentVersion(agentId: string, newVersion: string, oldVersion?: string): Promise<void> {
    await this.query(
      \`UPDATE devices SET
         previous_agent_version = COALESCE($3, agent_version),
         agent_version = $2,
         last_update_check = CURRENT_TIMESTAMP,
         updated_at = CURRENT_TIMESTAMP
       WHERE agent_id = $1\`,
      [agentId, newVersion, oldVersion]
    );
  }

  // Update last update check timestamp
  async updateDeviceLastUpdateCheck(agentId: string): Promise<void> {
    await this.query(
      \`UPDATE devices SET last_update_check = CURRENT_TIMESTAMP WHERE agent_id = $1\`,
      [agentId]
    );
  }

  // Get all agent releases
  async getAgentReleases(): Promise<any[]> {
    const result = await this.query(\`
      SELECT version, release_date as "releaseDate", changelog, is_required as "isRequired",
             platforms, min_version as "minVersion", created_at as "createdAt"
      FROM agent_releases
      ORDER BY string_to_array(version, '.')::int[] DESC
    \`);
    return result.rows;
  }

`;

  dbContent = dbContent.replace(closeMethMatch[0], updateMethods + closeMethMatch[0]);
  fs.writeFileSync(databasePath, dbContent);
  console.log('Added database methods for update tracking');
}

// ============================================================================
// 2. ADD VERSION CHECK AND UPDATE ENDPOINTS TO SERVER
// ============================================================================
const serverPath = 'D:/Projects/Sentinel/src/main/server.ts';
let serverContent = fs.readFileSync(serverPath, 'utf-8');

// Add crypto import at top
if (!serverContent.includes("import * as crypto from 'crypto'")) {
  serverContent = serverContent.replace(
    "import * as os from 'os';",
    "import * as os from 'os';\nimport * as crypto from 'crypto';"
  );
}

// Find where to add routes (after the /api/server/info route)
const serverInfoRoute = serverContent.indexOf("// Server info (for agents to discover WebSocket endpoint)");
const nextRouteBlock = serverContent.indexOf("}", serverContent.indexOf("});", serverInfoRoute) + 2);

const newRoutes = `

    // =========================================================================
    // AGENT UPDATE API ENDPOINTS
    // =========================================================================

    // Agent version check endpoint - agents poll this to check for updates
    this.app.get('/api/agent/version', async (req: Request, res: Response) => {
      try {
        const { platform, arch, current } = req.query as { platform?: string; arch?: string; current?: string };

        if (!platform || !current) {
          res.status(400).json({ error: 'Missing required parameters: platform, current' });
          return;
        }

        // Map platform names
        const normalizedPlatform = platform === 'darwin' ? 'macos' : platform;

        // Get latest release for this platform
        const latestRelease = await this.database.getLatestAgentRelease(normalizedPlatform);

        if (!latestRelease) {
          res.json({
            available: false,
            currentVersion: current,
            latestVersion: current,
            message: 'No releases available'
          });
          return;
        }

        // Compare versions
        const isNewer = this.compareVersions(latestRelease.version, current) > 0;

        if (!isNewer) {
          res.json({
            available: false,
            currentVersion: current,
            latestVersion: latestRelease.version
          });
          return;
        }

        // Build download URL
        const localIp = this.getLocalIpAddress();
        const downloadUrl = \`http://\${localIp}:\${this.port}/api/agent/update/download?platform=\${normalizedPlatform}&arch=\${arch || 'amd64'}\`;

        // Get binary info for checksum
        const binaryInfo = this.getAgentBinaryInfo(normalizedPlatform);

        res.json({
          available: true,
          currentVersion: current,
          latestVersion: latestRelease.version,
          versionInfo: {
            version: latestRelease.version,
            platform: normalizedPlatform,
            arch: arch || 'amd64',
            downloadUrl,
            checksum: binaryInfo?.checksum || '',
            size: binaryInfo?.size || 0,
            releaseDate: latestRelease.releaseDate,
            changelog: latestRelease.changelog,
            required: latestRelease.isRequired || false
          }
        });
      } catch (error) {
        console.error('Version check error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Agent update download endpoint
    this.app.get('/api/agent/update/download', async (req: Request, res: Response) => {
      try {
        const { platform, arch } = req.query as { platform?: string; arch?: string };

        if (!platform) {
          res.status(400).json({ error: 'Missing required parameter: platform' });
          return;
        }

        // Get binary filename
        let filename: string;
        switch (platform.toLowerCase()) {
          case 'windows':
            filename = 'sentinel-agent.exe';
            break;
          case 'macos':
            filename = 'sentinel-agent-macos';
            break;
          case 'linux':
            filename = 'sentinel-agent-linux';
            break;
          default:
            res.status(400).json({ error: 'Unsupported platform' });
            return;
        }

        // Get binary path
        const downloadsDir = electronApp.isPackaged
          ? path.join(process.resourcesPath, 'downloads')
          : path.join(__dirname, '..', '..', 'downloads');
        const binaryPath = path.join(downloadsDir, filename);

        if (!fs.existsSync(binaryPath)) {
          res.status(404).json({
            error: 'Agent binary not found',
            message: \`Binary not available for platform: \${platform}\`
          });
          return;
        }

        // Get latest version for header
        const latestRelease = await this.database.getLatestAgentRelease(platform);
        const agentVersion = latestRelease?.version || 'unknown';

        // Get file stats
        const stats = fs.statSync(binaryPath);

        // Log download attempt
        const agentId = req.headers['x-agent-id'] as string;
        const clientIp = req.ip || req.socket.remoteAddress || 'unknown';

        if (agentId) {
          const currentVersion = req.headers['x-current-version'] as string;
          await this.database.logAgentUpdate({
            agentId,
            fromVersion: currentVersion,
            toVersion: agentVersion,
            platform,
            architecture: arch as string,
            ipAddress: clientIp,
            status: 'downloading'
          });
        }

        // Send file
        res.setHeader('Content-Type', 'application/octet-stream');
        res.setHeader('Content-Disposition', \`attachment; filename="\${filename}"\`);
        res.setHeader('Content-Length', stats.size);
        res.setHeader('X-Agent-Version', agentVersion);

        const stream = fs.createReadStream(binaryPath);
        stream.pipe(res);
      } catch (error) {
        console.error('Update download error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Agent update status reporting endpoint
    this.app.post('/api/agent/update/status', async (req: Request, res: Response) => {
      try {
        const { agentId, status, fromVersion, toVersion, error: errorMessage } = req.body;

        if (!agentId || !status) {
          res.status(400).json({ error: 'Missing required fields: agentId, status' });
          return;
        }

        // Log the status update
        console.log(\`Agent \${agentId} update status: \${status} (\${fromVersion} -> \${toVersion})\`);

        // Find the most recent update record for this agent
        const updates = await this.database.getAgentUpdateHistory(agentId, 1);
        if (updates.length > 0) {
          await this.database.updateAgentUpdateStatus(updates[0].id, status, errorMessage);
        }

        // If completed successfully, update device version
        if (status === 'completed' && toVersion) {
          await this.database.updateDeviceAgentVersion(agentId, toVersion, fromVersion);
        }

        res.json({ success: true });
      } catch (error) {
        console.error('Update status error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get all agent releases (for admin UI)
    this.app.get('/api/agent/releases', async (req: Request, res: Response) => {
      try {
        const releases = await this.database.getAgentReleases();
        res.json(releases);
      } catch (error) {
        console.error('Get releases error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Create a new agent release
    this.app.post('/api/agent/releases', async (req: Request, res: Response) => {
      try {
        const release = await this.database.createAgentRelease(req.body);
        res.json(release);
      } catch (error) {
        console.error('Create release error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

`;

// Insert after the setupRoutes section, before setupWebSocket
const setupWebSocketMatch = serverContent.indexOf('private setupWebSocket(): void {');
if (setupWebSocketMatch > 0) {
  // Find a good insertion point - after the last route in setupRoutes
  const setupRoutesEnd = serverContent.lastIndexOf('}', serverContent.indexOf('private setupWebSocket'));

  // Add helper methods to the class
  const helperMethods = `

  // Compare semantic versions: returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal
  private compareVersions(v1: string, v2: string): number {
    const parts1 = v1.split('.').map(Number);
    const parts2 = v2.split('.').map(Number);

    for (let i = 0; i < Math.max(parts1.length, parts2.length); i++) {
      const p1 = parts1[i] || 0;
      const p2 = parts2[i] || 0;
      if (p1 > p2) return 1;
      if (p1 < p2) return -1;
    }
    return 0;
  }

  // Get agent binary info (checksum, size)
  private getAgentBinaryInfo(platform: string): { checksum: string; size: number } | null {
    let filename: string;
    switch (platform.toLowerCase()) {
      case 'windows':
        filename = 'sentinel-agent.exe';
        break;
      case 'macos':
        filename = 'sentinel-agent-macos';
        break;
      case 'linux':
        filename = 'sentinel-agent-linux';
        break;
      default:
        return null;
    }

    const downloadsDir = electronApp.isPackaged
      ? path.join(process.resourcesPath, 'downloads')
      : path.join(__dirname, '..', '..', 'downloads');
    const binaryPath = path.join(downloadsDir, filename);

    if (!fs.existsSync(binaryPath)) {
      return null;
    }

    const stats = fs.statSync(binaryPath);
    const fileBuffer = fs.readFileSync(binaryPath);
    const checksum = crypto.createHash('sha256').update(fileBuffer).digest('hex');

    return { checksum, size: stats.size };
  }

`;

  // Insert helper methods before setupWebSocket
  serverContent = serverContent.slice(0, setupWebSocketMatch) + helperMethods + serverContent.slice(setupWebSocketMatch);
}

// Now insert the routes at the end of setupRoutes
const routesInsertPoint = serverContent.indexOf('// Server info (for agents to discover WebSocket endpoint)');
const endOfServerInfoRoute = serverContent.indexOf('});', routesInsertPoint) + 4;

serverContent = serverContent.slice(0, endOfServerInfoRoute) + newRoutes + serverContent.slice(endOfServerInfoRoute);

fs.writeFileSync(serverPath, serverContent);
console.log('Added version check and update endpoints to server');

// ============================================================================
// 3. UPDATE MIGRATION TO ADD CURRENT VERSION
// ============================================================================
const migrationPath = 'D:/Projects/Sentinel/src/main/migrations/003_agent_updates.sql';
let migrationContent = fs.readFileSync(migrationPath, 'utf-8');

// Add version 1.11.0 if not present
if (!migrationContent.includes("1.11.0")) {
  const insertMatch = migrationContent.match(/ON CONFLICT \(version\) DO NOTHING;/);
  if (insertMatch) {
    const newVersions = `    ('1.9.0', '2024-12-09', '- Full system monitoring
- Protection manager
- Remote desktop support
- Auto-update framework', ARRAY['windows', 'linux', 'darwin']),
    ('1.10.0', '2024-12-10', '- Added ticketing system
- Bug fixes', ARRAY['windows', 'linux', 'darwin']),
    ('1.11.0', '2024-12-10', '- Automatic diagnostic collection on ticket creation
- System error logs collection
- Application logs collection
- Active programs tracking', ARRAY['windows', 'linux', 'darwin'])
`;
    migrationContent = migrationContent.replace(
      "    ('1.3.2',",
      newVersions + "    ('1.3.2',"
    );
    fs.writeFileSync(migrationPath, migrationContent);
    console.log('Updated migration with new version records');
  }
}

// ============================================================================
// 4. ADD PUSH UPDATE CAPABILITY TO AGENT MANAGER
// ============================================================================
const agentsPath = 'D:/Projects/Sentinel/src/main/agents.ts';
let agentsContent = fs.readFileSync(agentsPath, 'utf-8');

// Add push update method if not exists
if (!agentsContent.includes('triggerAgentUpdate')) {
  const lastClosingBrace = agentsContent.lastIndexOf('}');

  const pushUpdateMethod = `

  // Trigger an update on a specific agent (push update)
  async triggerAgentUpdate(deviceId: string): Promise<boolean> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    console.log(\`Triggering update check for device \${deviceId}\`);

    // Send update check command to agent
    const result = await this.sendRequest(device.agentId, {
      type: 'check_update',
      force: true, // Force immediate check
    }, 30000);

    return result?.success || false;
  }

  // Trigger updates on all connected agents
  async triggerFleetUpdate(): Promise<{ success: number; failed: number }> {
    let success = 0;
    let failed = 0;

    for (const [agentId, conn] of this.connections) {
      try {
        this.sendToAgent(agentId, {
          type: 'check_update',
          force: true,
        });
        success++;
      } catch (error) {
        console.error(\`Failed to trigger update for agent \${agentId}:\`, error);
        failed++;
      }
    }

    return { success, failed };
  }
`;

  agentsContent = agentsContent.slice(0, lastClosingBrace) + pushUpdateMethod + '\n' + agentsContent.slice(lastClosingBrace);
  fs.writeFileSync(agentsPath, agentsContent);
  console.log('Added push update methods to AgentManager');
}

console.log('\\n=== Server-side auto-update implementation complete ===');
console.log('Next: Update the Go agent with improved update logic');
