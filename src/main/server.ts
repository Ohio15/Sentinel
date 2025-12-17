import express, { Express, Request, Response, NextFunction } from 'express';
import { WebSocketServer, WebSocket } from 'ws';
import * as http from 'http';
import * as fs from 'fs';
import * as path from 'path';
import { v4 as uuidv4 } from 'uuid';
import { Database } from './database';
import { AgentManager } from './agents';
import * as os from 'os';
import * as crypto from 'crypto';

// Rate limiter class
class RateLimiter {
  private requests: Map<string, { count: number; resetTime: number }> = new Map();
  private cleanupInterval: NodeJS.Timeout;

  constructor() {
    // Clean up expired entries every minute
    this.cleanupInterval = setInterval(() => this.cleanup(), 60000);
  }

  private cleanup(): void {
    const now = Date.now();
    for (const [key, value] of this.requests.entries()) {
      if (now > value.resetTime) {
        this.requests.delete(key);
      }
    }
  }

  checkLimit(identifier: string, maxRequests: number, windowMs: number): { allowed: boolean; remaining: number; resetTime: number } {
    const now = Date.now();
    const record = this.requests.get(identifier);

    if (!record || now > record.resetTime) {
      // New window
      const resetTime = now + windowMs;
      this.requests.set(identifier, { count: 1, resetTime });
      return { allowed: true, remaining: maxRequests - 1, resetTime };
    }

    // Existing window
    if (record.count >= maxRequests) {
      return { allowed: false, remaining: 0, resetTime: record.resetTime };
    }

    record.count++;
    return { allowed: true, remaining: maxRequests - record.count, resetTime: record.resetTime };
  }

