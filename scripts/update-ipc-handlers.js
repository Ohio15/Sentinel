const fs = require('fs');
const path = 'D:/Projects/Sentinel/src/main/main.ts';
let content = fs.readFileSync(path, 'utf8');

// 1. Add setNotifyRenderer after backendRelay initialization
const initBlock = `  // Initialize backend relay for external backend support
  backendRelay = new BackendRelay(database);
  await backendRelay.initialize();`;

const initBlockWithNotify = `  // Initialize backend relay for external backend support
  backendRelay = new BackendRelay(database);
  await backendRelay.initialize();

  // Set up renderer notification for backend relay
  backendRelay.setNotifyRenderer((channel, data) => {
    mainWindow?.webContents.send(channel, data);
  });`;

if (!content.includes('setNotifyRenderer')) {
  content = content.replace(initBlock, initBlockWithNotify);
  console.log('Added setNotifyRenderer call');
}

// 2. Update devices:setMetricsInterval handler
const oldMetricsInterval = `  ipcMain.handle('devices:setMetricsInterval', async (_, deviceId: string, intervalMs: number) => {
    return agentManager.setMetricsInterval(deviceId, intervalMs);
  });`;

const newMetricsInterval = `  ipcMain.handle('devices:setMetricsInterval', async (_, deviceId: string, intervalMs: number) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.setMetricsInterval(deviceId, device.agentId, intervalMs);
      } catch (error) {
        console.error('[IPC] Backend relay setMetricsInterval failed:', error);
        throw error;
      }
    }
    return agentManager.setMetricsInterval(deviceId, intervalMs);
  });`;

if (content.includes(oldMetricsInterval)) {
  content = content.replace(oldMetricsInterval, newMetricsInterval);
  console.log('Updated devices:setMetricsInterval handler');
}

// 3. Update terminal:start handler
const oldTerminalStart = `  ipcMain.handle('terminal:start', async (_, deviceId: string) => {
    return agentManager.startTerminalSession(deviceId);
  });`;

const newTerminalStart = `  ipcMain.handle('terminal:start', async (_, deviceId: string) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.startTerminal(deviceId, device.agentId);
      } catch (error) {
        console.error('[IPC] Backend relay terminal start failed:', error);
        throw error;
      }
    }
    return agentManager.startTerminalSession(deviceId);
  });`;

if (content.includes(oldTerminalStart)) {
  content = content.replace(oldTerminalStart, newTerminalStart);
  console.log('Updated terminal:start handler');
}

// 4. Update terminal:send handler
const oldTerminalSend = `  ipcMain.handle('terminal:send', async (_, sessionId: string, data: string) => {
    return agentManager.sendTerminalData(sessionId, data);
  });`;

const newTerminalSend = `  ipcMain.handle('terminal:send', async (_, sessionId: string, data: string) => {
    // Check if this is a relay session
    if (backendRelay.isRelaySession(sessionId)) {
      return backendRelay.sendTerminalData(sessionId, data);
    }
    return agentManager.sendTerminalData(sessionId, data);
  });`;

if (content.includes(oldTerminalSend)) {
  content = content.replace(oldTerminalSend, newTerminalSend);
  console.log('Updated terminal:send handler');
}

// 5. Update terminal:resize handler
const oldTerminalResize = `  ipcMain.handle('terminal:resize', async (_, sessionId: string, cols: number, rows: number) => {
    return agentManager.resizeTerminal(sessionId, cols, rows);
  });`;

const newTerminalResize = `  ipcMain.handle('terminal:resize', async (_, sessionId: string, cols: number, rows: number) => {
    if (backendRelay.isRelaySession(sessionId)) {
      return backendRelay.resizeTerminal(sessionId, cols, rows);
    }
    return agentManager.resizeTerminal(sessionId, cols, rows);
  });`;

if (content.includes(oldTerminalResize)) {
  content = content.replace(oldTerminalResize, newTerminalResize);
  console.log('Updated terminal:resize handler');
}

// 6. Update terminal:close handler
const oldTerminalClose = `  ipcMain.handle('terminal:close', async (_, sessionId: string) => {
    return agentManager.closeTerminalSession(sessionId);
  });`;

const newTerminalClose = `  ipcMain.handle('terminal:close', async (_, sessionId: string) => {
    if (backendRelay.isRelaySession(sessionId)) {
      return backendRelay.closeTerminal(sessionId);
    }
    return agentManager.closeTerminalSession(sessionId);
  });`;

