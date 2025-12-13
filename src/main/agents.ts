import { WebSocket } from 'ws';
import { v4 as uuidv4 } from 'uuid';
import { BrowserWindow } from 'electron';
import { Database } from './database';

interface AgentConnection {
  ws: WebSocket;
  agentId: string;
  deviceId?: string;
  lastSeen: Date;
}

interface TerminalSession {
  id: string;
  deviceId: string;
  agentId: string;
}

interface RemoteSession {
  id: string;
  deviceId: string;
  agentId: string;
  active: boolean;
}

interface PendingRequest {
  resolve: (value: any) => void;
  reject: (reason: any) => void;
  timeout: NodeJS.Timeout;
}

export class AgentManager {
  private connections: Map<string, AgentConnection> = new Map();
  private terminalSessions: Map<string, TerminalSession> = new Map();
  private dashboardSessions: Map<string, WebSocket> = new Map();
  private remoteSessions: Map<string, RemoteSession> = new Map();
  private pendingRequests: Map<string, PendingRequest> = new Map();
  private database: Database;
  private alertCooldowns: Map<string, Date> = new Map();

  constructor(database: Database) {
    this.database = database;
    this.startHeartbeatChecker();
    this.startAlertChecker();
  }

  async registerConnection(agentId: string, ws: WebSocket): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    this.connections.set(agentId, {
      ws,
      agentId,
      deviceId: device?.id,
      lastSeen: new Date(),
    });
    console.log(`Agent registered: ${agentId}`);

