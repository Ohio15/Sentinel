import express, { Express, Request, Response } from 'express';
import { WebSocketServer, WebSocket } from 'ws';
import * as http from 'http';
import * as fs from 'fs';
import * as path from 'path';
import { v4 as uuidv4 } from 'uuid';
import { Database } from './database';
import { AgentManager } from './agents';
import * as os from 'os';
import * as crypto from 'crypto';

// Helper function to embed configuration into agent binary
function embedConfigInBinary(binaryData: Buffer, serverUrl: string, token: string): Buffer {
  // The agent has placeholder strings that we replace:
  // SENTINEL_EMBEDDED_SERVER:________________________________________________________________:END (93 chars total)
  // SENTINEL_EMBEDDED_TOKEN:________________________________________________________________:END (92 chars total)

  const serverPlaceholder = 'SENTINEL_EMBEDDED_SERVER:' + '_'.repeat(64) + ':END';
  const tokenPlaceholder = 'SENTINEL_EMBEDDED_TOKEN:' + '_'.repeat(64) + ':END';

  // Pad values to exactly 64 characters
  const paddedServer = serverUrl.padEnd(64, '_').substring(0, 64);
  const paddedToken = token.padEnd(64, '_').substring(0, 64);

  const serverReplacement = 'SENTINEL_EMBEDDED_SERVER:' + paddedServer + ':END';
  const tokenReplacement = 'SENTINEL_EMBEDDED_TOKEN:' + paddedToken + ':END';

  // Convert buffer to string for replacement (binary-safe using latin1)
  let binaryStr = binaryData.toString('latin1');

  // Replace placeholders
  binaryStr = binaryStr.replace(serverPlaceholder, serverReplacement);
  binaryStr = binaryStr.replace(tokenPlaceholder, tokenReplacement);

  return Buffer.from(binaryStr, 'latin1');
}
import { app as electronApp } from 'electron';

export class Server {
  private app: Express;
  private server: http.Server | null = null;
  private wss: WebSocketServer | null = null;
  private database: Database;
  private agentManager: AgentManager;
  private port: number = 8080;
  private enrollmentToken: string = '';

  constructor(database: Database, agentManager: AgentManager) {
    this.database = database;
    this.agentManager = agentManager;
    this.app = express();
    this.setupMiddleware();
    this.setupRoutes();
  }

  private setupMiddleware(): void {
    this.app.use(express.json());
    this.app.use(express.urlencoded({ extended: true }));

    // CORS for local development
    this.app.use((req, res, next) => {
      res.header('Access-Control-Allow-Origin', '*');
      res.header('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS');
      res.header('Access-Control-Allow-Headers', 'Content-Type, Authorization, X-Enrollment-Token');
      if (req.method === 'OPTIONS') {
        res.sendStatus(200);
      } else {
        next();
      }
    });
  }

