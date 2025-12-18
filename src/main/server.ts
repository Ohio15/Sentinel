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
import cookieParser from 'cookie-parser';
import { msalAuth, MSALConfig } from './auth/msal';
import { EmailService, EmailConfig } from './notifications/email';

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
  private emailService: EmailService;

  constructor(database: Database, agentManager: AgentManager) {
    this.database = database;
    this.agentManager = agentManager;
    this.app = express();
    this.rateLimiter = new RateLimiter();
    this.emailService = new EmailService(database);
    this.setupMiddleware();
    this.setupRoutes();
  }

  private setupMiddleware(): void {
    this.app.use(express.json());
    this.app.use(express.urlencoded({ extended: true }));
    this.app.use(cookieParser());

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

    // ============================================================================
    // Update Groups API
    // ============================================================================

    // List all update groups
    this.app.get('/api/update-groups', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const groups = await this.database.getUpdateGroups();
        res.json(groups);
      } catch (error) {
        console.error('Get update groups error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Create a new update group
    this.app.post('/api/update-groups', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id, name, priority, autoPromote, successThresholdPercent, failureThresholdPercent, minDevicesForDecision, waitTimeMinutes } = req.body;
        await this.database.createUpdateGroup({
          id: id || uuidv4(),
          name,
          priority: priority || 0,
          autoPromote: autoPromote ?? false,
          successThresholdPercent: successThresholdPercent || 95,
          failureThresholdPercent: failureThresholdPercent || 10,
          minDevicesForDecision: minDevicesForDecision || 3,
          waitTimeMinutes: waitTimeMinutes || 60
        });
        res.json({ success: true });
      } catch (error) {
        console.error('Create update group error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Assign device to update group
    this.app.post('/api/devices/:id/update-group', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const { groupId } = req.body;
        await this.database.assignDeviceToUpdateGroup(id, groupId || null);
        res.json({ success: true });
      } catch (error) {
        console.error('Assign device to group error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // ============================================================================
    // Agent Health API
    // ============================================================================

    // Get all agent health scores
    this.app.get('/api/health/agents', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const health = await this.database.getAllAgentHealth();
        res.json(health);
      } catch (error) {
        console.error('Get all agent health error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get specific device health
    this.app.get('/api/devices/:id/health', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const health = await this.database.getAgentHealth(id);
        if (!health) {
          res.status(404).json({ error: 'Health data not found' });
          return;
        }
        res.json(health);
      } catch (error) {
        console.error('Get device health error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get device health history
    this.app.get('/api/devices/:id/health/history', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const hours = parseInt(req.query.hours as string) || 24;
        const history = await this.database.getAgentHealthHistory(id, hours);
        res.json(history);
      } catch (error) {
        console.error('Get device health history error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // ============================================================================
    // Command Queue API
    // ============================================================================

    // Get queued commands for a device
    this.app.get('/api/devices/:id/queue', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const commands = await this.database.getPendingCommandsForDevice(id);
        res.json(commands);
      } catch (error) {
        console.error('Get device queue error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Queue a command for a device
    this.app.post('/api/devices/:id/queue', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const { commandType, payload, priority, expiresInMinutes } = req.body;

        await this.database.queueCommand({
          id: uuidv4(),
          deviceId: id,
          commandType,
          payload,
          priority: priority || 50,
          expiresAt: expiresInMinutes ? new Date(Date.now() + expiresInMinutes * 60000) : undefined
        });

        res.json({ success: true });
      } catch (error) {
        console.error('Queue command error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // ============================================================================
    // Rollouts API
    // ============================================================================

    // List all rollouts
    this.app.get('/api/rollouts', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const limit = parseInt(req.query.limit as string) || 50;
        const rollouts = await this.database.getRollouts(limit);
        res.json(rollouts);
      } catch (error) {
        console.error('Get rollouts error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get rollout details
    this.app.get('/api/rollouts/:id', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const rollouts = await this.database.getRollouts(100);
        const rollout = rollouts.find(r => r.id === id);
        if (!rollout) {
          res.status(404).json({ error: 'Rollout not found' });
          return;
        }
        const stages = await this.database.getRolloutStages(id);
        const events = await this.database.getRolloutEvents(id);
        res.json({ ...rollout, stages, events });
      } catch (error) {
        console.error('Get rollout error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get rollout stages
    this.app.get('/api/rollouts/:id/stages', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const stages = await this.database.getRolloutStages(id);
        res.json(stages);
      } catch (error) {
        console.error('Get rollout stages error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get rollout devices
    this.app.get('/api/rollouts/:id/devices', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const stageId = req.query.stageId as string | undefined;
        const devices = await this.database.getRolloutDevices(id, stageId);
        res.json(devices);
      } catch (error) {
        console.error('Get rollout devices error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // Get rollout events
    this.app.get('/api/rollouts/:id/events', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const events = await this.database.getRolloutEvents(id);
        res.json(events);
      } catch (error) {
        console.error('Get rollout events error:', error);
        res.status(500).json({ error: 'Internal server error' });
      }
    });

    // =========================================================================
    // Support Portal API - Authentication
    // =========================================================================

    // Portal login - redirect to Microsoft
    this.app.get('/portal/auth/login', async (req: Request, res: Response) => {
      try {
        if (!msalAuth.isConfigured()) {
          res.status(503).json({ error: 'Portal authentication not configured' });
          return;
        }

        const redirectTo = req.query.redirect as string || '/portal/tickets';
        const authUrl = await msalAuth.getAuthCodeUrl(redirectTo);
        res.redirect(authUrl);
      } catch (error) {
        console.error('Portal login error:', error);
        res.status(500).json({ error: 'Authentication error' });
      }
    });

    // OAuth callback from Microsoft
    this.app.get('/portal/auth/callback', async (req: Request, res: Response) => {
      try {
        const { code, state, error, error_description } = req.query;

        if (error) {
          console.error('OAuth error:', error, error_description);
          res.redirect(`/portal?error=${encodeURIComponent(error_description as string || 'Authentication failed')}`);
          return;
        }

        if (!code || !state) {
          res.redirect('/portal?error=Invalid%20callback');
          return;
        }

        // Exchange code for tokens
        const tokenResponse = await msalAuth.acquireTokenByCode(code as string, state as string);

        // Look up tenant mapping
        const tenantMapping = await this.database.getClientTenantByTenantId(tokenResponse.account.tenantId);

        if (!tenantMapping) {
          console.log('Unmapped tenant attempted login:', tokenResponse.account.tenantId);
          res.redirect('/portal?error=Your%20organization%20is%20not%20registered%20with%20this%20support%20portal');
          return;
        }

        if (!tenantMapping.enabled) {
          res.redirect('/portal?error=Your%20organization%20access%20has%20been%20disabled');
          return;
        }

        // Create session
        const session = await this.database.createPortalSession({
          userEmail: tokenResponse.account.username,
          userName: tokenResponse.account.name,
          tenantId: tokenResponse.account.tenantId,
          clientId: tenantMapping.clientId,
          accessToken: tokenResponse.accessToken,
          idToken: tokenResponse.idToken,
          expiresAt: tokenResponse.expiresOn,
        });

        // Set session cookie
        res.cookie('portal_session', session.id, {
          httpOnly: true,
          secure: process.env.NODE_ENV === 'production',
          sameSite: 'lax',
          maxAge: 24 * 60 * 60 * 1000, // 24 hours
        });

        // Redirect to tickets page
        res.redirect('/portal/tickets');
      } catch (error: any) {
        console.error('Portal callback error:', error);
        res.redirect(`/portal?error=${encodeURIComponent(error.message || 'Authentication failed')}`);
      }
    });

    // Portal logout
    this.app.post('/portal/auth/logout', async (req: Request, res: Response) => {
      try {
        const sessionId = req.cookies?.portal_session;
        if (sessionId) {
          await this.database.deletePortalSession(sessionId);
        }
        res.clearCookie('portal_session');
        res.json({ success: true });
      } catch (error) {
        console.error('Portal logout error:', error);
        res.status(500).json({ error: 'Logout failed' });
      }
    });

    // Get current user
    this.app.get('/portal/auth/me', async (req: Request, res: Response) => {
      try {
        const sessionId = req.cookies?.portal_session;
        if (!sessionId) {
          res.status(401).json({ error: 'Not authenticated' });
          return;
        }

        const session = await this.database.getPortalSession(sessionId);
        if (!session) {
          res.clearCookie('portal_session');
          res.status(401).json({ error: 'Session expired' });
          return;
        }

        // Update activity
        await this.database.updatePortalSessionActivity(sessionId);

        res.json({
          email: session.userEmail,
          name: session.userName,
          clientId: session.clientId,
        });
      } catch (error) {
        console.error('Portal auth me error:', error);
        res.status(500).json({ error: 'Failed to get user info' });
      }
    });

    // =========================================================================
    // Support Portal API - Tickets
    // =========================================================================

    // Middleware to require portal session
    const requirePortalAuth = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
      const sessionId = req.cookies?.portal_session;
      if (!sessionId) {
        res.status(401).json({ error: 'Not authenticated' });
        return;
      }

      const session = await this.database.getPortalSession(sessionId);
      if (!session) {
        res.clearCookie('portal_session');
        res.status(401).json({ error: 'Session expired' });
        return;
      }

      // Attach session to request
      (req as any).portalSession = session;
      await this.database.updatePortalSessionActivity(sessionId);
      next();
    };

    // List user's tickets
    this.app.get('/portal/api/tickets', requirePortalAuth, async (req: Request, res: Response) => {
      try {
        const session = (req as any).portalSession;
        const tickets = await this.database.getTicketsBySubmitter(session.userEmail);
        res.json(tickets);
      } catch (error) {
        console.error('Portal get tickets error:', error);
        res.status(500).json({ error: 'Failed to get tickets' });
      }
    });

    // Get single ticket
    this.app.get('/portal/api/tickets/:id', requirePortalAuth, async (req: Request, res: Response) => {
      try {
        const session = (req as any).portalSession;
        const { id } = req.params;

        const ticket = await this.database.getTicket(id);
        if (!ticket) {
          res.status(404).json({ error: 'Ticket not found' });
          return;
        }

        // Verify user has access (either submitter or same client)
        const isSubmitter = ticket.submitterEmail === session.userEmail ||
                           ticket.requesterEmail === session.userEmail;
        const isSameClient = ticket.clientId === session.clientId;

        if (!isSubmitter && !isSameClient) {
          res.status(403).json({ error: 'Access denied' });
          return;
        }

        // Get comments (exclude internal)
        const comments = await this.database.getTicketComments(id);
        const publicComments = comments.filter((c: any) => !c.isInternal);

        res.json({ ...ticket, comments: publicComments });
      } catch (error) {
        console.error('Portal get ticket error:', error);
        res.status(500).json({ error: 'Failed to get ticket' });
      }
    });

    // Create new ticket
    this.app.post('/portal/api/tickets', requirePortalAuth, async (req: Request, res: Response) => {
      try {
        const session = (req as any).portalSession;
        const { subject, description, priority, type, deviceId } = req.body;

        if (!subject || !description) {
          res.status(400).json({ error: 'Subject and description are required' });
          return;
        }

        const ticket = await this.database.createPortalTicket({
          subject,
          description,
          priority: priority || 'medium',
          type: type || 'incident',
          deviceId,
          clientId: session.clientId,
          submitterEmail: session.userEmail,
          submitterName: session.userName || session.userEmail,
        });

        // Send notification to technicians
        await this.emailService.notifyTicketCreated(ticket);

        res.status(201).json(ticket);
      } catch (error) {
        console.error('Portal create ticket error:', error);
        res.status(500).json({ error: 'Failed to create ticket' });
      }
    });

    // Add comment to ticket
    this.app.post('/portal/api/tickets/:id/comments', requirePortalAuth, async (req: Request, res: Response) => {
      try {
        const session = (req as any).portalSession;
        const { id } = req.params;
        const { content } = req.body;

        if (!content) {
          res.status(400).json({ error: 'Content is required' });
          return;
        }

        // Verify access
        const ticket = await this.database.getTicket(id);
        if (!ticket) {
          res.status(404).json({ error: 'Ticket not found' });
          return;
        }

        const isSubmitter = ticket.submitterEmail === session.userEmail ||
                           ticket.requesterEmail === session.userEmail;
        const isSameClient = ticket.clientId === session.clientId;

        if (!isSubmitter && !isSameClient) {
          res.status(403).json({ error: 'Access denied' });
          return;
        }

        // Add comment
        const comment = await this.database.createTicketComment({
          ticketId: id,
          content,
          isInternal: false,
          authorName: session.userName || session.userEmail,
          authorEmail: session.userEmail,
        });

        // Notify about new comment
        await this.emailService.notifyTicketComment(ticket, comment);

        res.status(201).json(comment);
      } catch (error) {
        console.error('Portal add comment error:', error);
        res.status(500).json({ error: 'Failed to add comment' });
      }
    });

    // =========================================================================
    // Portal Admin API - Settings & Tenant Mapping
    // =========================================================================

    // Get portal settings
    this.app.get('/api/portal/settings', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const settings = await this.database.getSettings();
        // Return nested structure expected by Settings UI
        res.json({
          azureAd: {
            clientId: settings.azureClientId || '',
            clientSecret: settings.azureClientSecret ? '********' : '',
            redirectUri: settings.azureRedirectUri || '',
          },
          email: {
            enabled: settings.emailNotificationsEnabled === 'true',
            portalUrl: settings.portalUrl || '',
            smtp: {
              host: settings.smtpHost || '',
              port: parseInt(settings.smtpPort || '587', 10),
              secure: settings.smtpSecure === 'true',
              user: settings.smtpUser || '',
              password: settings.smtpPassword ? '********' : '',
              fromAddress: settings.smtpFromAddress || '',
              fromName: settings.smtpFromName || '',
            },
          },
        });
      } catch (error) {
        console.error('Get portal settings error:', error);
        res.status(500).json({ error: 'Failed to get settings' });
      }
    });

    // Update portal settings
    this.app.put('/api/portal/settings', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const updates: Record<string, string> = {};
        const body = req.body;

        // Handle nested structure from Settings UI
        const azureAd = body.azureAd || {};
        const email = body.email || {};
        const smtp = email.smtp || {};

        // Azure AD settings (from nested structure)
        if (azureAd.clientId !== undefined) updates.azureClientId = azureAd.clientId;
        if (azureAd.clientSecret && azureAd.clientSecret !== '********') {
          updates.azureClientSecret = azureAd.clientSecret;
        }
        if (azureAd.redirectUri !== undefined) updates.azureRedirectUri = azureAd.redirectUri;

        // Email notification settings
        if (email.enabled !== undefined) {
          updates.emailNotificationsEnabled = String(email.enabled);
        }
        if (email.portalUrl !== undefined) updates.portalUrl = email.portalUrl;

        // SMTP settings (from nested structure)
        if (smtp.host !== undefined) updates.smtpHost = smtp.host;
        if (smtp.port !== undefined) updates.smtpPort = String(smtp.port);
        if (smtp.secure !== undefined) updates.smtpSecure = String(smtp.secure);
        if (smtp.user !== undefined) updates.smtpUser = smtp.user;
        if (smtp.password && smtp.password !== '********') {
          updates.smtpPassword = smtp.password;
        }
        if (smtp.fromAddress !== undefined) updates.smtpFromAddress = smtp.fromAddress;
        if (smtp.fromName !== undefined) updates.smtpFromName = smtp.fromName;

        // Also support flat field names for backwards compatibility
        if (body.azureClientId !== undefined) updates.azureClientId = body.azureClientId;
        if (body.azureClientSecret && body.azureClientSecret !== '********') {
          updates.azureClientSecret = body.azureClientSecret;
        }
        if (body.azureRedirectUri !== undefined) updates.azureRedirectUri = body.azureRedirectUri;

        await this.database.updateSettings(updates);

        // Reinitialize services if needed
        await this.initializePortalServices();

        res.json({ success: true });
      } catch (error) {
        console.error('Update portal settings error:', error);
        res.status(500).json({ error: 'Failed to update settings' });
      }
    });

    // List clients (for tenant mapping dropdown)
    this.app.get('/api/clients', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const clients = await this.database.getClients();
        res.json(clients);
      } catch (error) {
        console.error('Get clients error:', error);
        res.status(500).json({ error: 'Failed to get clients' });
      }
    });

    // List client tenant mappings
    this.app.get('/api/client-tenants', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const tenants = await this.database.getClientTenants();
        res.json(tenants);
      } catch (error) {
        console.error('Get client tenants error:', error);
        res.status(500).json({ error: 'Failed to get tenant mappings' });
      }
    });

    // Create client tenant mapping
    this.app.post('/api/client-tenants', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        let { clientId, tenantId, tenantName, enabled } = req.body;

        if (!tenantId) {
          res.status(400).json({ error: 'Tenant ID is required' });
          return;
        }

        // Validate tenant ID format (should be a GUID)
        const guidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
        if (!guidRegex.test(tenantId)) {
          res.status(400).json({ error: 'Invalid Tenant ID format. Must be a valid GUID.' });
          return;
        }

        // If no clientId provided, auto-create a client using the tenant name
        if (!clientId) {
          const clientName = tenantName || `Tenant ${tenantId.substring(0, 8)}`;
          const newClient = await this.database.createClient({ name: clientName });
          clientId = newClient.id;
          console.log(`[Server] Auto-created client "${clientName}" for tenant ${tenantId}`);
        }

        const mapping = await this.database.createClientTenant({
          clientId,
          tenantId,
          tenantName,
          enabled,
        });

        res.status(201).json(mapping);
      } catch (error: any) {
        console.error('Create client tenant error:', error);
        if (error.message?.includes('duplicate')) {
          res.status(409).json({ error: 'This tenant is already mapped' });
        } else {
          res.status(500).json({ error: 'Failed to create mapping' });
        }
      }
    });

    // Update client tenant mapping
    this.app.put('/api/client-tenants/:id', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        const { tenantName, enabled } = req.body;

        const mapping = await this.database.updateClientTenant(id, { tenantName, enabled });
        if (!mapping) {
          res.status(404).json({ error: 'Mapping not found' });
          return;
        }

        res.json(mapping);
      } catch (error) {
        console.error('Update client tenant error:', error);
        res.status(500).json({ error: 'Failed to update mapping' });
      }
    });

    // Delete client tenant mapping
    this.app.delete('/api/client-tenants/:id', this.requireAuth.bind(this), async (req: Request, res: Response) => {
      try {
        const { id } = req.params;
        await this.database.deleteClientTenant(id);
        res.json({ success: true });
      } catch (error) {
        console.error('Delete client tenant error:', error);
        res.status(500).json({ error: 'Failed to delete mapping' });
      }
    });

    // =========================================================================
    // Portal Static Files
    // =========================================================================

    // Serve portal static files
    const portalDir = electronApp.isPackaged
      ? path.join(process.resourcesPath, 'portal')
      : path.join(__dirname, '..', '..', 'src', 'portal');

    this.app.use('/portal', express.static(portalDir));

    // Serve portal index for SPA routes
    this.app.get('/portal/*', (req: Request, res: Response) => {
      const indexPath = path.join(portalDir, 'index.html');
      if (fs.existsSync(indexPath)) {
        res.sendFile(indexPath);
      } else {
        res.status(404).send('Portal not found. Please configure the portal.');
      }
    });
  }

  /**
   * Initialize portal-related services (MSAL and Email)
   */
  public async initializePortalServices(): Promise<void> {
    const settings = await this.database.getSettings();

    // Initialize MSAL if configured
    if (settings.azureClientId && settings.azureClientSecret && settings.azureRedirectUri) {
      const msalConfig: MSALConfig = {
        clientId: settings.azureClientId,
        clientSecret: settings.azureClientSecret,
        redirectUri: settings.azureRedirectUri,
      };
      msalAuth.initialize(msalConfig);
      console.log('[Server] MSAL initialized for portal authentication');
    }

    // Initialize Email service if configured
    const emailConfig: EmailConfig = {
      enabled: settings.emailNotificationsEnabled === 'true',
      portalUrl: settings.portalUrl || `http://localhost:${this.port}`,
    };

    if (emailConfig.enabled && settings.smtpHost) {
      emailConfig.smtp = {
        host: settings.smtpHost,
        port: parseInt(settings.smtpPort) || 587,
        secure: parseInt(settings.smtpPort) === 465,
        user: settings.smtpUser || '',
        password: settings.smtpPassword || '',
        fromAddress: settings.smtpFromAddress || '',
        fromName: settings.smtpFromName,
      };
    }

    await this.emailService.initialize(emailConfig);
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

                // Check if device exists, if not tell agent to re-enroll
                const existingDevice = await this.database.getDeviceByAgentId(agentId);
                if (existingDevice) {
                  await this.database.updateDeviceLastSeen(agentId);
                  console.log(`Agent ${agentId} authenticated and registered`);
                } else {
                  console.log(`Agent ${agentId} connected but device not found - requesting re-enrollment`);
                }

                ws.send(JSON.stringify({
                  type: 'auth_response',
                  success: true,
                  payload: {
                    success: true,
                    needsEnrollment: !existingDevice
                  },
                  timestamp: new Date().toISOString(),
                }));
                return;
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

    // Initialize portal services (MSAL and Email)
    await this.initializePortalServices();

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
