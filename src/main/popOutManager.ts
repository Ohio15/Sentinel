import { BrowserWindow, ipcMain, screen } from 'electron';
import * as path from 'path';

interface PopOutWindow {
  id: string;
  window: BrowserWindow;
  deviceId: string;
  tab: string;
  createdAt: number;
}

interface PopOutConfig {
  deviceId: string;
  tab: string;
  width?: number;
  height?: number;
  x?: number;
  y?: number;
}

class PopOutManager {
  private windows: Map<string, PopOutWindow> = new Map();
  private mainWindow: BrowserWindow | null = null;

  setMainWindow(window: BrowserWindow) {
    this.mainWindow = window;
  }

  create(config: PopOutConfig): { id: string; success: boolean; error?: string } {
    try {
      const id = `popout-${config.deviceId}-${config.tab}-${Date.now()}`;

      // Get display bounds for default positioning
      const display = screen.getPrimaryDisplay();
      const { width: screenWidth, height: screenHeight } = display.workAreaSize;

      // Default size
      const width = config.width || 1200;
      const height = config.height || 800;

      // Default position (offset from main window if exists)
      let x = config.x;
      let y = config.y;

      if (x === undefined || y === undefined) {
        if (this.mainWindow && !this.mainWindow.isDestroyed()) {
          const [mainX, mainY] = this.mainWindow.getPosition();
          x = x ?? mainX + 50;
          y = y ?? mainY + 50;
        } else {
          x = x ?? Math.floor((screenWidth - width) / 2);
          y = y ?? Math.floor((screenHeight - height) / 2);
        }
      }

      // Ensure window is within screen bounds
      x = Math.max(0, Math.min(x, screenWidth - width));
      y = Math.max(0, Math.min(y, screenHeight - height));

      // Create the pop-out window
      const window = new BrowserWindow({
        width,
        height,
        x,
        y,
        minWidth: 600,
        minHeight: 400,
        webPreferences: {
          nodeIntegration: false,
          contextIsolation: true,
          preload: path.join(__dirname, 'preload.js'),
        },
        title: `Performance - ${config.deviceId.slice(0, 8)}`,
        frame: true,
        autoHideMenuBar: true,
        show: false, // Show after load
      });

      // Load the pop-out route
      const indexPath = path.join(__dirname, '../renderer/index.html');
      window.loadFile(indexPath, {
        hash: `popout/performance/${config.deviceId}`,
      });

      // Show when ready
      window.once('ready-to-show', () => {
        window.show();
        window.focus();
      });

      // Handle window close
      window.on('closed', () => {
        this.windows.delete(id);
        console.log(`[PopOut] Window closed: ${id}`);
      });

      // Store the window reference
      this.windows.set(id, {
        id,
        window,
        deviceId: config.deviceId,
        tab: config.tab,
        createdAt: Date.now(),
      });

      console.log(`[PopOut] Created window: ${id}`);

      return { id, success: true };
    } catch (error) {
      console.error('[PopOut] Failed to create window:', error);
      return {
        id: '',
        success: false,
        error: error instanceof Error ? error.message : 'Unknown error',
      };
    }
  }

  close(id: string): boolean {
    const popOut = this.windows.get(id);
    if (popOut && !popOut.window.isDestroyed()) {
      popOut.window.close();
      this.windows.delete(id);
      return true;
    }
    return false;
  }

  reattach(id: string): boolean {
    const popOut = this.windows.get(id);
    if (!popOut) return false;

    // Notify main window to switch to the device's performance tab
    if (this.mainWindow && !this.mainWindow.isDestroyed()) {
      this.mainWindow.webContents.send('popOut:reattachRequest', {
        deviceId: popOut.deviceId,
        tab: popOut.tab,
      });
      this.mainWindow.focus();
    }

    // Close the pop-out window
    if (!popOut.window.isDestroyed()) {
      popOut.window.close();
    }
    this.windows.delete(id);

    return true;
  }

  list(): Array<{ id: string; deviceId: string; tab: string; createdAt: number }> {
    return Array.from(this.windows.values()).map(w => ({
      id: w.id,
      deviceId: w.deviceId,
      tab: w.tab,
      createdAt: w.createdAt,
    }));
  }

  getByDeviceId(deviceId: string): PopOutWindow | undefined {
    return Array.from(this.windows.values()).find(w => w.deviceId === deviceId);
  }

  focusWindow(id: string): boolean {
    const popOut = this.windows.get(id);
    if (popOut && !popOut.window.isDestroyed()) {
      if (popOut.window.isMinimized()) {
        popOut.window.restore();
      }
      popOut.window.focus();
      return true;
    }
    return false;
  }

  closeAll(): void {
    for (const [id, popOut] of this.windows) {
      if (!popOut.window.isDestroyed()) {
        popOut.window.close();
      }
    }
    this.windows.clear();
  }
}

// Singleton instance
export const popOutManager = new PopOutManager();

// Register IPC handlers
export function registerPopOutHandlers(): void {
  ipcMain.handle('popOut:create', (_, config: PopOutConfig) => {
    return popOutManager.create(config);
  });

  ipcMain.handle('popOut:close', (_, id: string) => {
    return popOutManager.close(id);
  });

  ipcMain.handle('popOut:reattach', (_, id: string) => {
    return popOutManager.reattach(id);
  });

  ipcMain.handle('popOut:list', () => {
    return popOutManager.list();
  });

  ipcMain.handle('popOut:focus', (_, id: string) => {
    return popOutManager.focusWindow(id);
  });

  console.log('[PopOut] IPC handlers registered');
}