  destroy(): void {
    clearInterval(this.cleanupInterval);
  }
}

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
  private port: number = 8081;
  private enrollmentToken: string = '';
  private dashboardToken: string = '';
  private rateLimiter: RateLimiter;
  private allowedOrigins: string[] = [];

  constructor(database: Database, agentManager: AgentManager) {
    this.database = database;
    this.agentManager = agentManager;
    this.app = express();
    this.rateLimiter = new RateLimiter();
    this.setupMiddleware();
    this.setupRoutes();
  }

  private setupMiddleware(): void {
    this.app.use(express.json());
    this.app.use(express.urlencoded({ extended: true }));

    // CORS middleware with origin validation
    this.app.use((req: Request, res: Response, next: NextFunction) => {
      const origin = req.headers.origin;

      // Check if origin is allowed
      const isAllowed = this.isOriginAllowed(origin);

      if (isAllowed && origin) {
        res.header('Access-Control-Allow-Origin', origin);
        res.header('Access-Control-Allow-Credentials', 'true');
      }

      res.header('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS');
      res.header('Access-Control-Allow-Headers', 'Content-Type, Authorization, X-Enrollment-Token');

      if (req.method === 'OPTIONS') {
        res.sendStatus(200);
      } else {
        next();
      }
    });
  }

  // Check if origin is allowed based on whitelist
  private isOriginAllowed(origin: string | undefined): boolean {
    if (!origin) return true; // Allow requests without origin (like curl)

    // If no specific origins configured, allow localhost on any port
    if (this.allowedOrigins.length === 0) {
      return origin.match(/^http:\/\/localhost(:\d+)?$/) !== null ||
             origin.match(/^http:\/\/127\.0\.0\.1(:\d+)?$/) !== null;
    }

    // Check against whitelist
    for (const allowed of this.allowedOrigins) {
      if (allowed === origin) return true;

      // Support wildcard port: http://localhost:*
      if (allowed.endsWith(':*')) {
        const baseOrigin = allowed.slice(0, -2);
        if (origin.startsWith(baseOrigin)) return true;
      }
    }

    return false;
  }

  // Session-based authentication middleware
  private requireAuth(req: Request, res: Response, next: NextFunction): void {
    const authHeader = req.headers.authorization;
    const token = authHeader?.startsWith('Bearer ') ? authHeader.slice(7) : null;

    if (!token || (token !== this.enrollmentToken && token !== this.dashboardToken)) {
      res.status(401).json({ error: 'Unauthorized' });
      return;
    }

    next();
  }

  private setupRoutes(): void {
    // Health check
    this.app.get('/health', (req: Request, res: Response) => {
      res.json({ status: 'ok', timestamp: new Date().toISOString() });
    });

    // Agent enrollment with rate limiting
    this.app.post('/api/agent/enroll', async (req: Request, res: Response) => {
      const clientIp = req.ip || req.socket.remoteAddress || 'unknown';

      // Rate limit: 5 requests per minute per IP
      const enrollLimit = this.rateLimiter.checkLimit(`enroll:${clientIp}`, 5, 60000);
      res.header('X-RateLimit-Remaining', enrollLimit.remaining.toString());
      res.header('X-RateLimit-Reset', new Date(enrollLimit.resetTime).toISOString());

      if (!enrollLimit.allowed) {
        res.status(429).json({
          error: 'Too many enrollment requests',
          retryAfter: Math.ceil((enrollLimit.resetTime - Date.now()) / 1000)
        });
        return;
      }

      const token = req.headers['x-enrollment-token'];
      if (token !== this.enrollmentToken) {
        // Track auth failures
        const authLimit = this.rateLimiter.checkLimit(`auth:${clientIp}`, 10, 60000);
        if (!authLimit.allowed) {
          res.status(429).json({
            error: 'Too many authentication failures',
            retryAfter: Math.ceil((authLimit.resetTime - Date.now()) / 1000)
          });
          return;
        }

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

    // List available agent downloads (requires authentication)
    this.app.get('/api/agent/downloads', this.requireAuth.bind(this), (req: Request, res: Response) => {
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

    // Create a new agent release (requires authentication)
    this.app.post('/api/agent/releases', this.requireAuth.bind(this), async (req: Request, res: Response) => {
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

  // Timing-safe comparison to prevent timing attacks
  private timingSafeEqual(a: Buffer, b: Buffer): boolean {
    try {
      // Ensure buffers are the same length by padding
      const maxLen = Math.max(a.length, b.length);
      const bufA = Buffer.alloc(maxLen);
      const bufB = Buffer.alloc(maxLen);
      a.copy(bufA);
      b.copy(bufB);
      return crypto.timingSafeEqual(bufA, bufB);
    } catch {
      return false;
    }
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
      const url = new URL(request.url || '', `http://${request.headers.host}`);
      const pathname = url.pathname;

      // Accept /ws, /ws/agent for agents, and /ws/dashboard for dashboard
      const isDashboard = pathname === '/ws/dashboard';

      // Validate dashboard token in URL
      if (isDashboard) {
        const token = url.searchParams.get('token');
        if (token !== this.dashboardToken) {
          console.log('Dashboard connection rejected: invalid token');
          socket.write('HTTP/1.1 401 Unauthorized\r\n\r\n');
          socket.destroy();
          return;
        }
      }

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
      // Dashboard connections are pre-authenticated via token in URL
      let authenticated = isDashboardConnection;

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
            // Extract agentId and token from either top-level or payload (agent sends in payload)
            const authAgentId = message.agentId || message.payload?.agentId;
            const authToken = message.token || message.payload?.token;

            // Use timing-safe comparison for token validation to prevent timing attacks
            const isValidToken = authToken && this.timingSafeEqual(
              Buffer.from(authToken, 'utf8'),
              Buffer.from(this.enrollmentToken, 'utf8')
            );

            if (isValidToken || authAgentId) {
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
    this.port = settings.serverPort || 8081;
    this.enrollmentToken = settings.enrollmentToken || uuidv4();
    this.dashboardToken = settings.dashboardToken || uuidv4();

    // Update tokens if not set
    const updates: any = {};
    if (!settings.enrollmentToken) {
      updates.enrollmentToken = this.enrollmentToken;
    }
    if (!settings.dashboardToken) {
      updates.dashboardToken = this.dashboardToken;
      console.log(`Generated new dashboard token: ${this.dashboardToken}`);
    }
    if (Object.keys(updates).length > 0) {
      await this.database.updateSettings(updates);
    }

    // Configure allowed origins from environment variable
    const corsOrigins = process.env.CORS_ORIGINS;
    if (corsOrigins) {
      this.allowedOrigins = corsOrigins.split(',').map(o => o.trim());
      console.log('CORS allowed origins:', this.allowedOrigins);
    } else {
      console.log('CORS: Using default (localhost:* only)');
    }

    return new Promise((resolve, reject) => {
      this.server = this.app.listen(this.port, '0.0.0.0', () => {
        console.log(`Server listening on port ${this.port}`);
        console.log(`Dashboard WebSocket: ws://localhost:${this.port}/ws/dashboard?token=${this.dashboardToken}`);
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
      this.rateLimiter.destroy();
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

  getDashboardToken(): string {
    return this.dashboardToken;
  }

  async regenerateEnrollmentToken(): Promise<string> {
    this.enrollmentToken = uuidv4();
    await this.database.updateSettings({ enrollmentToken: this.enrollmentToken });
    return this.enrollmentToken;
  }

  async regenerateDashboardToken(): Promise<string> {
    this.dashboardToken = uuidv4();
    await this.database.updateSettings({ dashboardToken: this.dashboardToken });
    console.log(`Regenerated dashboard token: ${this.dashboardToken}`);
    return this.dashboardToken;
  }

  getAgentInstallerCommand(platform: string): string {
    const localIp = this.getLocalIpAddress();
    const serverUrl = `http://${localIp}:${this.port}`;

    // Note: The downloaded agent binary already has the server URL and enrollment token
    // embedded in it, so we don't need to pass them as command-line arguments.
    // This prevents token exposure in process lists and command history.

    switch (platform.toLowerCase()) {
      case 'windows':
        // Download agent with embedded config and install
        return `powershell -ExecutionPolicy Bypass -Command "& { $ErrorActionPreference='Stop'; $agentPath = Join-Path $env:TEMP 'sentinel-agent.exe'; Write-Host 'Downloading agent...'; Invoke-WebRequest -Uri '${serverUrl}/api/agent/download/windows' -OutFile $agentPath -UseBasicParsing; Write-Host 'Installing agent...'; Start-Process -FilePath $agentPath -ArgumentList '--install' -Verb RunAs -Wait; Write-Host 'Agent installed successfully.' }"`;

      case 'macos':
        // Download agent with embedded config and install
        return `curl -sL "${serverUrl}/api/agent/download/macos" -o sentinel-agent && chmod +x sentinel-agent && sudo ./sentinel-agent --install`;

      case 'linux':
        // Download agent with embedded config and install
        return `curl -sL "${serverUrl}/api/agent/download/linux" -o sentinel-agent && chmod +x sentinel-agent && sudo ./sentinel-agent --install`;

      default:
        return 'Platform not supported';
    }
  }
}