    // Notify renderer of device online
    this.notifyRenderer('devices:online', { agentId, deviceId: device?.id });
  }

  unregisterConnection(agentId: string): void {
    const conn = this.connections.get(agentId);
    this.connections.delete(agentId);
    console.log(`Agent unregistered: ${agentId}`);

    // Close any active sessions
    for (const [sessionId, session] of this.terminalSessions) {
      if (session.agentId === agentId) {
        this.terminalSessions.delete(sessionId);
      }
    }

    for (const [sessionId, session] of this.remoteSessions) {
      if (session.agentId === agentId) {
        this.remoteSessions.delete(sessionId);
      }
    }

    // Notify renderer of device offline
    this.notifyRenderer('devices:offline', { agentId, deviceId: conn?.deviceId });
  }

  getConnectedAgentCount(): number {
    return this.connections.size;
  }

  isAgentConnected(agentId: string): boolean {
    return this.connections.has(agentId);
  }


  async pingAgent(deviceId: string): Promise<{ online: boolean; status: string; message: string }> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      return { online: false, status: 'error', message: 'Device not found' };
    }

    const isOnline = this.isAgentConnected(device.agentId);

    if (!isOnline) {
      await this.database.updateDeviceStatus(deviceId, 'offline');
      this.notifyRenderer('devices:offline', { agentId: device.agentId, deviceId });
      return {
        online: false,
        status: 'offline',
        message: 'Agent is not connected. The agent service may need to be restarted.',
      };
    }

    const sent = this.sendToAgent(device.agentId, { type: 'ping', requestId: uuidv4() });
    if (!sent) {
      return { online: false, status: 'error', message: 'Failed to send ping to agent' };
    }

    await this.database.updateDeviceStatus(deviceId, 'online');
    this.notifyRenderer('devices:online', { agentId: device.agentId, deviceId });

    return {
      online: true,
      status: 'online',
      message: 'Agent is connected and responsive',
    };
  }

  sendToAgent(agentId: string, message: any): boolean {
    const conn = this.connections.get(agentId);
    if (conn && conn.ws.readyState === WebSocket.OPEN) {
      conn.ws.send(JSON.stringify(message));
      return true;
    }
    return false;
  }

  private async sendRequest(agentId: string, message: any, timeoutMs: number = 30000): Promise<any> {
    const requestId = uuidv4();
    message.requestId = requestId;

    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.pendingRequests.delete(requestId);
        reject(new Error('Request timeout'));
      }, timeoutMs);

      this.pendingRequests.set(requestId, { resolve, reject, timeout });

      if (!this.sendToAgent(agentId, message)) {
        clearTimeout(timeout);
        this.pendingRequests.delete(requestId);
        reject(new Error('Agent not connected'));
      }
    });
  }

  async handleMessage(agentId: string, message: any, ws: WebSocket): Promise<void> {
    const conn = this.connections.get(agentId);
    if (conn) {
      conn.lastSeen = new Date();
    }

    switch (message.type) {
      case 'heartbeat':
        this.handleHeartbeat(agentId, message);
        break;

      case 'metrics':
        this.handleMetrics(agentId, message);
        break;

      case 'response':
        this.handleResponse(message);
        break;

      case 'terminal_output':
        this.handleTerminalOutput(message);
        break;

      case 'remote_frame':
        this.handleRemoteFrame(message);
        break;

      case 'file_data':
        this.handleFileData(message);
        break;

      case 'event':
        this.handleEvent(agentId, message);
        break;

      default:
        console.log(`Unknown message type: ${message.type}`);
    }
  }

  private async handleHeartbeat(agentId: string, message: any): Promise<void> {
    await this.database.updateDeviceLastSeen(agentId);

    // Send heartbeat acknowledgment
    this.sendToAgent(agentId, {
      type: 'heartbeat_ack',
      timestamp: new Date().toISOString(),
    });
  }

  private async handleMetrics(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (device) {
      await this.database.insertMetrics(device.id, message.data);
      this.notifyRenderer('metrics:updated', {
        deviceId: device.id,
        metrics: message.data,
      });

      // Check alert rules
      await this.checkAlertRules(device, message.data);
    }
  }

  private handleResponse(message: any): void {
    const pending = this.pendingRequests.get(message.requestId);
    if (pending) {
      clearTimeout(pending.timeout);
      this.pendingRequests.delete(message.requestId);

      if (message.success) {
        pending.resolve(message.data);
      } else {
        pending.reject(new Error(message.error || 'Unknown error'));
      }
    }
  }

  registerDashboardSession(sessionId: string, ws: WebSocket): void {
    this.dashboardSessions.set(sessionId, ws);
    console.log('Registered dashboard session:', sessionId);
  }

  unregisterDashboardSession(sessionId: string): void {
    this.dashboardSessions.delete(sessionId);
    console.log('Unregistered dashboard session:', sessionId);
  }

  private handleTerminalOutput(message: any): void {
    const dashboardWs = this.dashboardSessions.get(message.sessionId);
    if (dashboardWs && dashboardWs.readyState === 1) {
      dashboardWs.send(JSON.stringify({
        type: 'terminal_output',
        payload: {
          sessionId: message.sessionId,
          data: message.data,
        }
      }));
    }
    // Also notify renderer via IPC for backward compatibility
    this.notifyRenderer('terminal:data', {
      sessionId: message.sessionId,
      data: message.data,
    });
  }

  private handleRemoteFrame(message: any): void {
    this.notifyRenderer('remote:frame', {
      sessionId: message.sessionId,
      data: message.data,
      width: message.width,
      height: message.height,
    });
  }

  private handleFileData(message: any): void {
    this.notifyRenderer('files:progress', {
      deviceId: message.deviceId,
      filename: message.filename,
      bytesTransferred: message.bytesTransferred,
      totalBytes: message.totalBytes,
      percentage: Math.round((message.bytesTransferred / message.totalBytes) * 100),
    });
  }

  private async handleEvent(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (device) {
      // Handle various events from agents (service stopped, process crashed, etc.)
      console.log(`Event from ${agentId}:`, message.event);

      if (message.event.severity === 'critical' || message.event.severity === 'warning') {
        const alert = await this.database.createAlert({
          deviceId: device.id,
          severity: message.event.severity,
          title: message.event.title,
          message: message.event.message,
        });
        this.notifyRenderer('alerts:new', alert);
      }
    }
  }

  private async checkAlertRules(device: any, metrics: any): Promise<void> {
    const rules = await this.database.getAlertRules();

    for (const rule of rules) {
      if (!rule.enabled) continue;

      const metricValue = metrics[this.camelToSnake(rule.metric)] || metrics[rule.metric];
      if (metricValue === undefined) continue;

      let triggered = false;
      switch (rule.operator) {
        case 'gt': triggered = metricValue > rule.threshold; break;
        case 'lt': triggered = metricValue < rule.threshold; break;
        case 'eq': triggered = metricValue === rule.threshold; break;
        case 'gte': triggered = metricValue >= rule.threshold; break;
        case 'lte': triggered = metricValue <= rule.threshold; break;
      }

      if (triggered) {
        const cooldownKey = `${device.id}:${rule.id}`;
        const lastAlert = this.alertCooldowns.get(cooldownKey);
        const now = new Date();

        if (!lastAlert || (now.getTime() - lastAlert.getTime()) > rule.cooldownMinutes * 60 * 1000) {
          const alert = await this.database.createAlert({
            deviceId: device.id,
            ruleId: rule.id,
            severity: rule.severity,
            title: rule.name,
            message: `${rule.metric} is ${metricValue.toFixed(1)}% (threshold: ${rule.threshold}%)`,
          });
          this.alertCooldowns.set(cooldownKey, now);
          this.notifyRenderer('alerts:new', alert);
        }
      }
    }
  }

  private camelToSnake(str: string): string {
    return str.replace(/[A-Z]/g, letter => `_${letter.toLowerCase()}`);
  }

  private notifyRenderer(channel: string, data: any): void {
    const windows = BrowserWindow.getAllWindows();
    for (const win of windows) {
      win.webContents.send(channel, data);
    }
  }

  private startHeartbeatChecker(): void {
    setInterval(async () => {
      const now = new Date();
      const timeout = 60000; // 60 seconds

      for (const [agentId, conn] of this.connections) {
        if (now.getTime() - conn.lastSeen.getTime() > timeout) {
          console.log(`Agent ${agentId} timed out`);
          conn.ws.close();
          this.unregisterConnection(agentId);
          await this.database.updateDeviceStatus(agentId, 'offline');
        }
      }
    }, 30000); // Check every 30 seconds
  }

  private startAlertChecker(): void {
    // Clean up old metric data periodically
    setInterval(async () => {
      const settings = await this.database.getSettings();
      await this.database.cleanOldMetrics(settings.metricsRetentionDays || 30);
    }, 3600000); // Every hour
  }

  // Command execution
  async executeCommand(deviceId: string, command: string, type: string): Promise<any> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    const cmd = await this.database.createCommand(deviceId, command, type);

    try {
      const result = await this.sendRequest(device.agentId, {
        type: 'execute_command',
        commandId: cmd.id,
        command,
        commandType: type,
      });

      await this.database.updateCommandStatus(cmd.id, 'completed', result.output);
      return { success: true, output: result.output };
    } catch (error: any) {
      await this.database.updateCommandStatus(cmd.id, 'failed', undefined, error.message);
      throw error;
    }
  }

  // Terminal sessions
  async startTerminalSession(deviceId: string): Promise<{ sessionId: string }> {
    console.log('=== START TERMINAL SESSION ===');
    console.log('Requested deviceId:', deviceId);
    console.log('Connected agents:', Array.from(this.connections.keys()));

    const device = await this.database.getDevice(deviceId);
    console.log('Device from DB:', device ? { id: device.id, agentId: device.agentId, hostname: device.hostname } : null);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    const sessionId = uuidv4();

    await this.sendRequest(device.agentId, {
      type: 'start_terminal',
      sessionId,
    });

    this.terminalSessions.set(sessionId, {
      id: sessionId,
      deviceId,
      agentId: device.agentId,
    });

    return { sessionId };
  }

  async sendTerminalData(sessionId: string, data: string): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (!session) {
      throw new Error('Terminal session not found');
    }

    this.sendToAgent(session.agentId, {
      type: 'terminal_input',
      sessionId,
      data,
    });
  }

  async resizeTerminal(sessionId: string, cols: number, rows: number): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (!session) {
      throw new Error('Terminal session not found');
    }

    this.sendToAgent(session.agentId, {
      type: 'terminal_resize',
      sessionId,
      cols,
      rows,
    });
  }

  async closeTerminalSession(sessionId: string): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (session) {
      this.sendToAgent(session.agentId, {
        type: 'close_terminal',
        sessionId,
      });
      this.terminalSessions.delete(sessionId);
    }
  }

  // File operations
  async listFiles(deviceId: string, remotePath: string): Promise<any[]> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    const result = await this.sendRequest(device.agentId, {
      type: 'list_files',
      path: remotePath,
    });

    return result.files;
  }

  async downloadFile(deviceId: string, remotePath: string, localPath: string): Promise<void> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    await this.sendRequest(device.agentId, {
      type: 'download_file',
      remotePath,
      localPath,
    }, 300000); // 5 minute timeout for file transfers
  }

  async uploadFile(deviceId: string, localPath: string, remotePath: string): Promise<void> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    await this.sendRequest(device.agentId, {
      type: 'upload_file',
      localPath,
      remotePath,
    }, 300000); // 5 minute timeout for file transfers
  }

  // Remote desktop
  async startRemoteSession(deviceId: string): Promise<{ sessionId: string }> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    const sessionId = uuidv4();

    await this.sendRequest(device.agentId, {
      type: 'start_remote',
      sessionId,
    });

    this.remoteSessions.set(sessionId, {
      id: sessionId,
      deviceId,
      agentId: device.agentId,
      active: true,
    });

    return { sessionId };
  }

  async stopRemoteSession(sessionId: string): Promise<void> {
    const session = this.remoteSessions.get(sessionId);
    if (session) {
      this.sendToAgent(session.agentId, {
        type: 'stop_remote',
        sessionId,
      });
      this.remoteSessions.delete(sessionId);
    }
  }

  async sendRemoteInput(sessionId: string, input: any): Promise<void> {
    const session = this.remoteSessions.get(sessionId);
    if (!session) {
      throw new Error('Remote session not found');
    }

    this.sendToAgent(session.agentId, {
      type: 'remote_input',
      sessionId,
      input,
    });
  }

  // Script execution
  async executeScript(scriptId: string, deviceIds: string[]): Promise<void> {
    const script = await this.database.getScript(scriptId);
    if (!script) {
      throw new Error('Script not found');
    }

    for (const deviceId of deviceIds) {
      const device = await this.database.getDevice(deviceId);
      if (!device || !this.isAgentConnected(device.agentId)) {
        continue;
      }

      // Check OS compatibility
      if (script.osTypes.length > 0 && !script.osTypes.includes(device.osType)) {
        continue;
      }

      const cmd = await this.database.createCommand(deviceId, script.content, script.language);

      this.sendRequest(device.agentId, {
        type: 'execute_script',
        commandId: cmd.id,
        script: script.content,
        language: script.language,
      }).then(async result => {
        await this.database.updateCommandStatus(cmd.id, 'completed', result.output);
      }).catch(async error => {
        await this.database.updateCommandStatus(cmd.id, 'failed', undefined, error.message);
      });
    }
  }

  // Collect diagnostics from device
  async collectDiagnostics(deviceId: string, hoursBack: number = 8): Promise<any> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    console.log(`Requesting diagnostics from device ${deviceId} for past ${hoursBack} hours`);

    const result = await this.sendRequest(device.agentId, {
      type: 'collect_diagnostics',
      data: { hoursBack },
    }, 120000); // 2 minute timeout for diagnostics collection

    return result;
  }


  // Trigger an update on a specific agent (push update)
  async triggerAgentUpdate(deviceId: string): Promise<boolean> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    console.log(`Triggering update check for device ${deviceId}`);

    // Send update check command to agent
    const result = await this.sendRequest(device.agentId, {
      type: 'check_update',
      force: true, // Force immediate check
    }, 30000);

    return result?.success || false;
  }

  // Trigger updates on all connected agents
  async triggerFleetUpdate(): Promise<{ success: number; failed: number }> {
    let success = 0;
    let failed = 0;

    for (const [agentId, conn] of this.connections) {
      try {
        this.sendToAgent(agentId, {
          type: 'check_update',
          force: true,
        });
        success++;
      } catch (error) {
        console.error(`Failed to trigger update for agent ${agentId}:`, error);
        failed++;
      }
    }

    return { success, failed };
  }

}