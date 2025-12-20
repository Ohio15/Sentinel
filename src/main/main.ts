import { app, BrowserWindow, ipcMain, shell, dialog, Menu } from 'electron';
import { autoUpdater } from 'electron-updater';
import * as path from 'path';
import * as fs from 'fs';
import * as dotenv from 'dotenv';
import { Database } from './database';
import { Server } from './server';
import { AgentManager } from './agents';
import { GrpcServer } from './grpc-server';
import { listCertificates, renewCertificates, getCACertificate, getCertsDir } from './cert-manager';
import * as os from 'os';

// Disable GPU acceleration to prevent crashes on some systems
app.disableHardwareAcceleration();
app.commandLine.appendSwitch('disable-gpu');
app.commandLine.appendSwitch('disable-software-rasterizer');

// Set custom userData path - use appData for persistence (required for auto-updates)
const customUserData = path.join(app.getPath('appData'), 'Sentinel');
if (!fs.existsSync(customUserData)) {
  fs.mkdirSync(customUserData, { recursive: true });
}
app.setPath('userData', customUserData);

// Load environment variables from .env file in userData directory
const envPath = path.join(customUserData, '.env');
if (fs.existsSync(envPath)) {
  dotenv.config({ path: envPath });
  console.log('Loaded environment from:', envPath);
} else {
  // Also try project root for development
  const devEnvPath = path.join(__dirname, '../../.env');
  if (fs.existsSync(devEnvPath)) {
    dotenv.config({ path: devEnvPath });
    console.log('Loaded environment from:', devEnvPath);
  }
}

// Helper function to embed configuration into agent binary
function embedConfigInBinary(binaryData: Buffer, serverUrl: string, token: string): Buffer {
  const serverPlaceholder = 'SENTINEL_EMBEDDED_SERVER:' + '_'.repeat(64) + ':END';
  const tokenPlaceholder = 'SENTINEL_EMBEDDED_TOKEN:' + '_'.repeat(64) + ':END';

  const paddedServer = serverUrl.padEnd(64, '_').substring(0, 64);
  const paddedToken = token.padEnd(64, '_').substring(0, 64);

  const serverReplacement = 'SENTINEL_EMBEDDED_SERVER:' + paddedServer + ':END';
  const tokenReplacement = 'SENTINEL_EMBEDDED_TOKEN:' + paddedToken + ':END';

  let binaryStr = binaryData.toString('latin1');
  binaryStr = binaryStr.replace(serverPlaceholder, serverReplacement);
  binaryStr = binaryStr.replace(tokenPlaceholder, tokenReplacement);

  return Buffer.from(binaryStr, 'latin1');
}
// Helper function to embed configuration into installer packages (PKG/DEB)
// Uses placeholder replacement in the config.json embedded within the package
function embedConfigInInstaller(installerData: Buffer, serverUrl: string, token: string): Buffer {
  let content = installerData.toString('latin1');
  // Replace placeholder strings in the config file within the package
  content = content.replace(/__SERVERURL__/g, serverUrl);
  content = content.replace(/__TOKEN__/g, token);
  return Buffer.from(content, 'latin1');
}

