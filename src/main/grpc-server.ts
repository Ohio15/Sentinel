import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import * as path from 'path';
import { BrowserWindow } from 'electron';
import { Database } from './database';
import { AgentManager } from './agents';

const PROTO_PATH = path.join(__dirname, '../../proto/dataplane.proto');

interface GrpcConnection {
  agentId: string;
  connectedAt: Date;
  lastMetricsAt: Date | null;
}

export class GrpcServer {
  private server: grpc.Server | null = null;
  private database: Database;
  private agentManager: AgentManager;
  private port: number;
  private connections: Map<string, GrpcConnection> = new Map();

  constructor(database: Database, agentManager: AgentManager, port: number = 8082) {
    this.database = database;
    this.agentManager = agentManager;
    this.port = port;
  }

  async start(): Promise<void> {
    const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
      keepCase: false,
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
    });

    const protoDescriptor = grpc.loadPackageDefinition(packageDefinition) as any;
    const dataplane = protoDescriptor.sentinel.dataplane;

    this.server = new grpc.Server();

    this.server.addService(dataplane.DataPlaneService.service, {
      StreamMetrics: this.handleStreamMetrics.bind(this),
      UploadInventory: this.handleUploadInventory.bind(this),
      StreamLogs: this.handleStreamLogs.bind(this),
      StreamFileContent: this.handleStreamFileContent.bind(this),
      UploadBulkData: this.handleUploadBulkData.bind(this),
    });

    return new Promise((resolve, reject) => {
      this.server!.bindAsync(
        `0.0.0.0:${this.port}`,
        grpc.ServerCredentials.createInsecure(),
        (error, port) => {
          if (error) {
            console.error('gRPC server failed to bind:', error);
            reject(error);
            return;
          }
          console.log(`gRPC DataPlane server listening on port ${port}`);
          resolve();
        }
      );
    });
  }

  async stop(): Promise<void> {
    if (this.server) {
      return new Promise((resolve) => {
        this.server!.tryShutdown(() => {
          console.log('gRPC server stopped');
          this.server = null;
          resolve();
        });
      });
    }
  }

  getPort(): number {
    return this.port;
  }

  getConnectionCount(): number {
    return this.connections.size;
  }

  isAgentConnectedGrpc(agentId: string): boolean {
    return this.connections.has(agentId);
  }

  // Client streaming RPC - receives a stream of metrics, returns single response
  private handleStreamMetrics(
    call: grpc.ServerReadableStream<any, any>,
    callback: grpc.sendUnaryData<any>
  ): void {
    let agentId: string | null = null;
    let metricsCount = 0;

    call.on('data', async (metrics: any) => {
      try {
        agentId = metrics.agentId;

        if (!agentId) {
          console.warn('Received metrics without agent_id');
          return;
        }

        // Find device by agent ID
        const device = await this.database.getDeviceByAgentId(agentId);
        if (!device) {
          console.warn(`gRPC: No device found for agent ${agentId}`);
          return;
        }

        // Track connection and update database status
        if (!this.connections.has(agentId)) {
          this.connections.set(agentId, {
            agentId,
            connectedAt: new Date(),
            lastMetricsAt: new Date(),
          });
          console.log(`gRPC: Agent ${agentId} connected via DataPlane`);
          // Mark device as gRPC connected
          await this.database.updateDeviceGrpcStatus(device.id, true);
        } else {
          const conn = this.connections.get(agentId)!;
          conn.lastMetricsAt = new Date();
          // Update gRPC last seen timestamp
          await this.database.updateDeviceGrpcStatus(device.id, true);
        }

        // Insert metrics into database
        await this.database.insertMetrics(device.id, {
          cpuPercent: metrics.cpuPercent,
          memoryPercent: metrics.memoryPercent,
          memoryUsedBytes: parseInt(metrics.memoryUsed) || 0,
          diskPercent: metrics.diskPercent,
          diskUsedBytes: parseInt(metrics.diskUsed) || 0,
          networkRxBytes: parseInt(metrics.networkRxBytes) || 0,
          networkTxBytes: parseInt(metrics.networkTxBytes) || 0,
          processCount: metrics.processCount || 0,
        });

        // Notify renderer
        this.notifyRenderer('metrics:updated', {
          deviceId: device.id,
          metrics: {
            cpuPercent: metrics.cpuPercent,
            memoryPercent: metrics.memoryPercent,
            memoryUsedBytes: parseInt(metrics.memoryUsed) || 0,
            memoryAvailableBytes: parseInt(metrics.memoryAvailable) || 0,
            diskPercent: metrics.diskPercent,
            diskUsedBytes: parseInt(metrics.diskUsed) || 0,
            diskTotalBytes: parseInt(metrics.diskTotal) || 0,
            networkRxBytes: parseInt(metrics.networkRxBytes) || 0,
            networkTxBytes: parseInt(metrics.networkTxBytes) || 0,
            processCount: metrics.processCount || 0,
            uptime: parseInt(metrics.uptime) || 0,
          },
          source: 'grpc',
        });

        metricsCount++;
      } catch (error) {
        console.error('gRPC: Error processing metrics:', error);
      }
    });

    call.on('end', async () => {
      if (agentId) {
        this.connections.delete(agentId);
        console.log(`gRPC: Agent ${agentId} disconnected from DataPlane (sent ${metricsCount} metrics)`);
        // Mark device as gRPC disconnected
        const device = await this.database.getDeviceByAgentId(agentId);
        if (device) {
          await this.database.updateDeviceGrpcStatus(device.id, false);
        }
      }
      callback(null, { success: true, error: '' });
    });

    call.on('error', async (error: any) => {
      if (agentId) {
        this.connections.delete(agentId);
        // Mark device as gRPC disconnected
        const device = await this.database.getDeviceByAgentId(agentId);
        if (device) {
          await this.database.updateDeviceGrpcStatus(device.id, false);
        }
      }
      // CANCELLED is normal when client disconnects
      if (error.code !== grpc.status.CANCELLED) {
        console.error('gRPC: StreamMetrics error:', error.message);
      }
    });
  }

  // Unary RPC - receives single request, returns single response
  private async handleUploadInventory(
    call: grpc.ServerUnaryCall<any, any>,
    callback: grpc.sendUnaryData<any>
  ): Promise<void> {
    try {
      const inventory = call.request;
      const agentId = inventory.agentId;

      if (!agentId) {
        callback(null, { success: false, error: 'Missing agent_id' });
        return;
      }

      console.log(`gRPC: Received inventory upload from agent ${agentId}`);

      const device = await this.database.getDeviceByAgentId(agentId);
      if (!device) {
        callback(null, { success: false, error: 'Device not found' });
        return;
      }

      // Update device with system info
      const systemInfo = inventory.systemInfo;
      if (systemInfo) {
        await this.updateDeviceSystemInfo(device.id, systemInfo);
      }

      // Store software inventory
      if (inventory.software && inventory.software.length > 0) {
        await this.storeSoftwareInventory(device.id, inventory.software);
      }

      // Notify renderer
      this.notifyRenderer('inventory:updated', {
        deviceId: device.id,
        systemInfo,
        softwareCount: inventory.software?.length || 0,
      });

      callback(null, { success: true, error: '' });
    } catch (error: any) {
      console.error('gRPC: Error processing inventory:', error);
      callback(null, { success: false, error: error.message });
    }
  }

  // Client streaming RPC - receives stream of log batches
  private handleStreamLogs(
    call: grpc.ServerReadableStream<any, any>,
    callback: grpc.sendUnaryData<any>
  ): void {
    let agentId: string | null = null;
    let logCount = 0;

    call.on('data', async (logBatch: any) => {
      try {
        agentId = logBatch.agentId;

        if (!agentId) {
          console.warn('gRPC: Received logs without agent_id');
          return;
        }

        const device = await this.database.getDeviceByAgentId(agentId);
        if (!device) {
          console.warn(`gRPC: No device found for agent ${agentId}`);
          return;
        }

        // Store each log entry
        for (const entry of logBatch.entries || []) {
          await this.storeLogEntry(device.id, entry);
          logCount++;
        }

        // Notify renderer of new logs
        this.notifyRenderer('logs:new', {
          deviceId: device.id,
          count: logBatch.entries?.length || 0,
        });
      } catch (error) {
        console.error('gRPC: Error processing logs:', error);
      }
    });

    call.on('end', () => {
      console.log(`gRPC: Log stream ended from agent ${agentId} (received ${logCount} entries)`);
      callback(null, { success: true, error: '' });
    });

    call.on('error', (error: any) => {
      if (error.code !== grpc.status.CANCELLED) {
        console.error('gRPC: StreamLogs error:', error.message);
      }
    });
  }

  // Client streaming RPC - receives stream of file chunks
  private handleStreamFileContent(
    call: grpc.ServerReadableStream<any, any>,
    callback: grpc.sendUnaryData<any>
  ): void {
    let agentId: string | null = null;
    let requestId: string | null = null;
    const chunks: Buffer[] = [];
    let filePath: string = '';
    let totalSize: number = 0;

    call.on('data', async (chunk: any) => {
      try {
        agentId = chunk.agentId;
        requestId = chunk.requestId;
        filePath = chunk.filePath;
        totalSize = parseInt(chunk.totalSize) || 0;

        // Collect chunks
        if (chunk.data) {
          chunks.push(Buffer.from(chunk.data));
        }

        // Notify progress
        const received = chunks.reduce((sum, c) => sum + c.length, 0);
        this.notifyRenderer('files:progress', {
          requestId,
          filePath,
          bytesTransferred: received,
          totalBytes: totalSize,
          percentage: totalSize > 0 ? Math.round((received / totalSize) * 100) : 0,
        });

        if (chunk.isLast) {
          // File transfer complete
          const fileData = Buffer.concat(chunks);
          this.notifyRenderer('files:complete', {
            requestId,
            filePath,
            data: fileData,
            size: fileData.length,
          });
        }
      } catch (error) {
        console.error('gRPC: Error processing file chunk:', error);
      }
    });

    call.on('end', () => {
      console.log(`gRPC: File stream ended for ${filePath}`);
      callback(null, { success: true, error: '' });
    });

    call.on('error', (error: any) => {
      if (error.code !== grpc.status.CANCELLED) {
        console.error('gRPC: StreamFileContent error:', error.message);
      }
    });
  }

  // Client streaming RPC - receives stream of bulk data chunks
  private handleUploadBulkData(
    call: grpc.ServerReadableStream<any, any>,
    callback: grpc.sendUnaryData<any>
  ): void {
    let agentId: string | null = null;
    let requestId: string | null = null;
    let dataType: string = '';
    const chunks: Buffer[] = [];

    call.on('data', async (chunk: any) => {
      try {
        agentId = chunk.agentId;
        requestId = chunk.requestId;
        dataType = chunk.dataType;

        if (chunk.data) {
          chunks.push(Buffer.from(chunk.data));
        }

        // Notify progress
        this.notifyRenderer('bulk:progress', {
          requestId,
          dataType,
          chunkIndex: chunk.chunkIndex,
          totalChunks: chunk.totalChunks,
        });

        if (chunk.isLast) {
          const fullData = Buffer.concat(chunks);
          console.log(`gRPC: Bulk data upload complete - type: ${dataType}, size: ${fullData.length}`);

          // Process based on data type
          await this.processBulkData(agentId!, dataType, fullData);

          this.notifyRenderer('bulk:complete', {
            requestId,
            dataType,
            size: fullData.length,
          });
        }
      } catch (error) {
        console.error('gRPC: Error processing bulk data:', error);
      }
    });

    call.on('end', () => {
      console.log(`gRPC: Bulk data stream ended for ${dataType}`);
      callback(null, { success: true, error: '' });
    });

    call.on('error', (error: any) => {
      if (error.code !== grpc.status.CANCELLED) {
        console.error('gRPC: UploadBulkData error:', error.message);
      }
    });
  }

  private async updateDeviceSystemInfo(deviceId: string, systemInfo: any): Promise<void> {
    console.log(`gRPC: Updating system info for device ${deviceId}:`, {
      hostname: systemInfo.hostname,
      os: systemInfo.os,
      osVersion: systemInfo.osVersion,
      cpuModel: systemInfo.cpuModel,
      cpuCores: systemInfo.cpuCores,
      totalMemory: systemInfo.totalMemory,
    });
  }

  private async storeSoftwareInventory(deviceId: string, software: any[]): Promise<void> {
    console.log(`gRPC: Storing ${software.length} software entries for device ${deviceId}`);
  }

  private async storeLogEntry(deviceId: string, entry: any): Promise<void> {
    console.log(`gRPC: Storing log entry for device ${deviceId}: [${entry.level}] ${entry.source}`);
  }

  private async processBulkData(agentId: string, dataType: string, data: Buffer): Promise<void> {
    console.log(`gRPC: Processing bulk data - agent: ${agentId}, type: ${dataType}, size: ${data.length}`);

    const device = await this.database.getDeviceByAgentId(agentId);
    if (!device) return;

    switch (dataType) {
      case 'diagnostics':
        break;
      case 'crash_dump':
        break;
      case 'performance_trace':
        break;
      default:
        console.log(`gRPC: Unknown bulk data type: ${dataType}`);
    }
  }

  private notifyRenderer(channel: string, data: any): void {
    const windows = BrowserWindow.getAllWindows();
    for (const win of windows) {
      win.webContents.send(channel, data);
    }
  }
}