  private setupRoutes(): void {
    // Health check
    this.app.get('/health', (req: Request, res: Response) => {
      res.json({ status: 'ok', timestamp: new Date().toISOString() });
    });

    // Agent enrollment
    this.app.post('/api/agent/enroll', async (req: Request, res: Response) => {
      const token = req.headers['x-enrollment-token'];
      if (token !== this.enrollmentToken) {
        res.status(401).json({ error: 'Invalid enrollment token' });
        return;
      }

      const agentInfo = req.body;
      if (!agentInfo.agentId) {
        res.status(400).json({ error: 'Agent ID required' });
        return;
      }

      const device = await this.database.createOrUpdateDevice(agentInfo);
      res.json({
        success: true,
        deviceId: device.id,
        config: {
          heartbeatInterval: 30,
          metricsInterval: 60,
        },
      });
    });

    // Agent download - serve actual binaries
    this.app.get('/api/agent/download/:platform', (req: Request, res: Response) => {
      const { platform } = req.params;

      // Determine binary filename based on platform
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

      // Look for binary in downloads directory (uses resources folder when packaged)
      const downloadsDir = electronApp.isPackaged
        ? path.join(process.resourcesPath, 'downloads')
        : path.join(__dirname, '..', '..', 'downloads');
      const binaryPath = path.join(downloadsDir, filename);

      // Check if binary exists
      if (!fs.existsSync(binaryPath)) {
        res.status(404).json({
          error: 'Agent binary not found',
          message: `Please build the agent using: cd agent && .\\build.ps1 -Platform ${platform}`,
          expectedPath: binaryPath
        });
        return;
      }

      // Read binary and embed configuration
      const binaryData = fs.readFileSync(binaryPath);
      const localIp = this.getLocalIpAddress();
      const serverUrl = `http://${localIp}:${this.port}`;

      // Embed server URL and enrollment token into binary
      const modifiedBinary = embedConfigInBinary(binaryData, serverUrl, this.enrollmentToken);

      // Set headers for binary download
      res.setHeader('Content-Type', 'application/octet-stream');
      res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
      res.setHeader('Content-Length', modifiedBinary.length);

      // Send the modified binary
      res.send(modifiedBinary);
    });

    // List available agent downloads
    this.app.get('/api/agent/downloads', (req: Request, res: Response) => {
      const downloadsDir = electronApp.isPackaged
        ? path.join(process.resourcesPath, 'downloads')
        : path.join(__dirname, '..', '..', 'downloads');

      if (!fs.existsSync(downloadsDir)) {
        res.json({ agents: [], message: 'No agents built yet. Run: cd agent && .\\build.ps1 -Platform all' });
        return;
      }

      const files = fs.readdirSync(downloadsDir)
        .filter(f => f.startsWith('sentinel-agent'))
        .map(filename => {
          const filePath = path.join(downloadsDir, filename);
          const stats = fs.statSync(filePath);
          return {
            filename,
            size: stats.size,
            modified: stats.mtime,
            platform: filename.includes('macos') ? 'macos' :
                     filename.includes('linux') ? 'linux' : 'windows'
          };
        });

      res.json({ agents: files });
    });

    // Server info (for agents to discover WebSocket endpoint)
    this.app.get('/api/server/info', (req: Request, res: Response) => {
      const localIp = this.getLocalIpAddress();
      res.json({
        wsEndpoint: `ws://${localIp}:${this.port}/ws`,
        version: '1.0.0',
      });


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
        const downloadUrl = `http://${localIp}:${this.port}/api/agent/update/download?platform=${normalizedPlatform}&arch=${arch || 'amd64'}`;

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
            message: `Binary not available for platform: ${platform}`
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
        res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
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
        console.log(`Agent ${agentId} update status: ${status} (${fromVersion} -> ${toVersion})`);

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

    });
  }

  

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

private setupWebSocket(): void {
    if (!this.server) return;

    // Create WebSocket server without path filter - we'll handle paths manually
    this.wss = new WebSocketServer({ noServer: true });

    // Handle upgrade requests to support multiple paths
    this.server.on('upgrade', (request, socket, head) => {
      const pathname = new URL(request.url || '', `http://${request.headers.host}`).pathname;

      // Accept /ws, /ws/agent for agents, and /ws/dashboard for dashboard
      const isDashboard = pathname === '/ws/dashboard';
      if (pathname === '/ws' || pathname === '/ws/agent' || isDashboard) {
        this.wss!.handleUpgrade(request, socket, head, (ws) => {
          this.wss!.emit('connection', ws, request);
        });
      } else {
        socket.destroy();
      }
    });

    this.wss.on('connection', (ws: WebSocket, req) => {
      const url = new URL(req.url || '', `http://${req.headers.host}`);
      const pathname = url.pathname;
      const isDashboardConnection = pathname === '/ws/dashboard';

      console.log('New WebSocket connection from path:', req.url, 'isDashboard:', isDashboardConnection);

      let agentId: string | null = null;
      let authenticated = isDashboardConnection; // Dashboard connections are authenticated via token in URL

      // For dashboard connections, store the ws for sending responses
      let dashboardWs: WebSocket | null = isDashboardConnection ? ws : null;

      ws.on('message', async (data: Buffer) => {
        try {
          const message = JSON.parse(data.toString());

          // Handle dashboard messages
          if (isDashboardConnection) {
            await this.handleDashboardMessage(message, ws);
            return;
          }

          // Handle authentication - support both direct fields and payload format
          if (message.type === 'auth') {
            console.log('=== AUTH MESSAGE RECEIVED ===');
            console.log('Raw auth message:', JSON.stringify(message, null, 2));
            // Extract agentId and token from either top-level or payload (agent sends in payload)
            const authAgentId = message.agentId || message.payload?.agentId;
            const authToken = message.token || message.payload?.token;

            if (authToken === this.enrollmentToken || authAgentId) {
              agentId = authAgentId;
              authenticated = true;

              // Register agent connection
              if (agentId) {
                await this.agentManager.registerConnection(agentId, ws);
                await this.database.updateDeviceLastSeen(agentId);
                console.log(`Agent ${agentId} authenticated and registered`);
              }

              ws.send(JSON.stringify({
                type: 'auth_response',
                success: true,
                payload: { success: true },
                timestamp: new Date().toISOString(),
              }));
            } else {
              ws.send(JSON.stringify({
                type: 'auth_response',
                success: false,
                payload: { success: false, error: 'Invalid credentials' },
                error: 'Invalid credentials',
              }));
              ws.close();
            }
            return;
          }

          // Require authentication for other messages
          if (!authenticated) {
            ws.send(JSON.stringify({
              type: 'error',
              error: 'Not authenticated',
            }));
            return;
          }

          // Route message to agent manager
          await this.agentManager.handleMessage(agentId!, message, ws);

        } catch (error) {
          console.error('Error processing WebSocket message:', error);
          ws.send(JSON.stringify({
            type: 'error',
            error: 'Invalid message format',
          }));
        }
      });

      ws.on('close', async () => {
        if (agentId) {
          this.agentManager.unregisterConnection(agentId);
          await this.database.updateDeviceStatus(agentId, 'offline');
          console.log(`Agent ${agentId} disconnected`);
        }
        console.log('WebSocket connection closed');
      });

      ws.on('error', (error) => {
        console.error('WebSocket error:', error);
      });

      // Send initial handshake request
      ws.send(JSON.stringify({
        type: 'handshake',
        timestamp: new Date().toISOString(),
      }));
    });
  }

