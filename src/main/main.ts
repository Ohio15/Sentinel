import { app, BrowserWindow, ipcMain, shell, dialog, Menu } from 'electron';
import { autoUpdater } from 'electron-updater';
import * as path from 'path';
import * as fs from 'fs';
import * as dotenv from 'dotenv';
import { LocalStore } from './local-store';
import { listCertificates, renewCertificates, getCACertificate, getCertsDir } from './cert-manager';
import { BackendRelay } from './backend-relay';
import * as os from 'os';

// Note: Server-only architecture - all agent operations go through BackendRelay
// AgentManager, Server, and GrpcServer have been removed from Electron
// Agents connect to the Go server (Docker or standalone), not to Electron

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
let database: LocalStore;
let backendRelay: BackendRelay;
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
  // Initialize database (for local caching and settings)
  database = new LocalStore();
  await database.initialize();

  // Initialize backend relay - this is now the ONLY way to communicate with agents
  // Agents connect to the Go server (Docker or standalone), not to Electron
  backendRelay = new BackendRelay(database);
  await backendRelay.initialize();

  // Set up renderer notification for backend relay
  backendRelay.setNotifyRenderer((channel, data) => {
    mainWindow?.webContents.send(channel, data);
  });

  console.log('Server-only architecture initialized');
  console.log('Agents connect to Go server, Electron is UI client only');

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
  // Backend relay configuration
  ipcMain.handle('backend:getConfig', async () => {
    return {
      url: backendRelay.getBackendUrl(),
      isConfigured: backendRelay.isConfigured(),
      isAuthenticated: backendRelay.isAuthenticated(),
    };
  });

  ipcMain.handle('backend:setUrl', async (_, url: string) => {
    backendRelay.setBackendUrl(url);
    await database.updateSettings({ externalBackendUrl: url });
    return { success: true };
  });

  ipcMain.handle('backend:authenticate', async (_, username: string, password: string) => {
    try {
      const success = await backendRelay.authenticate(username, password);
      return { success };
    } catch (error) {
      return { success: false, error: (error as Error).message };
    }
  });

  // Helper function to ensure backend is connected
  function ensureBackendConnected(): void {
    if (!backendRelay.isConfigured()) {
      throw new Error('Backend server not configured. Go to Settings to configure the server URL.');
    }
    if (!backendRelay.isAuthenticated()) {
      throw new Error('Not authenticated with backend server. Go to Settings to log in.');
    }
  }

  // Helper function to get device with agentId
  async function getDeviceWithAgent(deviceId: string): Promise<{ id: string; agentId: string }> {
    const device = await database.getDevice(deviceId);
    if (!device) throw new Error('Device not found');
    if (!device.agentId) throw new Error('Device has no agent ID');
    return device;
  }

  // Device management
  ipcMain.handle('devices:list', async (_, clientId?: string) => {
    const isConfigured = backendRelay.isConfigured();
    const isAuthenticated = backendRelay.isAuthenticated();
    console.log('[IPC] devices:list called, backend configured:', isConfigured, 'authenticated:', isAuthenticated);

    // When backend is authenticated, fetch fresh data from API
    if (isConfigured && isAuthenticated) {
      try {
        const devices = await backendRelay.getDevices();
        console.log('[IPC] devices:list from backend:', devices?.length, 'devices');
        // Also sync to local database for offline access
        backendRelay.syncDevices().catch(err => console.error('[IPC] Background sync failed:', err));
        return devices;
      } catch (error) {
        console.error('[IPC] Failed to fetch devices from backend, falling back to local:', error);
      }
    }

    console.log('[IPC] devices:list using local database fallback');
    // Fallback to local database
    const devices = await database.getDevices(clientId);
    // Trust database status - agents report to Go server which updates the database
    // Consider online if lastSeen is within 90 seconds (heartbeat interval + buffer)
    return devices.map(device => {
      if (device.isDisabled) {
        return { ...device, status: 'disabled' };
      }
      if (device.status === 'online' && device.lastSeen) {
        const lastSeenTime = new Date(device.lastSeen).getTime();
        const isRecentlyActive = (Date.now() - lastSeenTime) < 90000; // 90 seconds
        if (isRecentlyActive) {
          return { ...device, status: 'online' };
        }
      }
      return { ...device, status: 'offline' };
    });
  });

  ipcMain.handle('devices:get', async (_, id: string) => {
    console.log('[IPC] devices:get called with id:', id);
    
    // When backend is authenticated, fetch fresh data from API
    if (backendRelay.isConfigured() && backendRelay.isAuthenticated()) {
      try {
        const device = await backendRelay.getDevice(id);
        console.log('[IPC] devices:get from backend:', device ? device.hostname : 'null');
        return device;
      } catch (error) {
        console.error('[IPC] Failed to fetch device from backend, falling back to local:', error);
      }
    }
    
    // Fallback to local database
    const device = await database.getDevice(id);
    console.log('[IPC] devices:get result:', device ? device.hostname : 'null');
    if (device) {
      // Trust database status with recent activity check
      let status = 'offline';
      if (device.isDisabled) {
        status = 'disabled';
      } else if (device.status === 'online' && device.lastSeen) {
        const lastSeenTime = new Date(device.lastSeen).getTime();
        const isRecentlyActive = (Date.now() - lastSeenTime) < 90000;
        if (isRecentlyActive) status = 'online';
      }
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
    ensureBackendConnected();
    return backendRelay.disableDevice(id);
  });

  ipcMain.handle('devices:enable', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.enableDevice(id);
  });

  ipcMain.handle('devices:uninstall', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.uninstallAgent(id);
  });

  ipcMain.handle('devices:update', async (_, id: string, updates: { displayName?: string; tags?: string[] }) => {
    return database.updateDevice(id, updates);
  });


  ipcMain.handle('devices:ping', async (_, deviceId: string) => {
    ensureBackendConnected();
    return backendRelay.pingDevice(deviceId);
  });

  ipcMain.handle('devices:getMetrics', async (_, deviceId: string, hours: number) => {
    return database.getDeviceMetrics(deviceId, hours);
  });

  ipcMain.handle('devices:setMetricsInterval', async (_, deviceId: string, intervalMs: number) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.setMetricsInterval(deviceId, device.agentId, intervalMs);
  });

  // Commands
  ipcMain.handle('commands:execute', async (_, deviceId: string, command: string, type: string) => {
    ensureBackendConnected();
    return backendRelay.executeCommand(deviceId, command, type);
  });

  ipcMain.handle('commands:getHistory', async (_, deviceId: string) => {
    return database.getCommandHistory(deviceId);
  });

  // Remote terminal
  ipcMain.handle('terminal:start', async (_, deviceId: string) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.startTerminal(deviceId, device.agentId);
  });

  ipcMain.handle('terminal:send', async (_, sessionId: string, data: string) => {
    return backendRelay.sendTerminalData(sessionId, data);
  });

  ipcMain.handle('terminal:resize', async (_, sessionId: string, cols: number, rows: number) => {
    return backendRelay.resizeTerminal(sessionId, cols, rows);
  });

  ipcMain.handle('terminal:close', async (_, sessionId: string) => {
    return backendRelay.closeTerminal(sessionId);
  });

  // File transfer
  ipcMain.handle('files:drives', async (_, deviceId: string) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.listDrives(deviceId, device.agentId);
  });

  ipcMain.handle('files:list', async (_, deviceId: string, remotePath: string) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.listFiles(deviceId, device.agentId, remotePath);
  });

  ipcMain.handle('files:download', async (_, deviceId: string, remotePath: string, localPath: string) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.downloadFile(deviceId, device.agentId, remotePath, localPath);
  });

  ipcMain.handle('files:upload', async (_, deviceId: string, localPath: string, remotePath: string) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.uploadFile(deviceId, device.agentId, localPath, remotePath);
  });

  ipcMain.handle('files:scan', async (_, deviceId: string, path: string, maxDepth: number) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);
    return backendRelay.scanDirectory(deviceId, device.agentId, path, maxDepth);
  });

  ipcMain.handle('files:downloadToSandbox', async (_, deviceId: string, remotePath: string) => {
    ensureBackendConnected();
    const device = await getDeviceWithAgent(deviceId);

    const pathModule = await import('path');
    const fsModule = await import('fs');
    const osModule = await import('os');
    const cryptoModule = await import('crypto');

    // Create sandbox directory in user's app data
    const sandboxDir = pathModule.join(osModule.homedir(), '.sentinel', 'sandbox');
    if (!fsModule.existsSync(sandboxDir)) {
      fsModule.mkdirSync(sandboxDir, { recursive: true });
    }

    // Generate unique filename with timestamp and hash
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const hash = cryptoModule.randomBytes(4).toString('hex');
    const originalName = pathModule.basename(remotePath);
    const sandboxFilename = `${timestamp}_${hash}_${originalName}`;
    const localPath = pathModule.join(sandboxDir, sandboxFilename);

    await backendRelay.downloadFile(deviceId, device.agentId, remotePath, localPath);

    // Calculate SHA256 hash of downloaded file
    const fileBuffer = fsModule.readFileSync(localPath);
    const sha256Hash = cryptoModule.createHash('sha256').update(fileBuffer).digest('hex');

    return {
      localPath,
      sandboxDir,
      filename: sandboxFilename,
      originalName,
      sha256: sha256Hash,
      size: fileBuffer.length,
    };
  });

  // Remote desktop - not yet implemented in server-only mode
  // These require WebRTC/WebSocket relay through the Go server
  ipcMain.handle('remote:startSession', async () => {
    throw new Error('Remote desktop is not yet available. Feature coming soon.');
  });

  ipcMain.handle('remote:stopSession', async () => {
    throw new Error('Remote desktop is not yet available. Feature coming soon.');
  });

  ipcMain.handle('remote:sendInput', async () => {
    throw new Error('Remote desktop is not yet available. Feature coming soon.');
  });

  // WebRTC Remote Desktop - not yet implemented in server-only mode
  ipcMain.handle('webrtc:start', async () => {
    throw new Error('WebRTC remote desktop is not yet available. Feature coming soon.');
  });

  ipcMain.handle('webrtc:stop', async () => {
    throw new Error('WebRTC remote desktop is not yet available. Feature coming soon.');
  });

  ipcMain.handle('webrtc:signal', async () => {
    throw new Error('WebRTC remote desktop is not yet available. Feature coming soon.');
  });

  ipcMain.handle('webrtc:setQuality', async () => {
    throw new Error('WebRTC remote desktop is not yet available. Feature coming soon.');
  });


  // Alerts - route through backend when connected
  ipcMain.handle('alerts:list', async () => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getAlerts();
    }
    return database.getAlerts();
  });

  ipcMain.handle('alerts:acknowledge', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.acknowledgeAlert(id);
  });

  ipcMain.handle('alerts:resolve', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.resolveAlert(id);
  });

  ipcMain.handle('alerts:getRules', async () => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getAlertRules();
    }
    return database.getAlertRules();
  });

  ipcMain.handle('alerts:createRule', async (_, rule: any) => {
    ensureBackendConnected();
    return backendRelay.createAlertRule(rule);
  });

  ipcMain.handle('alerts:updateRule', async (_, id: string, rule: any) => {
    ensureBackendConnected();
    return backendRelay.updateAlertRule(id, rule);
  });

  ipcMain.handle('alerts:deleteRule', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteAlertRule(id);
  });

  // Scripts - route through backend when connected
  ipcMain.handle('scripts:list', async () => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getScripts();
    }
    return database.getScripts();
  });

  ipcMain.handle('scripts:create', async (_, script: any) => {
    ensureBackendConnected();
    return backendRelay.createScript(script);
  });

  ipcMain.handle('scripts:update', async (_, id: string, script: any) => {
    ensureBackendConnected();
    return backendRelay.updateScript(id, script);
  });

  ipcMain.handle('scripts:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteScript(id);
  });

  ipcMain.handle('scripts:execute', async (_, scriptId: string, deviceIds: string[]) => {
    ensureBackendConnected();
    return backendRelay.executeScript(scriptId, deviceIds);
  });

  // Tickets - route through backend when connected
  ipcMain.handle('tickets:list', async (_, filters?: { status?: string; priority?: string; assignedTo?: string; deviceId?: string; clientId?: string }) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTickets(filters);
    }
    return database.getTickets(filters);
  });

  ipcMain.handle('tickets:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicket(id);
    }
    return database.getTicket(id);
  });

  ipcMain.handle('tickets:create', async (_, ticket: any) => {
    ensureBackendConnected();
    const createdTicket = await backendRelay.createTicket(ticket);

    // If ticket has a deviceId, collect diagnostics in the background
    if (ticket.deviceId) {
      collectAndPostDiagnostics(createdTicket.id, ticket.deviceId).catch(err => {
        console.error('Failed to collect diagnostics:', err);
      });
    }

    return createdTicket;
  });

  ipcMain.handle('tickets:update', async (_, id: string, updates: any) => {
    ensureBackendConnected();
    return backendRelay.updateTicket(id, updates);
  });

  ipcMain.handle('tickets:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteTicket(id);
  });

  ipcMain.handle('tickets:getComments', async (_, ticketId: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketComments(ticketId);
    }
    return database.getTicketComments(ticketId);
  });

  ipcMain.handle('tickets:addComment', async (_, comment: any) => {
    ensureBackendConnected();
    return backendRelay.addTicketComment(comment.ticketId, comment.content, comment.isInternal);
  });

  ipcMain.handle('tickets:getActivity', async (_, ticketId: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketTimeline(ticketId);
    }
    return database.getTicketActivity(ticketId);
  });

  ipcMain.handle('tickets:getStats', async () => {
    // Stats still from local DB as summary
    return database.getTicketStats();
  });

  ipcMain.handle('tickets:getTemplates', async () => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketTemplates();
    }
    return database.getTicketTemplates();
  });

  ipcMain.handle('tickets:createTemplate', async (_, template: any) => {
    ensureBackendConnected();
    return backendRelay.createTicketTemplate(template);
  });

  ipcMain.handle('tickets:updateTemplate', async (_, id: string, template: any) => {
    ensureBackendConnected();
    return backendRelay.updateTicketTemplate(id, template);
  });

  ipcMain.handle('tickets:deleteTemplate', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteTicketTemplate(id);
  });

  // SLA Policies - route through backend when connected
  ipcMain.handle('sla:list', async (_, clientId?: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getSLAPolicies(clientId);
    }
    return database.getSLAPolicies(clientId);
  });

  ipcMain.handle('sla:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getSLAPolicy(id);
    }
    return database.getSLAPolicy(id);
  });

  ipcMain.handle('sla:create', async (_, policy: any) => {
    ensureBackendConnected();
    return backendRelay.createSLAPolicy(policy);
  });

  ipcMain.handle('sla:update', async (_, id: string, updates: any) => {
    ensureBackendConnected();
    return backendRelay.updateSLAPolicy(id, updates);
  });

  ipcMain.handle('sla:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteSLAPolicy(id);
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

  // Ticket Categories - route through backend when connected
  ipcMain.handle('categories:list', async (_, clientId?: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketCategories();
    }
    return database.getTicketCategories(clientId);
  });

  ipcMain.handle('categories:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketCategory(id);
    }
    return database.getTicketCategory(id);
  });

  ipcMain.handle('categories:create', async (_, category: any) => {
    ensureBackendConnected();
    return backendRelay.createTicketCategory(category);
  });

  ipcMain.handle('categories:update', async (_, id: string, updates: any) => {
    ensureBackendConnected();
    return backendRelay.updateTicketCategory(id, updates);
  });

  ipcMain.handle('categories:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteTicketCategory(id);
  });

  // Ticket Tags - route through backend when connected
  ipcMain.handle('tags:list', async (_, clientId?: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketTags();
    }
    return database.getTicketTags(clientId);
  });

  ipcMain.handle('tags:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketTag(id);
    }
    return database.getTicketTag(id);
  });

  ipcMain.handle('tags:create', async (_, tag: any) => {
    ensureBackendConnected();
    return backendRelay.createTicketTag(tag);
  });

  ipcMain.handle('tags:update', async (_, id: string, updates: any) => {
    ensureBackendConnected();
    return backendRelay.updateTicketTag(id, updates);
  });

  ipcMain.handle('tags:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteTicketTag(id);
  });

  ipcMain.handle('tags:getAssignments', async (_, ticketId: string) => {
    return database.getTicketTagAssignments(ticketId);
  });

  ipcMain.handle('tags:assign', async (_, ticketId: string, tagIds: string[], assignedBy?: string) => {
    if (backendRelay.isAuthenticated()) {
      // Add tags one by one via API
      for (const tagId of tagIds) {
        await backendRelay.addTagToTicket(ticketId, tagId);
      }
      return { success: true };
    }
    return database.assignTagsToTicket(ticketId, tagIds, assignedBy);
  });

  // Ticket Links - route through backend when connected
  ipcMain.handle('links:list', async (_, ticketId: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getTicketLinks(ticketId);
    }
    return database.getTicketLinks(ticketId);
  });

  ipcMain.handle('links:create', async (_, link: any) => {
    ensureBackendConnected();
    return backendRelay.createTicketLink(link.ticketId, {
      linkedTicketId: link.linkedTicketId,
      linkType: link.linkType
    });
  });

  ipcMain.handle('links:delete', async (_, id: string) => {
    ensureBackendConnected();
    // For delete, we need ticket ID - fall back to local for now
    return database.deleteTicketLink(id);
  });

  // Ticket Analytics
  ipcMain.handle('analytics:tickets', async (_, params: { clientId?: string; dateFrom?: string; dateTo?: string }) => {
    return database.getTicketAnalytics(params);
  });

  // Knowledge Base Categories - route through backend when connected
  ipcMain.handle('kb:categories:list', async () => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getKBCategories();
    }
    return database.getKBCategories();
  });

  ipcMain.handle('kb:categories:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getKBCategory(id);
    }
    return database.getKBCategory(id);
  });

  ipcMain.handle('kb:categories:create', async (_, category: any) => {
    ensureBackendConnected();
    return backendRelay.createKBCategory(category);
  });

  ipcMain.handle('kb:categories:update', async (_, id: string, updates: any) => {
    ensureBackendConnected();
    return backendRelay.updateKBCategory(id, updates);
  });

  ipcMain.handle('kb:categories:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteKBCategory(id);
  });

  // Knowledge Base Articles - route through backend when connected
  ipcMain.handle('kb:articles:list', async (_, options?: { categoryId?: string; status?: string; featured?: boolean; limit?: number; offset?: number }) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getKBArticles(options?.categoryId);
    }
    return database.getKBArticles(options);
  });

  ipcMain.handle('kb:articles:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getKBArticle(id);
    }
    return database.getKBArticle(id);
  });

  ipcMain.handle('kb:articles:getBySlug', async (_, slug: string) => {
    return database.getKBArticleBySlug(slug);
  });

  ipcMain.handle('kb:articles:create', async (_, article: any) => {
    ensureBackendConnected();
    return backendRelay.createKBArticle(article);
  });

  ipcMain.handle('kb:articles:update', async (_, id: string, updates: any) => {
    ensureBackendConnected();
    return backendRelay.updateKBArticle(id, updates);
  });

  ipcMain.handle('kb:articles:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteKBArticle(id);
  });

  ipcMain.handle('kb:articles:search', async (_, query: string, limit?: number) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getKBArticles(undefined, query);
    }
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
    // Portal services are handled by the Go server, not Electron
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



  // Clients - route through backend when connected
  ipcMain.handle('clients:list', async () => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getClients();
    }
    return database.getClientsWithCounts();
  });

  ipcMain.handle('clients:get', async (_, id: string) => {
    if (backendRelay.isAuthenticated()) {
      return backendRelay.getClient(id);
    }
    return database.getClient(id);
  });

  ipcMain.handle('clients:create', async (_, client: { name: string; description?: string; color?: string; logoUrl?: string; logoWidth?: number; logoHeight?: number }) => {
    ensureBackendConnected();
    return backendRelay.createClient(client);
  });

  ipcMain.handle('clients:update', async (_, id: string, client: { name?: string; description?: string; color?: string; logoUrl?: string; logoWidth?: number; logoHeight?: number }) => {
    ensureBackendConnected();
    return backendRelay.updateClient(id, client);
  });

  ipcMain.handle('clients:delete', async (_, id: string) => {
    ensureBackendConnected();
    return backendRelay.deleteClient(id);
  });

  ipcMain.handle('devices:assignToClient', async (_, deviceId: string, clientId: string | null) => {
    if (backendRelay.isAuthenticated()) {
      await backendRelay.updateDevice(deviceId, { clientId });
      return backendRelay.getDevice(deviceId);
    }
    await database.assignDeviceToClient(deviceId, clientId);
    return database.getDevice(deviceId);
  });

  ipcMain.handle('devices:bulkAssignToClient', async (_, deviceIds: string[], clientId: string | null) => {
    if (backendRelay.isAuthenticated()) {
      for (const deviceId of deviceIds) {
        await backendRelay.updateDevice(deviceId, { clientId });
      }
      return { success: true, count: deviceIds.length };
    }
    await database.bulkAssignDevicesToClient(deviceIds, clientId);
    return { success: true, count: deviceIds.length };
  });

  // Server info - now returns info about the connected Go backend
  ipcMain.handle('server:getInfo', async () => {
    // Count online agents from database (agents connect to Go server)
    let onlineCount = 0;
    try {
      const devices = await database.getDevices();
      const now = Date.now();
      onlineCount = devices.filter(d => {
        if (d.status === 'online' && d.lastSeen) {
          const lastSeenTime = new Date(d.lastSeen).getTime();
          return (now - lastSeenTime) < 90000; // 90 seconds
        }
        return false;
      }).length;
    } catch (e) { /* ignore */ }

    const backendUrl = backendRelay.getBackendUrl();
    return {
      backendUrl: backendUrl || 'Not configured',
      isConnected: backendRelay.isAuthenticated(),
      isWebSocketConnected: backendRelay.isWebSocketConnected(),
      agentCount: onlineCount,
      // Legacy fields for compatibility - agents connect to Go server, not Electron
      port: 0,
      grpcPort: 0,
      grpcAgentCount: 0,
      enrollmentToken: 'Configure in Go server',
    };
  });

  ipcMain.handle('server:regenerateToken', async () => {
    // Token management is handled by the Go server
    throw new Error('Token regeneration is managed by the Go server. Use the server admin interface.');
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
    // Certificate distribution is handled by the Go server
    throw new Error('Certificate distribution is managed by the Go server. Use the server admin interface.');
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


  // Agent installer - agents now connect to Go server, not Electron
  ipcMain.handle('agent:getInstallerCommand', async (_, platform: string) => {
    const serverUrl = backendRelay.getBackendUrl();
    if (!serverUrl) {
      throw new Error('Backend server not configured. Go to Settings to configure the server URL.');
    }
    // Enrollment token is managed by the Go server
    const platformCommands: Record<string, string> = {
      windows: `# Download agent from ${serverUrl}/api/agents/download/windows\n# Install with: sentinel-agent.exe -install -server=${serverUrl} -token=YOUR_TOKEN`,
      macos: `curl -o sentinel-agent ${serverUrl}/api/agents/download/macos && chmod +x sentinel-agent && sudo ./sentinel-agent -install -server=${serverUrl} -token=YOUR_TOKEN`,
      linux: `curl -o sentinel-agent ${serverUrl}/api/agents/download/linux && chmod +x sentinel-agent && sudo ./sentinel-agent -install -server=${serverUrl} -token=YOUR_TOKEN`,
    };
    return platformCommands[platform.toLowerCase()] || 'Unsupported platform';
  });

  // Agent download with save dialog
  ipcMain.handle('agent:download', async (_, platform: string) => {
    const serverUrl = backendRelay.getBackendUrl();
    if (!serverUrl) {
      return { success: false, error: 'Backend server not configured. Go to Settings to configure the server URL.' };
    }

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

    try {
      const rawInstallerData = fs.readFileSync(sourcePath);

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      // Note: Token needs to be obtained from Go server
      const installerData = platform.toLowerCase() !== 'windows'
        ? embedConfigInInstaller(rawInstallerData, serverUrl, 'YOUR_TOKEN')
        : rawInstallerData;

      await fs.promises.writeFile(result.filePath, installerData);

      // Return install command based on platform
      const installCommands: Record<string, string> = {
        windows: `msiexec /i "${result.filePath}" SERVERURL="${serverUrl}" ENROLLMENTTOKEN="YOUR_TOKEN" /qn`,
        macos: `sudo installer -pkg "${result.filePath}" -target /`,
        linux: `sudo dpkg -i "${result.filePath}"`,
      };

      return {
        success: true,
        filePath: result.filePath,
        size: installerData.length,
        installCommand: installCommands[platform.toLowerCase()],
        note: 'Replace YOUR_TOKEN with the enrollment token from the Go server.',
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
    const serverUrl = backendRelay.getBackendUrl();
    if (!serverUrl) {
      return { success: false, error: 'Backend server not configured. Go to Settings to configure the server URL.' };
    }

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
      const msiData = fs.readFileSync(sourcePath);

      // Write the MSI file
      await fs.promises.writeFile(result.filePath, msiData);

      return {
        success: true,
        filePath: result.filePath,
        size: msiData.length,
        installCommand: `msiexec /i "${result.filePath}" SERVERURL="${serverUrl}" ENROLLMENTTOKEN="YOUR_TOKEN" /qn`,
        note: 'Replace YOUR_TOKEN with the enrollment token from the Go server.',
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
    const serverUrl = backendRelay.getBackendUrl();
    if (!serverUrl) {
      throw new Error('Backend server not configured. Go to Settings to configure the server URL.');
    }

    return {
      serverUrl,
      enrollmentToken: 'Obtain from Go server admin interface',
      command: `msiexec /i "sentinel-agent.msi" SERVERURL="${serverUrl}" ENROLLMENTTOKEN="YOUR_TOKEN" /qn`,
      note: 'Replace YOUR_TOKEN with the enrollment token from the Go server.',
    };
  });

  // Execute PowerShell install script with UAC elevation
  ipcMain.handle('agent:runPowerShellInstall', async () => {
    const serverUrl = backendRelay.getBackendUrl();
    if (!serverUrl) {
      return { success: false, error: 'Backend server not configured. Go to Settings to configure the server URL.' };
    }

    // PowerShell script that downloads and installs the agent
    // Note: Token needs to be obtained from Go server
    const psScript = `
$ErrorActionPreference = 'Stop'
$token = Read-Host 'Enter enrollment token from Go server'
$agentPath = Join-Path $env:TEMP 'sentinel-agent.exe'
Write-Host 'Downloading Sentinel Agent...' -ForegroundColor Cyan
Invoke-WebRequest -Uri '${serverUrl}/api/agents/download/windows' -OutFile $agentPath -UseBasicParsing
Write-Host 'Installing agent...' -ForegroundColor Green
Start-Process -FilePath $agentPath -ArgumentList '--install',"--server=${serverUrl}","--token=$token" -Wait
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
// Note: In server-only mode, diagnostics collection is handled by the Go server
async function collectAndPostDiagnostics(ticketId: string, deviceId: string): Promise<void> {
  try {
    console.log(`[Diagnostics] Ticket ${ticketId} - diagnostics collection is handled by Go server`);
    // In server-only mode, we post a note that diagnostics are not yet available
    await database.createTicketComment({
      ticketId,
      content: `**Automatic Diagnostics**\n\nDiagnostics collection for device ${deviceId} is managed by the Go server. Check the server logs for detailed diagnostics.\n\n*Feature enhancement: Server-side diagnostics collection coming soon.*`,
      authorName: 'Sentinel System',
      isInternal: true,
    });
  } catch (error) {
    console.error(`Failed to post diagnostics note for ticket ${ticketId}:`, error);
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
      // Close database connection
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
