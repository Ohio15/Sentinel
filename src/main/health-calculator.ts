import { Database } from './database';

// Health score calculation factors and weights
interface HealthFactors {
  heartbeat: number;   // 30% - Connection reliability
  metrics: number;     // 20% - Metrics reporting consistency
  commands: number;    // 20% - Command execution success
  updates: number;     // 15% - Update compliance
  resources: number;   // 15% - System resource health
}

const HEALTH_WEIGHTS = {
  heartbeat: 0.30,
  metrics: 0.20,
  commands: 0.20,
  updates: 0.15,
  resources: 0.15,
};

export type HealthStatus = 'healthy' | 'degraded' | 'unhealthy' | 'unknown';

export interface HealthResult {
  score: number;
  status: HealthStatus;
  factors: HealthFactors;
  components: Record<string, ComponentHealth>;
}

export interface ComponentHealth {
  name: string;
  status: 'ok' | 'warning' | 'critical' | 'unknown';
  value?: number;
  message?: string;
}

export interface HealthHistoryEntry {
  deviceId: string;
  healthScore: number;
  status: HealthStatus;
  factors: HealthFactors;
  recordedAt: Date;
}

export class HealthCalculator {
  private database: Database;
  private updateInterval: NodeJS.Timeout | null = null;

  constructor(database: Database) {
    this.database = database;
  }

  // Start background health score updates
  startBackgroundUpdates(intervalMs: number = 60000): void {
    if (this.updateInterval) {
      clearInterval(this.updateInterval);
    }

    this.updateInterval = setInterval(async () => {
      try {
        await this.updateAllHealthScores();
      } catch (error) {
        console.error('Failed to update health scores:', error);
      }
    }, intervalMs);

    // Run immediately on start
    this.updateAllHealthScores().catch(console.error);
  }

  // Stop background updates
  stopBackgroundUpdates(): void {
    if (this.updateInterval) {
      clearInterval(this.updateInterval);
      this.updateInterval = null;
    }
  }

  // Calculate health score for a specific device
  async calculateHealthScore(deviceId: string): Promise<HealthResult> {
    const device = await this.database.getDevice(deviceId);
    if (!device) {
      return this.getEmptyResult();
    }

    const factors: HealthFactors = {
      heartbeat: await this.calculateHeartbeatScore(device),
      metrics: await this.calculateMetricsScore(device),
      commands: await this.calculateCommandScore(deviceId),
      updates: await this.calculateUpdateScore(device),
      resources: await this.calculateResourceScore(deviceId),
    };

    const score = Math.round(
      factors.heartbeat * HEALTH_WEIGHTS.heartbeat +
      factors.metrics * HEALTH_WEIGHTS.metrics +
      factors.commands * HEALTH_WEIGHTS.commands +
      factors.updates * HEALTH_WEIGHTS.updates +
      factors.resources * HEALTH_WEIGHTS.resources
    );

    const status = this.getStatusFromScore(score);
    const components = await this.getComponentHealth(deviceId, device);

    return { score, status, factors, components };
  }

  // Update health scores for all devices
  async updateAllHealthScores(): Promise<void> {
    const devices = await this.database.getDevices();

    for (const device of devices) {
      try {
        const result = await this.calculateHealthScore(device.id);
        await this.saveHealthScore(device.id, result);
      } catch (error) {
        console.error(`Failed to update health for device ${device.id}:`, error);
      }
    }
  }

  // Save health score to database
  private async saveHealthScore(deviceId: string, result: HealthResult): Promise<void> {
    await this.database.upsertAgentHealth(deviceId, {
      healthScore: result.score,
      status: result.status,
      factors: result.factors,
      components: result.components,
      updatedAt: new Date(),
    });
  }

  // Record health history snapshot
  async recordHealthSnapshot(): Promise<number> {
    return await this.database.recordHealthSnapshot();
  }

  // Get health history for a device
  async getHealthHistory(deviceId: string, hours: number = 24): Promise<HealthHistoryEntry[]> {
    return await this.database.getAgentHealthHistory(deviceId, hours);
  }

  // Heartbeat reliability score (last seen)
  private async calculateHeartbeatScore(device: any): Promise<number> {
    if (!device.lastSeen) return 0;

    const lastSeenMs = new Date(device.lastSeen).getTime();
    const now = Date.now();
    const minutesSinceLastSeen = (now - lastSeenMs) / 60000;

    // Perfect score if seen in last 2 minutes
    if (minutesSinceLastSeen <= 2) return 100;
    // Degrade linearly: 0 after 30 minutes
    if (minutesSinceLastSeen >= 30) return 0;

    return Math.round(100 - (minutesSinceLastSeen / 30) * 100);
  }

  // Metrics reporting consistency score
  private async calculateMetricsScore(device: any): Promise<number> {
    try {
      const metrics = await this.database.getDeviceMetrics(device.id, 1); // Last hour

      if (!metrics || metrics.length === 0) return 0;

      // Expect metrics every 60 seconds = 60 metrics per hour
      const expectedMetrics = 60;
      const coverage = (metrics.length / expectedMetrics) * 100;
      return Math.min(100, Math.round(coverage));
    } catch {
      return 50; // Default to degraded if can't determine
    }
  }

