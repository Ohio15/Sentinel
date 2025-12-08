import { app, BrowserWindow, ipcMain, shell, dialog, Menu } from 'electron';
import { autoUpdater } from 'electron-updater';
import * as path from 'path';
import * as fs from 'fs';
import { Database } from './database';
import { Server } from './server';
import { AgentManager } from './agents';
import * as os from 'os';

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

const isDev = process.env.NODE_ENV === 'development' || !app.isPackaged;

function createWindow(): void {
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
    show: false,
  });

  // Load the app
  if (isDev) {
    mainWindow.loadURL('http://localhost:5173');
    mainWindow.webContents.openDevTools();
  } else {
    mainWindow.loadFile(path.join(__dirname, '../renderer/index.html'));
  }

  // Show window when ready
  mainWindow.once('ready-to-show', () => {
    mainWindow?.show();
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

  // Setup IPC handlers
  setupIpcHandlers();
}

function setupIpcHandlers(): void {
  // Device management
  ipcMain.handle('devices:list', async () => {
    return database.getDevices();
  });

  ipcMain.handle('devices:get', async (_, id: string) => {
    return database.getDevice(id);
  });

  ipcMain.handle('devices:delete', async (_, id: string) => {
    return database.deleteDevice(id);
  });

  ipcMain.handle('devices:getMetrics', async (_, deviceId: string, hours: number) => {
    return database.getDeviceMetrics(deviceId, hours);
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
  ipcMain.handle('files:list', async (_, deviceId: string, remotePath: string) => {
    return agentManager.listFiles(deviceId, remotePath);
  });

  ipcMain.handle('files:download', async (_, deviceId: string, remotePath: string, localPath: string) => {
    return agentManager.downloadFile(deviceId, remotePath, localPath);
  });

  ipcMain.handle('files:upload', async (_, deviceId: string, localPath: string, remotePath: string) => {
    return agentManager.uploadFile(deviceId, localPath, remotePath);
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

  // Settings
  ipcMain.handle('settings:get', async () => {
    return database.getSettings();
  });

  ipcMain.handle('settings:update', async (_, settings: any) => {
    return database.updateSettings(settings);
  });

  // Server info
  ipcMain.handle('server:getInfo', async () => {
    return {
      port: server.getPort(),
      agentCount: agentManager.getConnectedAgentCount(),
      enrollmentToken: server.getEnrollmentToken(),
    };
  });

  ipcMain.handle('server:regenerateToken', async () => {
    return server.regenerateEnrollmentToken();
  });

  // Agent installer
  ipcMain.handle('agent:getInstallerCommand', async (_, platform: string) => {
    return server.getAgentInstallerCommand(platform);
  });

  // Agent download with save dialog
  ipcMain.handle('agent:download', async (_, platform: string) => {
    // Get the downloads directory - uses resources folder when packaged
    console.log('Agent download - isPackaged:', app.isPackaged);
    console.log('Agent download - resourcesPath:', process.resourcesPath);
    const downloadsDir = app.isPackaged
      ? path.join(process.resourcesPath, 'downloads')
      : path.join(__dirname, '..', '..', 'downloads');

    // Determine filename based on platform
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
        return { success: false, error: 'Unsupported platform' };
    }

    const sourcePath = path.join(downloadsDir, filename);
    console.log('Agent download - sourcePath:', sourcePath);
    console.log('Agent download - exists:', fs.existsSync(sourcePath));

    // Check if source file exists
    if (!fs.existsSync(sourcePath)) {
      return {
        success: false,
        error: `Agent binary not found at: ${sourcePath}`,
      };
    }

    // Show save dialog
    const result = await dialog.showSaveDialog(mainWindow!, {
      title: 'Save Agent Executable',
      defaultPath: filename,
      filters: platform === 'windows'
        ? [{ name: 'Executable', extensions: ['exe'] }]
        : [{ name: 'All Files', extensions: ['*'] }],
    });

    if (result.canceled || !result.filePath) {
      return { success: false, canceled: true };
    }

    // Read, embed config, and save file
    try {
      const binaryData = fs.readFileSync(sourcePath);

      // Get server info for embedding
      const localIp = getLocalIpAddress();
      const serverPort = server.getPort();
      const serverUrl = `http://${localIp}:${serverPort}`;
      const enrollmentToken = server.getEnrollmentToken();

      // Embed server URL and enrollment token into binary
      const modifiedBinary = embedConfigInBinary(binaryData, serverUrl, enrollmentToken);

      // Write the modified binary
      await fs.promises.writeFile(result.filePath, modifiedBinary);

      return {
        success: true,
        filePath: result.filePath,
        size: modifiedBinary.length,
      };
    } catch (error: any) {
      return {
        success: false,
        error: `Failed to save file: ${error.message}`,
      };
    }
  });
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

  ipcMain.handle('install-update', () => {
    autoUpdater.quitAndInstall(false, true);
  });

  ipcMain.handle('get-app-version', () => {
    return app.getVersion();
  });
}

// App lifecycle
app.whenReady().then(async () => {
  await initialize();
  createWindow();
  setupAutoUpdater();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

app.on('before-quit', async () => {
  await server?.stop();
  await database?.close();
});

// Handle uncaught exceptions
process.on('uncaughtException', (error) => {
  console.error('Uncaught Exception:', error);
});

process.on('unhandledRejection', (reason, promise) => {
  console.error('Unhandled Rejection at:', promise, 'reason:', reason);
});