if (content.includes(oldTerminalClose)) {
  content = content.replace(oldTerminalClose, newTerminalClose);
  console.log('Updated terminal:close handler');
}

// 7. Update files:drives handler
const oldFilesDrives = `  ipcMain.handle('files:drives', async (_, deviceId: string) => {
    return agentManager.listDrives(deviceId);
  });`;

const newFilesDrives = `  ipcMain.handle('files:drives', async (_, deviceId: string) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.listDrives(deviceId, device.agentId);
      } catch (error) {
        console.error('[IPC] Backend relay listDrives failed:', error);
        throw error;
      }
    }
    return agentManager.listDrives(deviceId);
  });`;

if (content.includes(oldFilesDrives)) {
  content = content.replace(oldFilesDrives, newFilesDrives);
  console.log('Updated files:drives handler');
}

// 8. Update files:list handler
const oldFilesList = `  ipcMain.handle('files:list', async (_, deviceId: string, remotePath: string) => {
    return agentManager.listFiles(deviceId, remotePath);
  });`;

const newFilesList = `  ipcMain.handle('files:list', async (_, deviceId: string, remotePath: string) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.listFiles(deviceId, device.agentId, remotePath);
      } catch (error) {
        console.error('[IPC] Backend relay listFiles failed:', error);
        throw error;
      }
    }
    return agentManager.listFiles(deviceId, remotePath);
  });`;

if (content.includes(oldFilesList)) {
  content = content.replace(oldFilesList, newFilesList);
  console.log('Updated files:list handler');
}

// 9. Update files:download handler
const oldFilesDownload = `  ipcMain.handle('files:download', async (_, deviceId: string, remotePath: string, localPath: string) => {
    return agentManager.downloadFile(deviceId, remotePath, localPath);
  });`;

const newFilesDownload = `  ipcMain.handle('files:download', async (_, deviceId: string, remotePath: string, localPath: string) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.downloadFile(deviceId, device.agentId, remotePath, localPath);
      } catch (error) {
        console.error('[IPC] Backend relay downloadFile failed:', error);
        throw error;
      }
    }
    return agentManager.downloadFile(deviceId, remotePath, localPath);
  });`;

if (content.includes(oldFilesDownload)) {
  content = content.replace(oldFilesDownload, newFilesDownload);
  console.log('Updated files:download handler');
}

// 10. Update files:upload handler
const oldFilesUpload = `  ipcMain.handle('files:upload', async (_, deviceId: string, localPath: string, remotePath: string) => {
    return agentManager.uploadFile(deviceId, localPath, remotePath);
  });`;

const newFilesUpload = `  ipcMain.handle('files:upload', async (_, deviceId: string, localPath: string, remotePath: string) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.uploadFile(deviceId, device.agentId, localPath, remotePath);
      } catch (error) {
        console.error('[IPC] Backend relay uploadFile failed:', error);
        throw error;
      }
    }
    return agentManager.uploadFile(deviceId, localPath, remotePath);
  });`;

if (content.includes(oldFilesUpload)) {
  content = content.replace(oldFilesUpload, newFilesUpload);
  console.log('Updated files:upload handler');
}

// 11. Update files:scan handler
const oldFilesScan = `  ipcMain.handle('files:scan', async (_, deviceId: string, path: string, maxDepth: number) => {
    return agentManager.scanDirectory(deviceId, path, maxDepth);
  });`;

const newFilesScan = `  ipcMain.handle('files:scan', async (_, deviceId: string, path: string, maxDepth: number) => {
    if (await needsRelay(deviceId)) {
      try {
        const device = await database.getDevice(deviceId);
        if (!device?.agentId) throw new Error('Device not found');
        return await backendRelay.scanDirectory(deviceId, device.agentId, path, maxDepth);
      } catch (error) {
        console.error('[IPC] Backend relay scanDirectory failed:', error);
        throw error;
      }
    }
    return agentManager.scanDirectory(deviceId, path, maxDepth);
  });`;

if (content.includes(oldFilesScan)) {
  content = content.replace(oldFilesScan, newFilesScan);
  console.log('Updated files:scan handler');
}

fs.writeFileSync(path, content, 'utf8');
console.log('Done updating IPC handlers');