  // Command execution success rate
  private async calculateCommandScore(deviceId: string): Promise<number> {
    try {
      const commands = await this.database.getCommandHistory(deviceId, 20);

      if (!commands || commands.length === 0) return 100; // No commands = assume healthy

      const completed = commands.filter((c: any) => c.status === 'completed').length;
      return Math.round((completed / commands.length) * 100);
    } catch {
      return 100; // Default to healthy if can't determine
    }
  }

  // Update compliance score
  private async calculateUpdateScore(device: any): Promise<number> {
    try {
      const latestRelease = await this.database.getLatestAgentRelease();
      if (!latestRelease) return 100;

      if (device.agentVersion === latestRelease.version) return 100;

      // Check how many versions behind
      const releases = await this.database.getAgentReleases();
      if (!releases || releases.length === 0) return 100;

      const currentIndex = releases.findIndex((r: any) => r.version === device.agentVersion);
      const latestIndex = releases.findIndex((r: any) => r.version === latestRelease.version);

      if (currentIndex === -1) return 50; // Unknown version

      const versionsBehind = Math.abs(latestIndex - currentIndex);
      // Lose 20 points per version behind
      return Math.max(0, 100 - versionsBehind * 20);
    } catch {
      return 100; // Default to healthy if can't determine
    }
  }

  // System resource health score
  private async calculateResourceScore(deviceId: string): Promise<number> {
    try {
      const metrics = await this.database.getDeviceMetrics(deviceId, 1);

      if (!metrics || metrics.length === 0) return 100;

      const latest = metrics[0];
      let score = 100;

      // Deduct for high CPU
      const cpuPercent = latest.cpuPercent || latest.cpu_percent || 0;
      if (cpuPercent > 90) score -= 30;
      else if (cpuPercent > 80) score -= 15;

      // Deduct for high memory
      const memoryPercent = latest.memoryPercent || latest.memory_percent || 0;
      if (memoryPercent > 90) score -= 30;
      else if (memoryPercent > 80) score -= 15;

      // Deduct for high disk
      const diskPercent = latest.diskPercent || latest.disk_percent || 0;
      if (diskPercent > 95) score -= 30;
      else if (diskPercent > 85) score -= 15;

      return Math.max(0, score);
    } catch {
      return 100; // Default to healthy if can't determine
    }
  }

  // Get individual component health details
  private async getComponentHealth(deviceId: string, device: any): Promise<Record<string, ComponentHealth>> {
    const components: Record<string, ComponentHealth> = {};

    // Connection status
    const lastSeenMs = device.lastSeen ? new Date(device.lastSeen).getTime() : 0;
    const minutesSinceLastSeen = lastSeenMs ? (Date.now() - lastSeenMs) / 60000 : Infinity;

    components['connection'] = {
      name: 'Connection',
      status: minutesSinceLastSeen <= 2 ? 'ok' : minutesSinceLastSeen <= 10 ? 'warning' : 'critical',
      value: Math.round(minutesSinceLastSeen),
      message: minutesSinceLastSeen <= 2 ? 'Online' : `Last seen ${Math.round(minutesSinceLastSeen)} minutes ago`,
    };

    // Agent version
    try {
      const latestRelease = await this.database.getLatestAgentRelease();
      const isUpToDate = !latestRelease || device.agentVersion === latestRelease.version;
      components['version'] = {
        name: 'Agent Version',
        status: isUpToDate ? 'ok' : 'warning',
        message: device.agentVersion || 'Unknown',
      };
    } catch {
      components['version'] = {
        name: 'Agent Version',
        status: 'unknown',
        message: device.agentVersion || 'Unknown',
      };
    }

    // Resource usage
    try {
      const metrics = await this.database.getDeviceMetrics(deviceId, 1);
      if (metrics && metrics.length > 0) {
        const latest = metrics[0];
        const cpuPercent = latest.cpuPercent || latest.cpu_percent || 0;
        const memoryPercent = latest.memoryPercent || latest.memory_percent || 0;
        const diskPercent = latest.diskPercent || latest.disk_percent || 0;

        components['cpu'] = {
          name: 'CPU Usage',
          status: cpuPercent > 90 ? 'critical' : cpuPercent > 80 ? 'warning' : 'ok',
          value: cpuPercent,
          message: `${cpuPercent.toFixed(1)}%`,
        };

        components['memory'] = {
          name: 'Memory Usage',
          status: memoryPercent > 90 ? 'critical' : memoryPercent > 80 ? 'warning' : 'ok',
          value: memoryPercent,
          message: `${memoryPercent.toFixed(1)}%`,
        };

        components['disk'] = {
          name: 'Disk Usage',
          status: diskPercent > 95 ? 'critical' : diskPercent > 85 ? 'warning' : 'ok',
          value: diskPercent,
          message: `${diskPercent.toFixed(1)}%`,
        };
      }
    } catch {
      // Skip resource components if metrics unavailable
    }

    return components;
  }

  private getStatusFromScore(score: number): HealthStatus {
    if (score >= 80) return 'healthy';
    if (score >= 50) return 'degraded';
    if (score > 0) return 'unhealthy';
    return 'unknown';
  }

  private getEmptyResult(): HealthResult {
    return {
      score: 0,
      status: 'unknown',
      factors: { heartbeat: 0, metrics: 0, commands: 0, updates: 0, resources: 0 },
      components: {},
    };
  }

  private getEmptyFactors(): HealthFactors {
    return { heartbeat: 0, metrics: 0, commands: 0, updates: 0, resources: 0 };
  }
}

export default HealthCalculator;
