const fs = require('fs');
const path = 'D:/Projects/Sentinel/src/main/main.ts';
let content = fs.readFileSync(path, 'utf-8');

// Fix devices:list handler
const oldList = `ipcMain.handle('devices:list', async () => {
    return database.getDevices();
  });`;

const newList = `ipcMain.handle('devices:list', async () => {
    const devices = await database.getDevices();
    // Override status based on actual WebSocket connection state
    return devices.map(device => ({
      ...device,
      status: device.agentId && agentManager.isAgentConnected(device.agentId) ? 'online' : 'offline'
    }));
  });`;

// Fix devices:get handler
const oldGet = `ipcMain.handle('devices:get', async (_, id: string) => {
    console.log('[IPC] devices:get called with id:', id);
    const device = await database.getDevice(id);
    console.log('[IPC] devices:get result:', device ? device.hostname : 'null');
    return device;
  });`;

const newGet = `ipcMain.handle('devices:get', async (_, id: string) => {
    console.log('[IPC] devices:get called with id:', id);
    const device = await database.getDevice(id);
    console.log('[IPC] devices:get result:', device ? device.hostname : 'null');
    if (device) {
      // Override status based on actual WebSocket connection state
      const isConnected = device.agentId && agentManager.isAgentConnected(device.agentId);
      console.log('[IPC] devices:get isConnected:', isConnected, 'agentId:', device.agentId);
      return {
        ...device,
        status: isConnected ? 'online' : 'offline'
      };
    }
    return device;
  });`;

let modified = false;

if (content.includes(oldList)) {
  content = content.replace(oldList, newList);
  modified = true;
  console.log('Fixed devices:list handler');
} else {
  console.log('devices:list pattern not found or already modified');
}

if (content.includes(oldGet)) {
  content = content.replace(oldGet, newGet);
  modified = true;
  console.log('Fixed devices:get handler');
} else {
  console.log('devices:get pattern not found or already modified');
}

if (modified) {
  fs.writeFileSync(path, content);
  console.log('Changes saved to main.ts');
} else {
  console.log('No changes needed');
}