function getLocalIpAddress(): string {
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

// Auto-updater configuration
autoUpdater.autoDownload = false; // User must confirm download
autoUpdater.autoInstallOnAppQuit = true;

// GitHub token for private repo access
function getGitHubToken(): string {
  // Check environment variables first
  if (process.env.GH_TOKEN) return process.env.GH_TOKEN;
  if (process.env.GITHUB_TOKEN) return process.env.GITHUB_TOKEN;

  // Check for config file in app directory (for production)
  try {
    const configPath = app.isPackaged
      ? path.join(process.resourcesPath, 'update-config.json')
      : path.join(__dirname, '../../update-config.json');

    if (fs.existsSync(configPath)) {
      const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
      if (config.githubToken) return config.githubToken;
    }
  } catch (e) {
    // Config file doesn't exist or is invalid
  }

  return '';
}

const GITHUB_TOKEN = getGitHubToken();

if (GITHUB_TOKEN) {
  // Use addAuthHeader for private repo authentication (recommended method)
  autoUpdater.addAuthHeader(`token ${GITHUB_TOKEN}`);
  console.log('GitHub token configured for auto-updates');
} else {
  console.log('No GitHub token - updates will only work for public repos');
}

function setupAutoUpdater(): void {
  // Only check for updates in production
  if (!app.isPackaged) {
    console.log('Skipping auto-update check in development mode');
    return;
  }

  // Check for updates on startup
  autoUpdater.checkForUpdates().catch((err) => {
    console.log('Update check failed:', err);
  });

  // Update available - notify renderer
  autoUpdater.on('update-available', (info) => {
    console.log('Update available:', info.version);
    mainWindow?.webContents.send('update-available', {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  // No update available
  autoUpdater.on('update-not-available', (info) => {
    console.log('No update available:', info.version);
    mainWindow?.webContents.send('update-not-available', {
      version: info.version,
    });
  });

  // Download progress
  autoUpdater.on('download-progress', (progress) => {
    console.log('Download progress:', progress.percent);
    mainWindow?.webContents.send('update-download-progress', {
      percent: progress.percent,
      bytesPerSecond: progress.bytesPerSecond,
      transferred: progress.transferred,
      total: progress.total,
    });
  });

  // Update downloaded - ready to install
  autoUpdater.on('update-downloaded', (info) => {
    console.log('Update downloaded:', info.version);
    mainWindow?.webContents.send('update-downloaded', {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  // Error handling
  autoUpdater.on('error', (err) => {
    console.error('Auto-updater error:', err);
    mainWindow?.webContents.send('update-error', {
      message: err.message,
    });
  });
}


let mainWindow: BrowserWindow | null = null;
let database: Database;
let server: Server;
let agentManager: AgentManager;
let grpcServer: GrpcServer;
let isQuitting = false;

// Ensure single instance - focus existing window if already running
const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
  // Another instance is running, quit immediately
  app.quit();
} else {
  // Handle second instance attempt - focus existing window
  app.on('second-instance', () => {
    if (mainWindow) {
      if (mainWindow.isMinimized()) {
        mainWindow.restore();
      }
      mainWindow.focus();
    }
  });
}

function createWindow(): void {
  console.log('Creating main window...');
  mainWindow = new BrowserWindow({
    width: 1400,
    height: 900,
    minWidth: 1024,
    minHeight: 768,
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      preload: path.join(__dirname, 'preload.js'),
    },
    icon: path.join(__dirname, '../../assets/icon.png'),
    titleBarStyle: 'default',
    show: true, // Show immediately instead of waiting
  });

  const indexPath = path.join(__dirname, '../renderer/index.html');
  console.log('Loading index.html from:', indexPath);

  // Load the app from built files
  mainWindow.loadFile(indexPath).then(() => {
    console.log('Index.html loaded successfully');
  }).catch((err) => {
    console.error('Failed to load index.html:', err);
  });

  // Show window when ready (backup)
  mainWindow.once('ready-to-show', () => {
    console.log('Window ready to show');
    mainWindow?.show();
    mainWindow?.focus();
  });

  // Handle external links
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });

  mainWindow.on('closed', () => {
    mainWindow = null;
  });
}

async function initialize(): Promise<void> {
  // Initialize database
  database = new Database();
  await database.initialize();

  // Initialize agent manager
  agentManager = new AgentManager(database);

  // Initialize embedded server
  server = new Server(database, agentManager);
  await server.start();

  // Initialize gRPC server (Data Plane - HTTP port + 1)
  const httpPort = server.getPort();
  const grpcPort = httpPort + 1;
  grpcServer = new GrpcServer(database, agentManager, grpcPort);
  try {
    await grpcServer.start();
    console.log('Dual-channel architecture initialized:');
    console.log(`  - WebSocket Control Plane: port ${httpPort}`);
    console.log(`  - gRPC Data Plane: port ${grpcPort}`);
  } catch (error) {
    console.error('Failed to start gRPC server:', error);
    // gRPC is optional, continue without it
  }

  // Setup IPC handlers
  setupIpcHandlers();
}


// Auto-updater IPC handlers - registered early so updates work even if DB fails
function setupUpdaterHandlers(): void {
  // Auto-updater IPC handlers
  ipcMain.handle('check-for-updates', async () => {
    try {
      const result = await autoUpdater.checkForUpdates();
      return { success: true, updateInfo: result?.updateInfo };
    } catch (error) {
      return { success: false, error: (error as Error).message };
    }
  });

  ipcMain.handle('download-update', async () => {
    try {
      await autoUpdater.downloadUpdate();
      return { success: true };
    } catch (error) {
      return { success: false, error: (error as Error).message };
    }
  });

  ipcMain.handle('install-update', async () => {
    // Set isQuitting flag BEFORE cleanup to prevent before-quit handler from interfering
    isQuitting = true;

    // Clean up resources before quitting for update
    try {
      console.log('Preparing to install update...');
      if (grpcServer) {
        console.log('Stopping gRPC server...');
        await grpcServer.stop();
      }
      if (server) {
        console.log('Stopping WebSocket server...');
        await server.stop();
      }
      if (database) {
        console.log('Closing database...');
        await database.close();
      }
      console.log('Cleanup complete, installing update...');
    } catch (error) {
      console.error('Error during cleanup:', error);
    }
    // Small delay to ensure cleanup is complete
    await new Promise(resolve => setTimeout(resolve, 500));
    // Use isSilent=true to prevent installer UI from blocking, isForceRunAfter=true to restart app
    autoUpdater.quitAndInstall(true, true);
  });

  // App version - doesn't need DB so register early
  ipcMain.handle('get-app-version', () => {
    return app.getVersion();
  });
}

function setupIpcHandlers(): void {
  // Device management
  ipcMain.handle('devices:list', async (_, clientId?: string) => {
    const devices = await database.getDevices(clientId);
    // Check local WebSocket connection first, but trust database status if agent
    // is connected to external backend (e.g., Docker) and has recent activity
    return devices.map(device => {
      if (device.agentId && agentManager.isAgentConnected(device.agentId)) {
        return { ...device, status: 'online' };
      }
      // Trust database status - agent may be connected to Docker backend
      // Consider online if lastSeen is within 90 seconds (heartbeat interval + buffer)
      if (device.status === 'online' && device.lastSeen) {
        const lastSeenTime = new Date(device.lastSeen).getTime();
        const now = Date.now();
        const isRecentlyActive = (now - lastSeenTime) < 90000; // 90 seconds
        if (isRecentlyActive) {
          return { ...device, status: 'online' };
        }
      }
      return { ...device, status: device.isDisabled ? 'disabled' : 'offline' };
    });
  });

  ipcMain.handle('devices:get', async (_, id: string) => {
    console.log('[IPC] devices:get called with id:', id);
    const device = await database.getDevice(id);
    console.log('[IPC] devices:get result:', device ? device.hostname : 'null');
    if (device) {
      // Check local connection first, then trust database status with recent activity check
      const isLocallyConnected = device.agentId && agentManager.isAgentConnected(device.agentId);
      let status = 'offline';
      if (isLocallyConnected) {
        status = 'online';
      } else if (device.status === 'online' && device.lastSeen) {
        const lastSeenTime = new Date(device.lastSeen).getTime();
        const isRecentlyActive = (Date.now() - lastSeenTime) < 90000;
        if (isRecentlyActive) status = 'online';
      }
      if (device.isDisabled) status = 'disabled';
      console.log('[IPC] devices:get status:', status, 'agentId:', device.agentId);
      return { ...device, status };
    }
    return device;
  });

  ipcMain.handle('devices:delete', async (_, id: string) => {
    // Only allow deletion if device is in 'uninstalling' status
    const status = await database.getDeviceStatus(id);
    if (status !== 'uninstalling') {
      throw new Error('Devices can only be removed after uninstalling the agent remotely. Use the Uninstall Agent option.');
    }
    return database.deleteDevice(id);
  });

  ipcMain.handle('devices:disable', async (_, id: string) => {
    return agentManager.disableDevice(id);
  });

  ipcMain.handle('devices:enable', async (_, id: string) => {
    return agentManager.enableDevice(id);
  });

  ipcMain.handle('devices:uninstall', async (_, id: string) => {
    return agentManager.uninstallAgent(id);
  });

  ipcMain.handle('devices:update', async (_, id: string, updates: { displayName?: string; tags?: string[] }) => {
    return database.updateDevice(id, updates);
  });

  
  ipcMain.handle('devices:ping', async (_, deviceId: string) => {
    return agentManager.pingAgent(deviceId);
  });

  ipcMain.handle('devices:getMetrics', async (_, deviceId: string, hours: number) => {
    return database.getDeviceMetrics(deviceId, hours);
  });

  ipcMain.handle('devices:setMetricsInterval', async (_, deviceId: string, intervalMs: number) => {
    return agentManager.setMetricsInterval(deviceId, intervalMs);
  });

  // Commands
  ipcMain.handle('commands:execute', async (_, deviceId: string, command: string, type: string) => {
    return agentManager.executeCommand(deviceId, command, type);
  });

  ipcMain.handle('commands:getHistory', async (_, deviceId: string) => {
    return database.getCommandHistory(deviceId);
  });

  // Remote terminal
  ipcMain.handle('terminal:start', async (_, deviceId: string) => {
    return agentManager.startTerminalSession(deviceId);
  });

  ipcMain.handle('terminal:send', async (_, sessionId: string, data: string) => {
    return agentManager.sendTerminalData(sessionId, data);
  });

  ipcMain.handle('terminal:resize', async (_, sessionId: string, cols: number, rows: number) => {
    return agentManager.resizeTerminal(sessionId, cols, rows);
  });

  ipcMain.handle('terminal:close', async (_, sessionId: string) => {
    return agentManager.closeTerminalSession(sessionId);
  });

  // File transfer
  ipcMain.handle('files:drives', async (_, deviceId: string) => {
    return agentManager.listDrives(deviceId);
  });

  ipcMain.handle('files:list', async (_, deviceId: string, remotePath: string) => {
    return agentManager.listFiles(deviceId, remotePath);
  });

  ipcMain.handle('files:download', async (_, deviceId: string, remotePath: string, localPath: string) => {
    return agentManager.downloadFile(deviceId, remotePath, localPath);
  });

  ipcMain.handle('files:upload', async (_, deviceId: string, localPath: string, remotePath: string) => {
    return agentManager.uploadFile(deviceId, localPath, remotePath);
  });

  ipcMain.handle('files:scan', async (_, deviceId: string, path: string, maxDepth: number) => {
    return agentManager.scanDirectory(deviceId, path, maxDepth);
  });

  ipcMain.handle('files:downloadToSandbox', async (_, deviceId: string, remotePath: string) => {
    const path = await import('path');
    const fs = await import('fs');
    const os = await import('os');
    const crypto = await import('crypto');
    
    // Create sandbox directory in user's app data
    const sandboxDir = path.join(os.homedir(), '.sentinel', 'sandbox');
    if (!fs.existsSync(sandboxDir)) {
      fs.mkdirSync(sandboxDir, { recursive: true });
    }
    
    // Generate unique filename with timestamp and hash
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const hash = crypto.randomBytes(4).toString('hex');
    const originalName = path.basename(remotePath);
    const sandboxFilename = `${timestamp}_${hash}_${originalName}`;
    const localPath = path.join(sandboxDir, sandboxFilename);
    
    await agentManager.downloadFile(deviceId, remotePath, localPath);
    
    // Calculate SHA256 hash of downloaded file
    const fileBuffer = fs.readFileSync(localPath);
    const sha256Hash = crypto.createHash('sha256').update(fileBuffer).digest('hex');
    
    return {
      localPath,
      sandboxDir,
      filename: sandboxFilename,
      originalName,
      sha256: sha256Hash,
      size: fileBuffer.length,
    };
  });

  // Remote desktop
  ipcMain.handle('remote:startSession', async (_, deviceId: string) => {
    return agentManager.startRemoteSession(deviceId);
  });

  ipcMain.handle('remote:stopSession', async (_, sessionId: string) => {
    return agentManager.stopRemoteSession(sessionId);
  });

  ipcMain.handle('remote:sendInput', async (_, sessionId: string, input: any) => {
    return agentManager.sendRemoteInput(sessionId, input);
  });

  // WebRTC Remote Desktop
  ipcMain.handle('webrtc:start', async (_, deviceId: string, offer: any) => {
    return agentManager.startWebRTCSession(deviceId, offer);
  });

  ipcMain.handle('webrtc:stop', async (_, deviceId: string) => {
    return agentManager.stopWebRTCSession(deviceId);
  });

  ipcMain.handle('webrtc:signal', async (_, deviceId: string, signal: any) => {
    return agentManager.sendWebRTCSignal(deviceId, signal);
  });

  ipcMain.handle('webrtc:setQuality', async (_, deviceId: string, quality: string) => {
    return agentManager.setWebRTCQuality(deviceId, quality);
  });

  // Alerts
  ipcMain.handle('alerts:list', async () => {
    return database.getAlerts();
  });

  ipcMain.handle('alerts:acknowledge', async (_, id: string) => {
    return database.acknowledgeAlert(id);
  });

  ipcMain.handle('alerts:resolve', async (_, id: string) => {
    return database.resolveAlert(id);
  });

  ipcMain.handle('alerts:getRules', async () => {
    return database.getAlertRules();
  });

  ipcMain.handle('alerts:createRule', async (_, rule: any) => {
    return database.createAlertRule(rule);
  });

  ipcMain.handle('alerts:updateRule', async (_, id: string, rule: any) => {
    return database.updateAlertRule(id, rule);
  });

  ipcMain.handle('alerts:deleteRule', async (_, id: string) => {
    return database.deleteAlertRule(id);
  });

  // Scripts
  ipcMain.handle('scripts:list', async () => {
    return database.getScripts();
  });

  ipcMain.handle('scripts:create', async (_, script: any) => {
    return database.createScript(script);
  });

  ipcMain.handle('scripts:update', async (_, id: string, script: any) => {
    return database.updateScript(id, script);
  });

  ipcMain.handle('scripts:delete', async (_, id: string) => {
    return database.deleteScript(id);
  });

  ipcMain.handle('scripts:execute', async (_, scriptId: string, deviceIds: string[]) => {
    return agentManager.executeScript(scriptId, deviceIds);
  });

  // Tickets
  ipcMain.handle('tickets:list', async (_, filters?: { status?: string; priority?: string; assignedTo?: string; deviceId?: string }) => {
    return database.getTickets(filters);
  });

  ipcMain.handle('tickets:get', async (_, id: string) => {
    return database.getTicket(id);
  });

  ipcMain.handle('tickets:create', async (_, ticket: any) => {
    // Create the ticket first
    const createdTicket = await database.createTicket(ticket);

    // If ticket has a deviceId, collect diagnostics in the background
    if (ticket.deviceId) {
      collectAndPostDiagnostics(createdTicket.id, ticket.deviceId).catch(err => {
        console.error('Failed to collect diagnostics:', err);
      });
    }

    return createdTicket;
  });

  ipcMain.handle('tickets:update', async (_, id: string, updates: any) => {
    return database.updateTicket(id, updates);
  });

  ipcMain.handle('tickets:delete', async (_, id: string) => {
    return database.deleteTicket(id);
  });

  ipcMain.handle('tickets:getComments', async (_, ticketId: string) => {
    return database.getTicketComments(ticketId);
  });

  ipcMain.handle('tickets:addComment', async (_, comment: any) => {
    return database.createTicketComment(comment);
  });

  ipcMain.handle('tickets:getActivity', async (_, ticketId: string) => {
    return database.getTicketActivity(ticketId);
  });

  ipcMain.handle('tickets:getStats', async () => {
    return database.getTicketStats();
  });

  ipcMain.handle('tickets:getTemplates', async () => {
    return database.getTicketTemplates();
  });

  ipcMain.handle('tickets:createTemplate', async (_, template: any) => {
    return database.createTicketTemplate(template);
  });

  ipcMain.handle('tickets:updateTemplate', async (_, id: string, template: any) => {
    return database.updateTicketTemplate(id, template);
  });

  ipcMain.handle('tickets:deleteTemplate', async (_, id: string) => {
    return database.deleteTicketTemplate(id);
  });

  // SLA Policies
  ipcMain.handle('sla:list', async (_, clientId?: string) => {
    return database.getSLAPolicies(clientId);
  });

  ipcMain.handle('sla:get', async (_, id: string) => {
    return database.getSLAPolicy(id);
  });

  ipcMain.handle('sla:create', async (_, policy: any) => {
    return database.createSLAPolicy(policy);
  });

  ipcMain.handle('sla:update', async (_, id: string, updates: any) => {
    return database.updateSLAPolicy(id, updates);
  });

  ipcMain.handle('sla:delete', async (_, id: string) => {
    return database.deleteSLAPolicy(id);
  });

  ipcMain.handle('sla:calculateDueDates', async (_, ticketId: string) => {
    return database.calculateSLADueDates(ticketId);
  });

  ipcMain.handle('sla:recordFirstResponse', async (_, ticketId: string) => {
    return database.recordFirstResponse(ticketId);
  });

  ipcMain.handle('sla:pause', async (_, ticketId: string) => {
    return database.pauseSLA(ticketId);
  });

  ipcMain.handle('sla:resume', async (_, ticketId: string) => {
    return database.resumeSLA(ticketId);
  });

  ipcMain.handle('sla:checkBreaches', async () => {
    return database.checkSLABreaches();
  });

  // Ticket Categories
  ipcMain.handle('categories:list', async (_, clientId?: string) => {
    return database.getTicketCategories(clientId);
  });

  ipcMain.handle('categories:get', async (_, id: string) => {
    return database.getTicketCategory(id);
  });

  ipcMain.handle('categories:create', async (_, category: any) => {
    return database.createTicketCategory(category);
  });

  ipcMain.handle('categories:update', async (_, id: string, updates: any) => {
    return database.updateTicketCategory(id, updates);
  });

  ipcMain.handle('categories:delete', async (_, id: string) => {
    return database.deleteTicketCategory(id);
  });

  // Ticket Tags
  ipcMain.handle('tags:list', async (_, clientId?: string) => {
    return database.getTicketTags(clientId);
  });

  ipcMain.handle('tags:get', async (_, id: string) => {
    return database.getTicketTag(id);
  });

  ipcMain.handle('tags:create', async (_, tag: any) => {
    return database.createTicketTag(tag);
  });

  ipcMain.handle('tags:update', async (_, id: string, updates: any) => {
    return database.updateTicketTag(id, updates);
  });

  ipcMain.handle('tags:delete', async (_, id: string) => {
    return database.deleteTicketTag(id);
  });

  ipcMain.handle('tags:getAssignments', async (_, ticketId: string) => {
    return database.getTicketTagAssignments(ticketId);
  });

  ipcMain.handle('tags:assign', async (_, ticketId: string, tagIds: string[], assignedBy?: string) => {
    return database.assignTagsToTicket(ticketId, tagIds, assignedBy);
  });

  // Ticket Links
  ipcMain.handle('links:list', async (_, ticketId: string) => {
    return database.getTicketLinks(ticketId);
  });

  ipcMain.handle('links:create', async (_, link: any) => {
    return database.createTicketLink(link);
  });

  ipcMain.handle('links:delete', async (_, id: string) => {
    return database.deleteTicketLink(id);
  });

  // Ticket Analytics
  ipcMain.handle('analytics:tickets', async (_, params: { clientId?: string; dateFrom?: string; dateTo?: string }) => {
    return database.getTicketAnalytics(params);
  });

  // Knowledge Base Categories
  ipcMain.handle('kb:categories:list', async () => {
    return database.getKBCategories();
  });

  ipcMain.handle('kb:categories:get', async (_, id: string) => {
    return database.getKBCategory(id);
  });

  ipcMain.handle('kb:categories:create', async (_, category: any) => {
    return database.createKBCategory(category);
  });

  ipcMain.handle('kb:categories:update', async (_, id: string, updates: any) => {
    return database.updateKBCategory(id, updates);
  });

  ipcMain.handle('kb:categories:delete', async (_, id: string) => {
    return database.deleteKBCategory(id);
  });

  // Knowledge Base Articles
  ipcMain.handle('kb:articles:list', async (_, options?: { categoryId?: string; status?: string; featured?: boolean; limit?: number; offset?: number }) => {
    return database.getKBArticles(options);
  });

  ipcMain.handle('kb:articles:get', async (_, id: string) => {
    return database.getKBArticle(id);
  });

  ipcMain.handle('kb:articles:getBySlug', async (_, slug: string) => {
    return database.getKBArticleBySlug(slug);
  });

  ipcMain.handle('kb:articles:create', async (_, article: any) => {
    return database.createKBArticle(article);
  });

  ipcMain.handle('kb:articles:update', async (_, id: string, updates: any) => {
    return database.updateKBArticle(id, updates);
  });

  ipcMain.handle('kb:articles:delete', async (_, id: string) => {
    return database.deleteKBArticle(id);
  });

  ipcMain.handle('kb:articles:search', async (_, query: string, limit?: number) => {
    return database.searchKBArticles(query, limit);
  });

  ipcMain.handle('kb:articles:suggest', async (_, ticketSubject: string, limit?: number) => {
    return database.suggestKBArticles(ticketSubject, limit);
  });

  ipcMain.handle('kb:articles:featured', async (_, limit?: number) => {
    return database.getKBFeaturedArticles(limit);
  });

  ipcMain.handle('kb:articles:related', async (_, articleId: string) => {
    return database.getKBRelatedArticles(articleId);
  });

  // Settings
  ipcMain.handle('settings:get', async () => {
    return database.getSettings();
  });

  ipcMain.handle('settings:update', async (_, settings: any) => {
    return database.updateSettings(settings);
  });

  // Portal Settings
  ipcMain.handle('portal:getSettings', async () => {
    const settings = await database.getSettings();
    return {
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
    };
  });

  ipcMain.handle('portal:updateSettings', async (_, body: any) => {
    const updates: Record<string, string> = {};

    const azureAd = body.azureAd || {};
    const email = body.email || {};
    const smtp = email.smtp || {};

    console.log('[IPC:portal:updateSettings] Received:', {
      hasClientId: !!azureAd.clientId,
      clientIdLength: azureAd.clientId?.length || 0,
      hasClientSecret: !!azureAd.clientSecret,
      clientSecretIsMasked: azureAd.clientSecret === '********',
      clientSecretLength: azureAd.clientSecret?.length || 0,
      hasRedirectUri: !!azureAd.redirectUri,
      redirectUri: azureAd.redirectUri,
    });

    if (azureAd.clientId !== undefined) updates.azureClientId = azureAd.clientId;
    if (azureAd.clientSecret && azureAd.clientSecret !== '********') {
      updates.azureClientSecret = azureAd.clientSecret;
      console.log('[IPC:portal:updateSettings] Saving new client secret (length:', azureAd.clientSecret.length, ')');
    }
    if (azureAd.redirectUri !== undefined) updates.azureRedirectUri = azureAd.redirectUri;

    if (email.enabled !== undefined) {
      updates.emailNotificationsEnabled = String(email.enabled);
    }
    if (email.portalUrl !== undefined) updates.portalUrl = email.portalUrl;

    if (smtp.host !== undefined) updates.smtpHost = smtp.host;
    if (smtp.port !== undefined) updates.smtpPort = String(smtp.port);
    if (smtp.secure !== undefined) updates.smtpSecure = String(smtp.secure);
    if (smtp.user !== undefined) updates.smtpUser = smtp.user;
    if (smtp.password && smtp.password !== '********') {
      updates.smtpPassword = smtp.password;
    }
    if (smtp.fromAddress !== undefined) updates.smtpFromAddress = smtp.fromAddress;
    if (smtp.fromName !== undefined) updates.smtpFromName = smtp.fromName;

    await database.updateSettings(updates);
    await server.initializePortalServices();
    return { success: true };
  });

  ipcMain.handle('portal:getClientTenants', async () => {
    return database.getClientTenants();
  });

  ipcMain.handle('portal:createClientTenant', async (_, data: { clientId?: string; tenantId: string; tenantName?: string }) => {
    let { clientId, tenantId, tenantName } = data;

    if (!clientId) {
      const clientName = tenantName || `Tenant ${tenantId.substring(0, 8)}`;
      const newClient = await database.createClient({ name: clientName });
      clientId = newClient.id;
    }

    return database.createClientTenant({
      clientId: clientId as string,
      tenantId,
      tenantName,
    });
  });

  ipcMain.handle('portal:deleteClientTenant', async (_, id: string) => {
    return database.deleteClientTenant(id);
  });


  // Clients
  ipcMain.handle('clients:list', async () => {
    return database.getClientsWithCounts();
  });

  ipcMain.handle('clients:get', async (_, id: string) => {
    return database.getClient(id);
  });

  ipcMain.handle('clients:create', async (_, client: { name: string; description?: string; color?: string; logoUrl?: string; logoWidth?: number; logoHeight?: number }) => {
    return database.createClient(client);
  });

  ipcMain.handle('clients:update', async (_, id: string, client: { name?: string; description?: string; color?: string; logoUrl?: string; logoWidth?: number; logoHeight?: number }) => {
    return database.updateClient(id, client);
  });

  ipcMain.handle('clients:delete', async (_, id: string) => {
    return database.deleteClient(id);
  });

  ipcMain.handle('devices:assignToClient', async (_, deviceId: string, clientId: string | null) => {
    await database.assignDeviceToClient(deviceId, clientId);
    return database.getDevice(deviceId);
  });

  ipcMain.handle('devices:bulkAssignToClient', async (_, deviceIds: string[], clientId: string | null) => {
    await database.bulkAssignDevicesToClient(deviceIds, clientId);
    return { success: true, count: deviceIds.length };
  });

  // Server info
  ipcMain.handle('server:getInfo', async () => {
    // Count agents: local connections + database online (connected to Docker backend)
    const localCount = agentManager.getConnectedAgentCount();
    let dbOnlineCount = 0;
    try {
      const devices = await database.getDevices();
      const now = Date.now();
      dbOnlineCount = devices.filter(d => {
        if (d.agentId && agentManager.isAgentConnected(d.agentId)) return false; // Already counted
        if (d.status === 'online' && d.lastSeen) {
          const lastSeenTime = new Date(d.lastSeen).getTime();
          return (now - lastSeenTime) < 90000; // 90 seconds
        }
        return false;
      }).length;
    } catch (e) { /* ignore */ }
    return {
      port: server.getPort(),
      grpcPort: grpcServer?.getPort() || 8082,
      agentCount: localCount + dbOnlineCount,
      grpcAgentCount: grpcServer?.getConnectionCount() || 0,
      enrollmentToken: server.getEnrollmentToken(),
    };
  });

  ipcMain.handle('server:regenerateToken', async () => {
    return server.regenerateEnrollmentToken();
  });

  // Certificate management
  ipcMain.handle('certs:list', async () => {
    return listCertificates();
  });

  ipcMain.handle('certs:renew', async () => {
    try {
      const result = await renewCertificates();
      return { success: result.success, error: result.success ? undefined : result.message };
    } catch (error: any) {
      return { success: false, error: error.message };
    }
  });

  ipcMain.handle('certs:distribute', async () => {
    const caCert = getCACertificate();
    if (!caCert) {
      return { success: 0, failed: 0, total: 0, error: 'CA certificate not found' };
    }
    return agentManager.broadcastCertificate(caCert.content, caCert.hash);
  });

  ipcMain.handle('certs:getAgentStatus', async () => {
    return database.getAgentCertStatuses();
  });

  ipcMain.handle('certs:getCurrent', async () => {
    return getCACertificate();
  });

  // Device update status
  ipcMain.handle('updates:getAll', async () => {
    return database.getAllDeviceUpdateStatuses();
  });

  ipcMain.handle('updates:getDevice', async (_, deviceId: string) => {
    return database.getDeviceUpdateStatus(deviceId);
  });

  ipcMain.handle('updates:getPending', async (_, minCount?: number) => {
    return database.getDevicesWithPendingUpdates(minCount || 1);
  });

  ipcMain.handle('updates:getSecurity', async () => {
    return database.getDevicesWithSecurityUpdates();
  });

  ipcMain.handle('updates:getRebootRequired', async () => {
    return database.getDevicesRequiringReboot();
  });


  // Agent installer
  ipcMain.handle('agent:getInstallerCommand', async (_, platform: string) => {
    return server.getAgentInstallerCommand(platform);
  });

  // Agent download with save dialog
  ipcMain.handle('agent:download', async (_, platform: string) => {
    // Use installer packages instead of raw binaries
    const agentDir = app.isPackaged
      ? path.join(process.resourcesPath, 'agent')
      : path.join(__dirname, '..', '..', 'release', 'agent');

    console.log('Agent installer download - platform:', platform);
    console.log('Agent installer download - agentDir:', agentDir);

    // Map platform to installer file
    interface InstallerInfo {
      file: string;
      filter: { name: string; extensions: string[] };
    }
    const installerMap: Record<string, InstallerInfo> = {
      windows: {
        file: 'sentinel-agent.msi',
        filter: { name: 'Windows Installer', extensions: ['msi'] }
      },
      macos: {
        file: 'sentinel-agent.pkg',
        filter: { name: 'macOS Installer', extensions: ['pkg'] }
      },
      linux: {
        file: 'sentinel-agent.deb',
        filter: { name: 'Debian Package', extensions: ['deb'] }
      },
    };

    const installer = installerMap[platform.toLowerCase()];
    if (!installer) {
      return { success: false, error: 'Unsupported platform' };
    }

    const sourcePath = path.join(agentDir, installer.file);
    console.log('Agent installer download - sourcePath:', sourcePath);
    console.log('Agent installer download - exists:', fs.existsSync(sourcePath));

    // Check if installer exists
    if (!fs.existsSync(sourcePath)) {
      return {
        success: false,
        error: `Installer not found: ${installer.file}. Build installers first.`,
      };
    }

    // Show save dialog
    const result = await dialog.showSaveDialog(mainWindow!, {
      title: 'Save Agent Installer',
      defaultPath: installer.file,
      filters: [installer.filter],
    });

    if (result.canceled || !result.filePath) {
      return { success: false, canceled: true };
    }

    // Get server info
    const localIp = getLocalIpAddress();
    const serverPort = server.getPort();
    const serverUrl = `http://${localIp}:${serverPort}`;
    const enrollmentToken = server.getEnrollmentToken();

    try {
      const rawInstallerData = fs.readFileSync(sourcePath);

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      const installerData = platform.toLowerCase() !== 'windows'
        ? embedConfigInInstaller(rawInstallerData, serverUrl, enrollmentToken)
        : rawInstallerData;

      await fs.promises.writeFile(result.filePath, installerData);

      // Return install command based on platform
      const installCommands: Record<string, string> = {
        windows: `msiexec /i "${result.filePath}" SERVERURL="${serverUrl}" ENROLLMENTTOKEN="${enrollmentToken}" /qn`,
        macos: `sudo installer -pkg "${result.filePath}" -target /`,
        linux: `sudo dpkg -i "${result.filePath}"`,
      };

      return {
        success: true,
        filePath: result.filePath,
        size: installerData.length,
        installCommand: installCommands[platform.toLowerCase()],
      };
    } catch (error: any) {
      return {
        success: false,
        error: `Failed to save installer: ${error.message}`,
      };
    }
  });

  // MSI download with save dialog
  ipcMain.handle('agent:downloadMsi', async () => {
    const agentDir = app.isPackaged
      ? path.join(process.resourcesPath, 'agent')
      : path.join(__dirname, '..', '..', 'release', 'agent');

    const sourcePath = path.join(agentDir, 'sentinel-agent.msi');
    console.log('MSI download - sourcePath:', sourcePath);

    // Check if MSI file exists
    if (!fs.existsSync(sourcePath)) {
      return {
        success: false,
        error: `MSI installer not found. Build with: cd agent && .\\build.ps1 -Platform windows -BuildMsi`,
      };
    }

    // Show save dialog
    const result = await dialog.showSaveDialog(mainWindow!, {
      title: 'Save Agent MSI Installer',
      defaultPath: 'sentinel-agent.msi',
      filters: [{ name: 'Windows Installer', extensions: ['msi'] }],
    });

    if (result.canceled || !result.filePath) {
      return { success: false, canceled: true };
    }

    try {
      // Read MSI file
      let msiData = fs.readFileSync(sourcePath);

      // Get server info for embedding in the MSI
      // For MSI, we'll create a transform file or embed via property table
      // For now, we copy and provide install command with parameters
      const localIp = getLocalIpAddress();
      const serverPort = server.getPort();
      const serverUrl = `http://${localIp}:${serverPort}`;
      const enrollmentToken = server.getEnrollmentToken();

      // Write the MSI file
      await fs.promises.writeFile(result.filePath, msiData);

      return {
        success: true,
        filePath: result.filePath,
        size: msiData.length,
        installCommand: `msiexec /i "${result.filePath}" SERVERURL="${serverUrl}" ENROLLMENTTOKEN="${enrollmentToken}" /qn`,
      };
    } catch (error: any) {
      return {
        success: false,
        error: `Failed to save MSI: ${error.message}`,
      };
    }
  });

  // Get MSI install command
  ipcMain.handle('agent:getMsiCommand', async () => {
    const localIp = getLocalIpAddress();
    const serverPort = server.getPort();
    const serverUrl = `http://${localIp}:${serverPort}`;
    const enrollmentToken = server.getEnrollmentToken();

    return {
      serverUrl,
      enrollmentToken,
      command: `msiexec /i "sentinel-agent.msi" SERVERURL="${serverUrl}" ENROLLMENTTOKEN="${enrollmentToken}" /qn`,
    };
  });

  // Execute PowerShell install script with UAC elevation
  ipcMain.handle('agent:runPowerShellInstall', async () => {
    const localIp = getLocalIpAddress();
    const serverPort = server.getPort();
    const serverUrl = `http://${localIp}:${serverPort}`;
    const enrollmentToken = server.getEnrollmentToken();

    // PowerShell script that downloads and installs the agent
    const psScript = `
$ErrorActionPreference = 'Stop'
$agentPath = Join-Path $env:TEMP 'sentinel-agent.exe'
Write-Host 'Downloading Sentinel Agent...' -ForegroundColor Cyan
Invoke-WebRequest -Uri '${serverUrl}/api/agent/download/windows' -OutFile $agentPath -UseBasicParsing
Write-Host 'Installing agent...' -ForegroundColor Green
Start-Process -FilePath $agentPath -ArgumentList '--install','--server=${serverUrl}','--token=${enrollmentToken}' -Wait
Write-Host 'Installation complete!' -ForegroundColor Green
Read-Host 'Press Enter to close'
`;

    // Create a temp script file
    const tempScriptPath = path.join(app.getPath('temp'), 'sentinel-install.ps1');
    fs.writeFileSync(tempScriptPath, psScript, 'utf8');

    try {
      // Launch PowerShell with UAC elevation
      const { spawn } = require('child_process');
      spawn('powershell.exe', [
        '-ExecutionPolicy', 'Bypass',
        '-Command',
        `Start-Process powershell -Verb RunAs -ArgumentList '-ExecutionPolicy','Bypass','-NoExit','-File','${tempScriptPath.replace(/\\/g, '\\\\')}'`
      ], {
        detached: true,
        stdio: 'ignore',
        shell: true,
      }).unref();

      return { success: true };
    } catch (error: any) {
      return {
        success: false,
        error: `Failed to launch PowerShell: ${error.message}`,
      };
    }
  });
}

// Helper function to collect diagnostics and post as ticket comments
async function collectAndPostDiagnostics(ticketId: string, deviceId: string): Promise<void> {
  try {
    console.log(`Collecting diagnostics for ticket ${ticketId} from device ${deviceId}`);

    // Collect diagnostics (8 hours back)
    const diagnostics = await agentManager.collectDiagnostics(deviceId, 8);

    if (!diagnostics) {
      console.log('No diagnostics data received');
      return;
    }

    const timestamp = new Date().toISOString();

    // Post System Errors as a comment
    if (diagnostics.systemErrors && diagnostics.systemErrors.length > 0) {
      const systemErrorsContent = formatLogEntries('System Errors (Event Viewer)', diagnostics.systemErrors);
      await database.createTicketComment({
        ticketId,
        content: systemErrorsContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post Application Logs as a comment
    if (diagnostics.applicationLogs && diagnostics.applicationLogs.length > 0) {
      const appLogsContent = formatLogEntries('Application Logs', diagnostics.applicationLogs);
      await database.createTicketComment({
        ticketId,
        content: appLogsContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post Security Events as a comment
    if (diagnostics.securityEvents && diagnostics.securityEvents.length > 0) {
      const securityContent = formatLogEntries('Security Events (Audit Failures)', diagnostics.securityEvents);
      await database.createTicketComment({
        ticketId,
        content: securityContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post Hardware Events as a comment
    if (diagnostics.hardwareEvents && diagnostics.hardwareEvents.length > 0) {
      const hardwareContent = formatLogEntries('Hardware Events', diagnostics.hardwareEvents);
      await database.createTicketComment({
        ticketId,
        content: hardwareContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post Network Events as a comment
    if (diagnostics.networkEvents && diagnostics.networkEvents.length > 0) {
      const networkContent = formatLogEntries('Network Events', diagnostics.networkEvents);
      await database.createTicketComment({
        ticketId,
        content: networkContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post Recent Crashes as a comment
    if (diagnostics.recentCrashes && diagnostics.recentCrashes.length > 0) {
      const crashesContent = formatLogEntries('Recent Application Crashes', diagnostics.recentCrashes);
      await database.createTicketComment({
        ticketId,
        content: crashesContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post Active Programs as a comment
    if (diagnostics.activePrograms && diagnostics.activePrograms.length > 0) {
      const programsContent = formatActivePrograms(diagnostics.activePrograms);
      await database.createTicketComment({
        ticketId,
        content: programsContent,
        authorName: 'Sentinel Diagnostics',
        isInternal: true,
      });
    }

    // Post summary comment
    await database.createTicketComment({
      ticketId,
      content: `**Automatic Diagnostics Collection Complete**\n\nCollected at: ${timestamp}\nTime range: Past 8 hours\n\n- System Errors: ${diagnostics.systemErrors?.length || 0}\n- Application Logs: ${diagnostics.applicationLogs?.length || 0}\n- Security Events: ${diagnostics.securityEvents?.length || 0}\n- Hardware Events: ${diagnostics.hardwareEvents?.length || 0}\n- Network Events: ${diagnostics.networkEvents?.length || 0}\n- Recent Crashes: ${diagnostics.recentCrashes?.length || 0}\n- Active Programs: ${diagnostics.activePrograms?.length || 0}`,
      authorName: 'Sentinel Diagnostics',
      isInternal: true,
    });

    console.log(`Diagnostics posted to ticket ${ticketId}`);
  } catch (error) {
    console.error(`Failed to collect/post diagnostics for ticket ${ticketId}:`, error);
    // Post error comment
    await database.createTicketComment({
      ticketId,
      content: `**Diagnostics Collection Failed**\n\nUnable to collect diagnostics from the device. The agent may be offline or unresponsive.\n\nError: ${error instanceof Error ? error.message : 'Unknown error'}`,
      authorName: 'Sentinel Diagnostics',
      isInternal: true,
    }).catch(() => {});
  }
}

function formatLogEntries(title: string, entries: any[]): string {
  let content = `**${title}** (Past 8 Hours)\n\n`;
  content += `Found ${entries.length} entries:\n\n`;

  for (const entry of entries.slice(0, 50)) { // Limit to 50 entries per comment
    content += `---\n`;
    content += `**Time:** ${entry.timestamp || 'N/A'}\n`;
    content += `**Source:** ${entry.source || 'N/A'}\n`;
    content += `**Level:** ${entry.level || 'N/A'}\n`;
    if (entry.eventId) content += `**Event ID:** ${entry.eventId}\n`;
    content += `**Message:** ${entry.message || 'No message'}\n\n`;
  }

  if (entries.length > 50) {
    content += `\n*... and ${entries.length - 50} more entries*\n`;
  }

  return content;
}

function formatActivePrograms(programs: any[]): string {
  let content = `**Active Programs** (Past 8 Hours)\n\n`;
  content += `Found ${programs.length} programs (sorted alphabetically):\n\n`;
  content += `| Program | Version | Company | Memory (MB) | Session Duration |\n`;
  content += `|---------|---------|---------|-------------|------------------|\n`;

  for (const prog of programs) {
    const name = prog.name || 'Unknown';
    const version = prog.version || '-';
    const company = prog.company || '-';
    const memory = prog.memoryMB ? prog.memoryMB.toFixed(1) : '-';
    const duration = prog.sessionDuration || '-';
    content += `| ${name} | ${version} | ${company} | ${memory} | ${duration} |\n`;
  }

  return content;
}

// App lifecycle
app.whenReady().then(async () => {
  // Create window first so user sees something immediately
  createWindow();

  // Register updater handlers early (before DB init) so updates work even if DB fails
  setupUpdaterHandlers();

  try {
    await initialize();
  } catch (error) {
    console.error('Failed to initialize:', error);
    // Show error dialog and continue - window is already visible
    dialog.showErrorBox(
      'Initialization Error',
      'Failed to start Sentinel: ' + (error as Error).message + '\n\nPlease ensure PostgreSQL is running and accessible.'
    );
  }

  setupAutoUpdater();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    isQuitting = true;
    app.quit();
  }
});

app.on('before-quit', async (event) => {
  if (!isQuitting) {
    isQuitting = true;
    event.preventDefault();

    console.log('Shutting down gracefully...');

    try {
      // Stop servers and close database first
      if (grpcServer) {
        console.log('Stopping gRPC server...');
        await grpcServer.stop();
      }
      if (server) {
        console.log('Stopping WebSocket server...');
        await server.stop();
      }
      if (database) {
        console.log('Closing database...');
        await database.close();
      }

      // Destroy all windows forcefully (not just close)
      BrowserWindow.getAllWindows().forEach(win => {
        win.removeAllListeners('close');
        win.removeAllListeners('closed');
        if (!win.isDestroyed()) {
          win.destroy();
        }
      });

      console.log('Cleanup complete');
    } catch (error) {
      console.error('Error during shutdown:', error);
    }

    // Small delay to ensure cleanup completes, then force quit
    setTimeout(() => {
      app.exit(0);
    }, 100);
  }
});

// Final cleanup on quit
app.on('will-quit', () => {
  console.log('Application will quit');
});

// Handle uncaught exceptions
process.on('uncaughtException', (error) => {
  console.error('Uncaught Exception:', error);
});

process.on('unhandledRejection', (reason, promise) => {
  console.error('Unhandled Rejection at:', promise, 'reason:', reason);
});