  private async handleDashboardMessage(message: any, ws: WebSocket): Promise<void> {
    console.log('Dashboard message received:', message.type, message.payload);

    try {
      switch (message.type) {
        case 'start_terminal': {
          const { deviceId, agentId, sessionId, cols, rows } = message.payload || {};
          console.log('Starting terminal for device:', deviceId, 'agent:', agentId);

          // Check if agent is connected
          if (!this.agentManager.isAgentConnected(agentId)) {
            console.log('Agent not connected:', agentId);
            ws.send(JSON.stringify({
              type: 'error',
              payload: { error: 'Agent not connected', sessionId }
            }));
            return;
          }

          // Forward to agent
          this.agentManager.sendToAgent(agentId, {
            type: 'start_terminal',
            sessionId,
            cols,
            rows,
          });

          // Register this dashboard ws for terminal output
          this.agentManager.registerDashboardSession(sessionId, ws);

          ws.send(JSON.stringify({
            type: 'terminal_started',
            payload: { sessionId }
          }));
          break;
        }

        case 'terminal_input': {
          const { agentId, sessionId, data } = message.payload || {};
          if (this.agentManager.isAgentConnected(agentId)) {
            this.agentManager.sendToAgent(agentId, {
              type: 'terminal_input',
              sessionId,
              data,
            });
          }
          break;
        }

        case 'terminal_resize': {
          const { agentId, sessionId, cols, rows } = message.payload || {};
          if (this.agentManager.isAgentConnected(agentId)) {
            this.agentManager.sendToAgent(agentId, {
              type: 'terminal_resize',
              sessionId,
              cols,
              rows,
            });
          }
          break;
        }

        case 'close_terminal': {
          const { agentId, sessionId } = message.payload || {};
          this.agentManager.unregisterDashboardSession(sessionId);
          if (this.agentManager.isAgentConnected(agentId)) {
            this.agentManager.sendToAgent(agentId, {
              type: 'close_terminal',
              sessionId,
            });
          }
          break;
        }

        default:
          console.log('Unknown dashboard message type:', message.type);
      }
    } catch (error) {
      console.error('Error handling dashboard message:', error);
      ws.send(JSON.stringify({
        type: 'error',
        payload: { error: String(error) }
      }));
    }
  }

