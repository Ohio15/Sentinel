import express, { Express, Request, Response } from 'express';
import { WebSocketServer, WebSocket } from 'ws';
import * as http from 'http';
import * as fs from 'fs';
import * as path from 'path';
import { v4 as uuidv4 } from 'uuid';
import { Database } from './database';
import { AgentManager } from './agents';
import * as os from 'os';
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

      // Look for binary in downloads directory (relative to app)
      const appPath = electronApp.isPackaged
        ? path.dirname(electronApp.getPath('exe'))
        : path.join(__dirname, '..', '..');
      const downloadsDir = path.join(appPath, 'downloads');
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

      // Get file stats for content-length
      const stats = fs.statSync(binaryPath);

      // Set headers for binary download
      res.setHeader('Content-Type', 'application/octet-stream');
      res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
      res.setHeader('Content-Length', stats.size);

      // Stream the file
      const fileStream = fs.createReadStream(binaryPath);
      fileStream.pipe(res);

      fileStream.on('error', (err) => {
        console.error('Error streaming agent binary:', err);
        if (!res.headersSent) {
          res.status(500).json({ error: 'Failed to download agent' });
        }
      });
    });

    // List available agent downloads
    this.app.get('/api/agent/downloads', (req: Request, res: Response) => {
      const appPath = electronApp.isPackaged
        ? path.dirname(electronApp.getPath('exe'))
        : path.join(__dirname, '..', '..');
      const downloadsDir = path.join(appPath, 'downloads');

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
    });
  }

  private setupWebSocket(): void {
    if (!this.server) return;

    this.wss = new WebSocketServer({ server: this.server, path: '/ws' });

    this.wss.on('connection', (ws: WebSocket, req) => {
      console.log('New WebSocket connection');

      let agentId: string | null = null;
      let authenticated = false;

      ws.on('message', async (data: Buffer) => {
        try {
          const message = JSON.parse(data.toString());

          // Handle authentication
          if (message.type === 'auth') {
            if (message.token === this.enrollmentToken || message.agentId) {
              agentId = message.agentId;
              authenticated = true;

              // Register agent connection
              if (agentId) {
                await this.agentManager.registerConnection(agentId, ws);
                await this.database.updateDeviceLastSeen(agentId);
              }

              ws.send(JSON.stringify({
                type: 'auth_response',
                success: true,
                timestamp: new Date().toISOString(),
              }));
            } else {
              ws.send(JSON.stringify({
                type: 'auth_response',
                success: false,
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
