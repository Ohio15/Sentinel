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

interface WebRTCSession {
  deviceId: string;
  agentId: string;
  quality: string;
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
  private webrtcSessions: Map<string, WebRTCSession> = new Map();
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
    console.log(`Agent registered: ${agentId}${device ? ` (device: ${device.id})` : ' (no device yet)'}`);

    // Only notify renderer if device already exists
    // New devices will trigger devices:online when first metrics create the device record
    if (device?.id) {
      this.notifyRenderer('devices:online', { agentId, deviceId: device.id });

      // Deliver any queued commands for this device
      this.deliverQueuedCommands(device.id, agentId).catch(err => {
        console.error(`Failed to deliver queued commands for ${device.id}:`, err);
      });
    } else {
      console.log(`[AgentManager] Agent ${agentId} connected but no device record yet - will be created on first metrics`);
    }
  }

  // Deliver queued commands to a reconnected agent
  private async deliverQueuedCommands(deviceId: string, agentId: string): Promise<void> {
    const pendingCommands = await this.database.getPendingCommandsForDevice(deviceId);
    if (pendingCommands.length === 0) return;

    console.log(`[Queue] Delivering ${pendingCommands.length} queued commands to device ${deviceId}`);

    for (const cmd of pendingCommands) {
      try {
        // Mark as delivered
        await this.database.markCommandDelivered(cmd.id);

        // Send to agent
        const sent = this.sendToAgent(agentId, {
          type: cmd.commandType,
          requestId: cmd.id,
          data: cmd.payload,
        });

        if (!sent) {
          console.error(`[Queue] Failed to send queued command ${cmd.id} to agent`);
        } else {
          console.log(`[Queue] Delivered queued command ${cmd.id} (${cmd.commandType})`);
        }
      } catch (error) {
        console.error(`[Queue] Error delivering command ${cmd.id}:`, error);
      }
    }
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

  async uninstallAgent(deviceId: string): Promise<{ success: boolean; message: string }> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      return { success: false, message: 'Device not found' };
    }

    const isOnline = this.isAgentConnected(device.agentId);
    if (!isOnline) {
      return { success: false, message: 'Agent is not connected. Cannot send uninstall command.' };
    }

    // Set device status to uninstalling
    await this.database.setDeviceUninstalling(deviceId);

    // Send uninstall command to agent
    const sent = this.sendToAgent(device.agentId, {
      type: 'uninstall_agent',
      requestId: uuidv4(),
    });

    if (!sent) {
      return { success: false, message: 'Failed to send uninstall command to agent' };
    }

    this.notifyRenderer('devices:updated', { deviceId, status: 'uninstalling' });

    return {
      success: true,
      message: 'Uninstall command sent to agent',
    };
  }

  async disableDevice(deviceId: string): Promise<{ success: boolean; message: string }> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      return { success: false, message: 'Device not found' };
    }

    // Disable in database
    await this.database.disableDevice(deviceId);

    // If agent is connected, disconnect it
    if (this.isAgentConnected(device.agentId)) {
      const conn = this.connections.get(device.agentId);
      if (conn) {
        this.sendToAgent(device.agentId, {
          type: 'disconnect',
          requestId: uuidv4(),
          reason: 'Device disabled by administrator',
        });
        conn.ws.close();
        this.connections.delete(device.agentId);
      }
    }

    this.notifyRenderer('devices:updated', { deviceId, status: 'disabled' });
    this.notifyRenderer('devices:offline', { agentId: device.agentId, deviceId });

    return { success: true, message: 'Device disabled' };
  }

  async enableDevice(deviceId: string): Promise<{ success: boolean; message: string }> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      return { success: false, message: 'Device not found' };
    }

    // Enable in database
    await this.database.enableDevice(deviceId);

    this.notifyRenderer('devices:updated', { deviceId, status: 'offline' });

    return { success: true, message: 'Device enabled' };
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

    // Extract type and wrap remaining fields in data object for agent compatibility
    const { type, ...rest } = message;
    const wrappedMessage = {
      type,
      requestId,
      data: rest,
    };

    console.log(`[sendRequest] Sending to agent ${agentId}:`, JSON.stringify(wrappedMessage, null, 2));

    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.pendingRequests.delete(requestId);
        reject(new Error('Request timeout'));
      }, timeoutMs);

      this.pendingRequests.set(requestId, { resolve, reject, timeout });

      if (!this.sendToAgent(agentId, wrappedMessage)) {
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

      case 'webrtc_signal':
        this.handleWebRTCSignal(agentId, message);
        break;

      case 'tamper_alert':
        this.handleTamperAlert(agentId, message);
        break;

      case 'cert_update_ack':
        this.handleCertUpdateAck(agentId, message);
        break;

      case 'update_status':
        this.handleUpdateStatus(agentId, message);
        break;

      case 'sync_request':
        this.handleSyncRequest(agentId, message);
        break;

      case 'bulk_metrics':
        this.handleBulkMetrics(agentId, message);
        break;

      case 'command_result':
        this.handleCommandResult(agentId, message);
        break;

      case 'health_report':
        this.handleHealthReport(agentId, message);
        break;
      case 'enroll':
        await this.handleEnrollment(agentId, message, ws);
        break;

      default:
        console.log(`Unknown message type: ${message.type}`);
    }
  }


  // Handle enrollment/re-enrollment from agent via WebSocket
  private async handleEnrollment(agentId: string, message: any, ws: WebSocket): Promise<void> {
    const data = message.data || message.payload || message;

    console.log(`[Enroll] Agent ${agentId} sending enrollment data via WebSocket`);

    // Build device info from enrollment data
    const deviceInfo = {
      agentId: agentId,
      hostname: data.hostname || data.systemInfo?.hostname || 'Unknown',
      displayName: data.displayName || data.hostname || data.systemInfo?.hostname || 'Unknown',
      osType: data.osType || data.systemInfo?.os || 'Unknown',
      osVersion: data.osVersion || data.systemInfo?.osVersion || '',
      osBuild: data.osBuild || data.systemInfo?.osBuild || '',
      architecture: data.architecture || data.systemInfo?.arch || '',
      agentVersion: data.agentVersion || data.version || '',
      ipAddress: data.ipAddress || data.systemInfo?.ipAddress || '',
      macAddress: data.macAddress || data.systemInfo?.macAddress || '',
      platform: data.platform || data.systemInfo?.platform || '',
      platformFamily: data.platformFamily || data.systemInfo?.platformFamily || '',
      cpuModel: data.cpuModel || data.systemInfo?.cpu?.model || '',
      cpuCores: data.cpuCores || data.systemInfo?.cpu?.cores || null,
      cpuThreads: data.cpuThreads || data.systemInfo?.cpu?.threads || null,
      cpuSpeed: data.cpuSpeed || data.systemInfo?.cpu?.speed || null,
      totalMemory: data.totalMemory || data.systemInfo?.memory?.total || null,
      bootTime: data.bootTime || data.systemInfo?.bootTime || null,
      gpu: data.gpu || data.systemInfo?.gpu || null,
      storage: data.storage || data.systemInfo?.storage || null,
      serialNumber: data.serialNumber || data.systemInfo?.serialNumber || '',
      manufacturer: data.manufacturer || data.systemInfo?.manufacturer || '',
      model: data.model || data.systemInfo?.model || '',
      domain: data.domain || data.systemInfo?.domain || '',
      metadata: data.metadata || {},
      tags: data.tags || [],
    };

    try {
      const device = await this.database.createOrUpdateDevice(deviceInfo);
      console.log(`[Enroll] Device ${device.id} created/updated for agent ${agentId}`);

      // Update the connection with the new device ID
      const conn = this.connections.get(agentId);
      if (conn) {
        conn.deviceId = device.id;
      }

      // Send enrollment response
      ws.send(JSON.stringify({
        type: 'enroll_response',
        success: true,
        payload: {
          deviceId: device.id,
          config: {
            heartbeatInterval: 30,
            metricsInterval: 60,
          },
        },
        timestamp: new Date().toISOString(),
      }));

      // Notify renderer of new device
      this.notifyRenderer('device:enrolled', {
        deviceId: device.id,
        agentId: agentId,
        hostname: device.hostname,
      });
      this.notifyRenderer('devices:online', { agentId, deviceId: device.id, isNew: true });
      this.notifyRenderer('devices:updated', { deviceId: device.id });
    } catch (error) {
      console.error(`[Enroll] Failed to create device for agent ${agentId}:`, error);
      ws.send(JSON.stringify({
        type: 'enroll_response',
        success: false,
        error: 'Failed to create device record',
        timestamp: new Date().toISOString(),
      }));
    }
  }

  // Handle sync request from agent after reconnection
  private async handleSyncRequest(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (!device) return;

    const data = message.data || message;
    console.log(`[Sync] Agent ${agentId} requesting sync after ${data.offlineDuration || 'unknown'} offline`);
    console.log(`[Sync] Agent has ${data.cachedMetricsCount || 0} cached metrics, ${data.cachedEventsCount || 0} events`);

    // Get pending commands for this device
    const pendingCommands = await this.database.getPendingCommandsForDevice(device.id);

    // Send sync response with pending commands
    this.sendToAgent(agentId, {
      type: 'sync_response',
      data: {
        pendingCommands: pendingCommands.map(cmd => ({
          id: cmd.id,
          type: cmd.commandType,
          payload: cmd.payload,
          priority: cmd.priority,
        })),
        serverTime: new Date().toISOString(),
        acceptBulkMetrics: true,
        maxBatchSize: 100,
      },
    });
  }

  // Handle bulk metrics upload from agent (cached during offline period)
  private async handleBulkMetrics(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (!device) return;

    const data = message.data || message;
    const metrics = data.metrics || [];

    console.log(`[Sync] Received ${metrics.length} cached metrics from agent ${agentId}`);

    let processed = 0;
    for (const metric of metrics) {
      try {
        // Store the backlogged metric with its original timestamp
        await this.database.storeMetricsBacklog(device.id, new Date(metric.timestamp), metric.data);
        processed++;
      } catch (error) {
        console.error(`[Sync] Failed to store cached metric:`, error);
      }
    }

    // Acknowledge receipt
    this.sendToAgent(agentId, {
      type: 'bulk_metrics_ack',
      data: {
        received: processed,
        batchId: data.batchId,
      },
    });

    console.log(`[Sync] Processed ${processed}/${metrics.length} cached metrics`);
  }

  // Handle command result from agent (for queued commands)
  private async handleCommandResult(agentId: string, message: any): Promise<void> {
    const data = message.data || message;
    const commandId = data.commandId || message.requestId;

    if (!commandId) {
      console.error('[Queue] Command result missing commandId');
      return;
    }

    if (data.success) {
      await this.database.markCommandCompleted(commandId, data.result || data.output);
      console.log(`[Queue] Command ${commandId} completed successfully`);
    } else {
      await this.database.markCommandFailed(commandId, data.error || 'Unknown error');
      console.log(`[Queue] Command ${commandId} failed: ${data.error}`);
    }

    // Notify renderer of command completion
    const device = await this.database.getDeviceByAgentId(agentId);
    if (device) {
      this.notifyRenderer('command:completed', {
        deviceId: device.id,
        commandId,
        success: data.success,
        result: data.result || data.output,
        error: data.error,
      });
    }
  }

  // Handle health report from agent
  private async handleHealthReport(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (!device) return;

    const data = message.data || message;
    console.log(`[Health] Received health report from ${device.hostname || agentId}:`, {
      score: data.score,
      status: data.status,
    });

    // Store the health report
    await this.database.upsertAgentHealth(device.id, {
      healthScore: data.score || 100,
      status: data.status || 'unknown',
      factors: data.factors || {},
      components: data.components || {},
      updatedAt: new Date(),
    });

    // Notify renderer
    this.notifyRenderer('health:updated', {
      deviceId: device.id,
      ...data,
    });
  }

  private async handleHeartbeat(agentId: string, message: any): Promise<void> {
    // Check if device exists, create if not (orphaned agent recovery)
    let device = await this.database.getDeviceByAgentId(agentId);
    if (!device) {
      console.log(`[Heartbeat] Device not found for agent ${agentId}, creating from heartbeat data`);
      const hostname = message.hostname || `Agent-${agentId.substring(0, 8)}`;
      device = await this.database.createOrUpdateDevice({
        agentId,
        hostname,
        osType: message.osType || 'Unknown',
        osVersion: message.osVersion || '',
        platform: message.platform || 'unknown',
        architecture: message.architecture || '',
        agentVersion: message.agentVersion || '',
      });

      // Update the connection with the new device ID
      const conn = this.connections.get(agentId);
      if (conn) {
        conn.deviceId = device.id;
      }

      // Notify renderer that a new device was created and is online
      console.log(`[Heartbeat] New device created: ${device.id} (${device.hostname}), notifying renderer`);
      this.notifyRenderer('devices:online', { agentId, deviceId: device.id, isNew: true });
      this.notifyRenderer('devices:updated', { deviceId: device.id });
    } else {
      await this.database.updateDeviceLastSeen(agentId);
    }

    // Update agent version if provided in heartbeat
    if (message.agentVersion && device) {
      await this.database.updateDeviceAgentVersion(agentId, message.agentVersion);
    }

    // Send heartbeat acknowledgment
    this.sendToAgent(agentId, {
      type: 'heartbeat_ack',
      timestamp: new Date().toISOString(),
    });
  }

  private async handleMetrics(agentId: string, message: any): Promise<void> {
    let device = await this.database.getDeviceByAgentId(agentId);
    let isNewDevice = false;

    // If device doesn't exist, create a minimal record from the metrics data
    if (!device) {
      console.log(`Device not found for agent ${agentId}, creating from metrics data`);
      isNewDevice = true;
      const hostname = message.data?.hostname || `Agent-${agentId.substring(0, 8)}`;
      device = await this.database.createOrUpdateDevice({
        agentId,
        hostname,
        osType: message.data?.osType || 'Unknown',
        osVersion: message.data?.osVersion || '',
        platform: message.data?.platform || 'unknown',
        architecture: message.data?.architecture || '',
      });

      // Update the connection with the new device ID
      const conn = this.connections.get(agentId);
      if (conn) {
        conn.deviceId = device.id;
      }

      // Notify renderer that a new device was created and is online
      console.log(`[AgentManager] New device created: ${device.id} (${device.hostname}), notifying renderer`);
      this.notifyRenderer('devices:online', { agentId, deviceId: device.id, isNew: true });
      this.notifyRenderer('devices:updated', { deviceId: device.id });
    }

    if (device) {
      await this.database.insertMetrics(device.id, message.data);
      this.notifyRenderer('metrics:updated', {
        deviceId: device.id,
        metrics: message.data,
        source: 'websocket',
      });
      console.log(`[WebSocket] Metrics received for device ${device.id}, CPU: ${message.data?.cpuPercent?.toFixed(1)}%`);

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

  private async handleTamperAlert(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (device) {
      console.log(`[SECURITY] Tamper alert from ${agentId}:`, message.report || message.data);

      // Create a critical alert for tamper detection
      const alert = await this.database.createAlert({
        deviceId: device.id,
        severity: 'critical',
        title: 'Tamper Detection Alert',
        message: message.report || message.data || 'Tampering attempt detected on agent',
      });
      this.notifyRenderer('alerts:new', alert);
      this.notifyRenderer('security:tamper', {
        deviceId: device.id,
        agentId,
        report: message.report || message.data,
        timestamp: new Date().toISOString(),
      });
    }
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

  // Command execution (with offline queue support)
  async executeCommand(deviceId: string, command: string, type: string, options?: {
    queueIfOffline?: boolean;
    priority?: number;
    expiresInMinutes?: number;
  }): Promise<any> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    const isConnected = this.isAgentConnected(device.agentId);

    // If agent is offline and queueIfOffline is enabled, queue the command
    if (!isConnected && options?.queueIfOffline) {
      const commandId = uuidv4();
      const expiresAt = options.expiresInMinutes
        ? new Date(Date.now() + options.expiresInMinutes * 60 * 1000)
        : undefined;

      await this.database.queueCommand({
        id: commandId,
        deviceId,
        commandType: 'execute_command',
        payload: { command, commandType: type },
        priority: options.priority,
        expiresAt,
      });

      console.log(`[Queue] Command queued for offline device ${deviceId}: ${command}`);
      return {
        success: true,
        queued: true,
        commandId,
        message: 'Command queued for delivery when agent reconnects',
      };
    }

    if (!isConnected) {
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

  // Queue any command for an offline agent
  async queueCommandForDevice(deviceId: string, commandType: string, payload: any, options?: {
    priority?: number;
    expiresInMinutes?: number;
  }): Promise<string> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    const commandId = uuidv4();
    const expiresAt = options?.expiresInMinutes
      ? new Date(Date.now() + options.expiresInMinutes * 60 * 1000)
      : undefined;

    await this.database.queueCommand({
      id: commandId,
      deviceId,
      commandType,
      payload,
      priority: options?.priority,
      expiresAt,
    });

    console.log(`[Queue] Command ${commandType} queued for device ${deviceId}`);
    return commandId;
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
      data: { sessionId, data },
    });
  }

  async resizeTerminal(sessionId: string, cols: number, rows: number): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (!session) {
      throw new Error('Terminal session not found');
    }

    this.sendToAgent(session.agentId, {
      type: 'terminal_resize',
      data: { sessionId, cols, rows },
    });
  }

  async closeTerminalSession(sessionId: string): Promise<void> {
    const session = this.terminalSessions.get(sessionId);
    if (session) {
      this.sendToAgent(session.agentId, {
        type: 'close_terminal',
        data: { sessionId },
      });
      this.terminalSessions.delete(sessionId);
    }
  }

  // File operations
  async listDrives(deviceId: string): Promise<any[]> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    const result = await this.sendRequest(device.agentId, {
      type: 'list_drives',
    });

    return result.drives;
  }

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

  async scanDirectory(deviceId: string, path: string, maxDepth: number = 10): Promise<any> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    const result = await this.sendRequest(device.agentId, {
      type: 'scan_directory',
      path,
      maxDepth,
    }, 600000); // 10 minute timeout for large directory scans

    return result.result;
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
        data: { sessionId },
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
      data: { sessionId, input },
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
      hoursBack,  // Note: sendRequest wraps this in data: { hoursBack }
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

  // WebRTC Remote Desktop
  async startWebRTCSession(deviceId: string, offer: any): Promise<void> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    // Generate a session ID
    const sessionId = uuidv4();
    console.log(`[WebRTC] Starting session ${sessionId} for device ${deviceId}`);
    console.log(`[WebRTC] Agent ID: ${device.agentId}, Quality: ${offer.quality || 'medium'}`);
    console.log(`[WebRTC] Offer SDP length: ${offer.sdp?.length || 0}`);

    // Store the session
    this.webrtcSessions.set(deviceId, {
      deviceId,
      agentId: device.agentId,
      quality: offer.quality || 'medium',
      active: true,
    });

    // Send WebRTC start command to agent with offer SDP
    console.log(`[WebRTC] Sending webrtc_start to agent...`);
    const result = await this.sendRequest(device.agentId, {
      type: 'webrtc_start',
      sessionId,
      offerSdp: offer.sdp,
      quality: offer.quality || 'medium',
    });
    console.log(`[WebRTC] Got result from agent:`, result);

    // Forward the answer SDP back to the renderer
    if (result && result.answerSdp) {
      console.log(`[WebRTC] Forwarding answer SDP to renderer (length: ${result.answerSdp.length})`);
      this.notifyRenderer('webrtc:signal', {
        deviceId,
        type: 'answer',
        sdp: result.answerSdp,
      });
    } else {
      console.log(`[WebRTC] WARNING: No answerSdp in result`);
    }

    console.log(`[WebRTC] Session ${sessionId} started successfully`);
  }

  async stopWebRTCSession(deviceId: string): Promise<void> {
    const session = this.webrtcSessions.get(deviceId);
    if (session) {
      this.sendToAgent(session.agentId, {
        type: 'webrtc_stop',
        data: { deviceId },
      });
      this.webrtcSessions.delete(deviceId);
      console.log(`Stopped WebRTC session for device ${deviceId}`);
    }
  }

  async sendWebRTCSignal(deviceId: string, signal: any): Promise<void> {
    const session = this.webrtcSessions.get(deviceId);
    if (!session) {
      throw new Error('WebRTC session not found');
    }

    this.sendToAgent(session.agentId, {
      type: 'webrtc_signal',
      data: signal,
    });
  }

  async setWebRTCQuality(deviceId: string, quality: string): Promise<void> {
    const session = this.webrtcSessions.get(deviceId);
    if (!session) {
      throw new Error('WebRTC session not found');
    }

    session.quality = quality;
    this.sendToAgent(session.agentId, {
      type: 'webrtc_quality',
      data: { quality },
    });
  }

  private handleWebRTCSignal(agentId: string, message: any): void {
    // Find the device for this agent and forward the signal to the renderer
    for (const [deviceId, session] of this.webrtcSessions) {
      if (session.agentId === agentId) {
        this.notifyRenderer('webrtc:signal', {
          deviceId,
          ...message.data,
        });
        break;
      }
    }
  }


  // Set metrics interval for real-time updates (for Performance tab high-frequency mode)
  async setMetricsInterval(deviceId: string, intervalMs: number): Promise<void> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      throw new Error('Device not found');
    }

    if (!this.isAgentConnected(device.agentId)) {
      throw new Error('Agent not connected');
    }

    console.log(`Setting metrics interval for device ${deviceId} to ${intervalMs}ms`);

    await this.sendRequest(device.agentId, {
      type: 'set_metrics_interval',
      intervalMs,
    });
  }


  // Broadcast CA certificate to all connected agents
  async broadcastCertificate(certContent: string, certHash: string): Promise<{ success: number; failed: number; total: number }> {
    let success = 0;
    let failed = 0;
    const total = this.connections.size;

    console.log(`[Certs] Broadcasting CA certificate to ${total} connected agents`);

    for (const [agentId] of this.connections) {
      try {
        this.sendToAgent(agentId, {
          type: 'update_certificate',
          data: {
            certType: 'ca',
            certContent,
            certHash,
          },
        });

        await this.database.setAgentCertStatus(agentId, certHash, true, false);
        success++;
        console.log(`[Certs] Sent certificate to agent ${agentId}`);
      } catch (error) {
        console.error(`[Certs] Failed to send certificate to agent ${agentId}:`, error);
        failed++;
      }
    }

    console.log(`[Certs] Broadcast complete: ${success} success, ${failed} failed out of ${total}`);
    this.notifyRenderer('certs:distributed', { success, failed, total });

    return { success, failed, total };
  }

  // Handle certificate update acknowledgment from agent
  private async handleCertUpdateAck(agentId: string, message: any): Promise<void> {
    const certHash = message.certHash || message.data?.certHash;
    if (certHash) {
      await this.database.setAgentCertStatus(agentId, certHash, true, true);
      console.log(`[Certs] Agent ${agentId} confirmed certificate update (hash: ${certHash.substring(0, 8)}...)`);
      this.notifyRenderer('certs:agentConfirmed', { agentId, certHash });
    }
  }

  // Handle update status from agent
  private async handleUpdateStatus(agentId: string, message: any): Promise<void> {
    const device = await this.database.getDeviceByAgentId(agentId);
    if (!device) {
      console.log(`[Updates] Received update status from unknown agent: ${agentId}`);
      return;
    }

    const data = message.data || message;
    console.log(`[Updates] Received update status from ${device.hostname || agentId}:`, {
      pendingCount: data.pendingCount,
      securityUpdateCount: data.securityUpdateCount,
      rebootRequired: data.rebootRequired,
    });

    // Store update status in database
    await this.database.upsertDeviceUpdateStatus(device.id, {
      pendingCount: data.pendingCount || 0,
      securityUpdateCount: data.securityUpdateCount || 0,
      rebootRequired: data.rebootRequired || false,
      lastChecked: data.lastChecked || new Date().toISOString(),
      lastUpdateInstalled: data.lastUpdateInstalled,
      pendingUpdates: data.pendingUpdates || [],
    });

    // Notify renderer of update status change
    this.notifyRenderer('updates:status', {
      deviceId: device.id,
      hostname: device.hostname,
      ...data,
    });

    // Check for pending updates alerts
    await this.checkUpdateAlerts(device, data);
  }

  // Check if update status should trigger alerts
  private async checkUpdateAlerts(device: any, updateStatus: any): Promise<void> {
    const rules = await this.database.getAlertRules();

    for (const rule of rules) {
      if (!rule.enabled) continue;

      // Handle pending_updates metric type
      if (rule.metric === 'pending_updates' || rule.metric === 'pendingUpdates') {
        const pendingCount = updateStatus.pendingCount || 0;
        let triggered = false;

        switch (rule.operator) {
          case 'gt': triggered = pendingCount > rule.threshold; break;
          case 'gte': triggered = pendingCount >= rule.threshold; break;
          case 'eq': triggered = pendingCount === rule.threshold; break;
        }

        if (triggered) {
          await this.createUpdateAlert(device, rule, 'pending_updates', pendingCount, updateStatus);
        }
      }

      // Handle security_updates metric type
      if (rule.metric === 'security_updates' || rule.metric === 'securityUpdates') {
        const securityCount = updateStatus.securityUpdateCount || 0;
        let triggered = false;

        switch (rule.operator) {
          case 'gt': triggered = securityCount > rule.threshold; break;
          case 'gte': triggered = securityCount >= rule.threshold; break;
          case 'eq': triggered = securityCount === rule.threshold; break;
        }

        if (triggered) {
          await this.createUpdateAlert(device, rule, 'security_updates', securityCount, updateStatus);
        }
      }

      // Handle reboot_required metric type
      if (rule.metric === 'reboot_required' || rule.metric === 'rebootRequired') {
        if (updateStatus.rebootRequired && rule.threshold === 1) {
          await this.createUpdateAlert(device, rule, 'reboot_required', 1, updateStatus);
        }
      }
    }
  }

  // Create alert for update-related issues
  private async createUpdateAlert(device: any, rule: any, metricType: string, value: number, updateStatus: any): Promise<void> {
    const cooldownKey = `${device.id}:${rule.id}`;
    const lastAlert = this.alertCooldowns.get(cooldownKey);
    const now = new Date();

    if (!lastAlert || (now.getTime() - lastAlert.getTime()) > rule.cooldownMinutes * 60 * 1000) {
      let message = '';
      switch (metricType) {
        case 'pending_updates':
          message = `${value} pending Windows updates (${updateStatus.securityUpdateCount || 0} security updates)`;
          break;
        case 'security_updates':
          message = `${value} critical security updates pending`;
          break;
        case 'reboot_required':
          message = 'System reboot required to complete updates';
          break;
      }

      const alert = await this.database.createAlert({
        deviceId: device.id,
        ruleId: rule.id,
        severity: rule.severity,
        title: rule.name,
        message,
      });

      this.alertCooldowns.set(cooldownKey, now);
      this.notifyRenderer('alerts:new', alert);
      console.log(`[Updates] Created alert for ${device.hostname}: ${message}`);
    }
  }
}