  private getLocalIpAddress(): string {
    const interfaces = os.networkInterfaces();
    for (const name of Object.keys(interfaces)) {
      for (const iface of interfaces[name] || []) {
        if (iface.family === 'IPv4' && !iface.internal) {
          return iface.address;
        }
      }
    }
    return '127.0.0.1';
  }

  async start(): Promise<void> {
    // Load settings
    const settings = await this.database.getSettings();
    this.port = settings.serverPort || 8080;
    this.enrollmentToken = settings.enrollmentToken || uuidv4();

    // Update token if not set
    if (!settings.enrollmentToken) {
      await this.database.updateSettings({ enrollmentToken: this.enrollmentToken });
    }

    return new Promise((resolve, reject) => {
      this.server = this.app.listen(this.port, '0.0.0.0', () => {
        console.log(`Server listening on port ${this.port}`);
        this.setupWebSocket();
        resolve();
      });

      this.server.on('error', async (error: NodeJS.ErrnoException) => {
        if (error.code === 'EADDRINUSE') {
          console.error(`Port ${this.port} is already in use`);
          // Try next port
          this.port++;
          await this.database.updateSettings({ serverPort: this.port });
          this.server?.close();
          this.start().then(resolve).catch(reject);
        } else {
          reject(error);
        }
      });
    });
  }

  async stop(): Promise<void> {
    return new Promise((resolve) => {
      this.wss?.close();
      this.server?.close(() => {
        console.log('Server stopped');
        resolve();
      });
    });
  }

  getPort(): number {
    return this.port;
  }

  getEnrollmentToken(): string {
    return this.enrollmentToken;
  }

  async regenerateEnrollmentToken(): Promise<string> {
    this.enrollmentToken = uuidv4();
    await this.database.updateSettings({ enrollmentToken: this.enrollmentToken });
    return this.enrollmentToken;
  }

  getAgentInstallerCommand(platform: string): string {
    const localIp = this.getLocalIpAddress();
    const serverUrl = `http://${localIp}:${this.port}`;

    switch (platform.toLowerCase()) {
      case 'windows':
        // Download to temp directory to avoid permission issues and ensure clean execution
        return `powershell -ExecutionPolicy Bypass -Command "& { $ErrorActionPreference='Stop'; $agentPath = Join-Path $env:TEMP 'sentinel-agent.exe'; Write-Host 'Downloading agent...'; Invoke-WebRequest -Uri '${serverUrl}/api/agent/download/windows' -OutFile $agentPath -UseBasicParsing; Write-Host 'Installing agent...'; Start-Process -FilePath $agentPath -ArgumentList '--install','--server=${serverUrl}','--token=${this.enrollmentToken}' -Verb RunAs -Wait }"`;

      case 'macos':
        return `curl -sL "${serverUrl}/api/agent/download/macos" -o sentinel-agent && chmod +x sentinel-agent && sudo ./sentinel-agent --server=${serverUrl} --token=${this.enrollmentToken}`;

      case 'linux':
        return `curl -sL "${serverUrl}/api/agent/download/linux" -o sentinel-agent && chmod +x sentinel-agent && sudo ./sentinel-agent --server=${serverUrl} --token=${this.enrollmentToken}`;

      default:
        return 'Platform not supported';
    }
  }
}
