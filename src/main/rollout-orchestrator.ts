/**
 * Rollout Orchestrator
 *
 * Manages staged rollouts of agent updates across update groups.
 * Supports auto-promote, auto-rollback, and manual promotion flows.
 */

import { v4 as uuidv4 } from 'uuid';
import { Database } from './database';
import { AgentManager } from './agents';

export interface UpdateGroup {
  id: string;
  name: string;
  priority: number;
  autoPromote: boolean;
  successThresholdPercent: number;
  failureThresholdPercent: number;
  minDevicesForDecision: number;
  waitTimeMinutes: number;
}

export interface Rollout {
  id: string;
  releaseVersion: string;
  name: string;
  status: 'pending' | 'in_progress' | 'paused' | 'completed' | 'failed' | 'rolled_back';
  downloadUrl?: string;
  checksum?: string;
  startedAt?: Date;
  completedAt?: Date;
  createdAt?: Date;
}

export interface RolloutStage {
  id: string;
  rolloutId: string;
  groupId: string;
  groupName: string;
  groupPriority: number;
  status: 'pending' | 'in_progress' | 'completed' | 'failed' | 'skipped';
  totalDevices: number;
  completedDevices: number;
  failedDevices: number;
  startedAt?: Date;
  completedAt?: Date;
}

export interface RolloutDevice {
  id: string;
  rolloutId: string;
  stageId: string;
  deviceId: string;
  status: 'pending' | 'downloading' | 'installing' | 'completed' | 'failed' | 'rolled_back';
  fromVersion?: string;
  toVersion: string;
  error?: string;
  startedAt?: Date;
  completedAt?: Date;
}

export interface StageEvaluation {
  stage: RolloutStage;
  successRate: number;
  failureRate: number;
  completionRate: number;
  canAutoPromote: boolean;
  shouldRollback: boolean;
  waitTimeRemaining: number;
  message: string;
}

export interface RolloutEvent {
  id: string;
  rolloutId: string;
  eventType: 'created' | 'started' | 'stage_started' | 'stage_completed' | 'stage_failed' | 'promoted' | 'paused' | 'resumed' | 'completed' | 'failed' | 'rolled_back';
  message: string;
  metadata?: Record<string, any>;
  createdAt: Date;
}

export class RolloutOrchestrator {
  private database: Database;
  private agentManager: AgentManager;
  private monitoringInterval: NodeJS.Timeout | null = null;
  private readonly MONITOR_INTERVAL_MS = 30000; // Check every 30 seconds

  constructor(database: Database, agentManager: AgentManager) {
    this.database = database;
    this.agentManager = agentManager;
  }

  /**
   * Start monitoring active rollouts
   */
  startMonitoring(): void {
    if (this.monitoringInterval) {
      return;
    }

    console.log('[Rollout] Starting rollout monitoring');
    this.monitoringInterval = setInterval(() => {
      this.monitorActiveRollouts().catch(err => {
        console.error('[Rollout] Monitoring error:', err);
      });
    }, this.MONITOR_INTERVAL_MS);

    // Run immediately
    this.monitorActiveRollouts().catch(err => {
      console.error('[Rollout] Initial monitoring error:', err);
    });
  }

  /**
   * Stop monitoring active rollouts
   */
  stopMonitoring(): void {
    if (this.monitoringInterval) {
      clearInterval(this.monitoringInterval);
      this.monitoringInterval = null;
      console.log('[Rollout] Stopped rollout monitoring');
    }
  }

  /**
   * Create a new rollout for a release version
   */
  async createRollout(
    releaseVersion: string,
    name: string,
    downloadUrl: string,
    checksum: string
  ): Promise<Rollout> {
    const rolloutId = uuidv4();

    // Create the rollout record
    await this.database.createRollout({
      id: rolloutId,
      releaseVersion,
      name,
      downloadUrl,
      checksum
    });

    // Get all update groups ordered by priority
    const groups = await this.database.getUpdateGroups();
    if (groups.length === 0) {
      throw new Error('No update groups defined. Create at least one update group before starting a rollout.');
    }

    // Create stages for each group
    for (const group of groups) {
      // Count devices in this group
      const devices = await this.database.getDevicesInUpdateGroup(group.id);
      const deviceCount = devices.length;

      const stageId = uuidv4();
      await this.database.createRolloutStage({
        id: stageId,
        rolloutId,
        groupId: group.id,
        totalDevices: deviceCount
      });
    }

    // Log event
    await this.logEvent(rolloutId, 'created', `Rollout created for version ${releaseVersion}`, {
      stageCount: groups.length
    });

    console.log(`[Rollout] Created rollout ${rolloutId} for version ${releaseVersion} with ${groups.length} stages`);

    // Fetch and return the created rollout
    const rollouts = await this.database.getRollouts(1);
    return this.mapRollout(rollouts.find(r => r.id === rolloutId) || {
      id: rolloutId,
      releaseVersion,
      name,
      status: 'pending',
      downloadUrl,
      checksum
    });
  }

  /**
   * Start a rollout (begins with lowest priority group)
   */
  async startRollout(rolloutId: string): Promise<void> {
    const rollouts = await this.database.getRollouts(100);
    const rollout = rollouts.find(r => r.id === rolloutId);

    if (!rollout) {
      throw new Error(`Rollout ${rolloutId} not found`);
    }

    if (rollout.status !== 'pending' && rollout.status !== 'paused') {
      throw new Error(`Cannot start rollout in ${rollout.status} status`);
    }

    // Update rollout status
    await this.database.updateRolloutStatus(rolloutId, 'in_progress');

    // Start the first stage
    const stages = await this.database.getRolloutStages(rolloutId);
    const firstStage = stages.find(s => s.status === 'pending');

    if (firstStage) {
      await this.startStage(rolloutId, firstStage.id);
    }

    await this.logEvent(rolloutId, 'started', 'Rollout started');
    console.log(`[Rollout] Started rollout ${rolloutId}`);
  }

  /**
   * Pause an active rollout
   */
  async pauseRollout(rolloutId: string): Promise<void> {
    const rollouts = await this.database.getRollouts(100);
    const rollout = rollouts.find(r => r.id === rolloutId);

    if (!rollout) {
      throw new Error(`Rollout ${rolloutId} not found`);
    }

    if (rollout.status !== 'in_progress') {
      throw new Error(`Cannot pause rollout in ${rollout.status} status`);
    }

    await this.database.updateRolloutStatus(rolloutId, 'paused');
    await this.logEvent(rolloutId, 'paused', 'Rollout paused');
    console.log(`[Rollout] Paused rollout ${rolloutId}`);
  }

  /**
   * Resume a paused rollout
   */
  async resumeRollout(rolloutId: string): Promise<void> {
    const rollouts = await this.database.getRollouts(100);
    const rollout = rollouts.find(r => r.id === rolloutId);

    if (!rollout) {
      throw new Error(`Rollout ${rolloutId} not found`);
    }

    if (rollout.status !== 'paused') {
      throw new Error(`Cannot resume rollout in ${rollout.status} status`);
    }

    await this.database.updateRolloutStatus(rolloutId, 'in_progress');
    await this.logEvent(rolloutId, 'resumed', 'Rollout resumed');
    console.log(`[Rollout] Resumed rollout ${rolloutId}`);
  }

  /**
   * Manually promote to the next stage
   */
  async promoteStage(rolloutId: string, stageId: string): Promise<void> {
    const stages = await this.database.getRolloutStages(rolloutId);
    const stage = stages.find(s => s.id === stageId);

    if (!stage) {
      throw new Error(`Stage ${stageId} not found`);
    }

    if (stage.status !== 'in_progress' && stage.status !== 'completed') {
      throw new Error(`Cannot promote stage in ${stage.status} status`);
    }

    // Mark current stage as completed if not already
    if (stage.status === 'in_progress') {
      await this.database.updateRolloutStageStatus(stageId, 'completed');
    }

    // Find and start next stage
    const currentIndex = stages.findIndex(s => s.id === stageId);
    const nextStage = stages[currentIndex + 1];

    if (nextStage && nextStage.status === 'pending') {
      await this.startStage(rolloutId, nextStage.id);
      await this.logEvent(rolloutId, 'promoted', `Promoted from ${stage.groupName} to ${nextStage.groupName}`, {
        fromStageId: stageId,
        toStageId: nextStage.id
      });
    } else {
      // No more stages - rollout complete
      await this.completeRollout(rolloutId);
    }
  }

  /**
   * Rollback a rollout - cancel remaining updates and revert completed ones
   */
  async rollbackRollout(rolloutId: string, reason: string): Promise<void> {
    console.log(`[Rollout] Rolling back rollout ${rolloutId}: ${reason}`);

    // Update rollout status
    await this.database.updateRolloutStatus(rolloutId, 'rolled_back');

    // Mark all pending/in_progress stages as skipped
    const stages = await this.database.getRolloutStages(rolloutId);
    for (const stage of stages) {
      if (stage.status === 'pending' || stage.status === 'in_progress') {
        await this.database.updateRolloutStageStatus(stage.id, 'skipped');
      }
    }

    // Get all devices that completed the update
    const completedDevices = await this.database.getRolloutDevices(rolloutId);
    const devicesToRollback = completedDevices.filter(d => d.status === 'completed');

    // Queue rollback commands for completed devices
    for (const device of devicesToRollback) {
      if (device.fromVersion) {
        await this.database.queueCommand({
          id: uuidv4(),
          deviceId: device.deviceId,
          commandType: 'rollback_update',
          payload: { targetVersion: device.fromVersion },
          priority: 100, // High priority
          expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000) // 24 hours
        });

        await this.database.updateRolloutDeviceStatus(device.deviceId, rolloutId, 'rolled_back');
      }
    }

    await this.logEvent(rolloutId, 'rolled_back', `Rollout rolled back: ${reason}`, {
      devicesAffected: devicesToRollback.length
    });

    console.log(`[Rollout] Rolled back ${devicesToRollback.length} devices`);
  }

  /**
   * Start a specific stage
   */
  private async startStage(rolloutId: string, stageId: string): Promise<void> {
    const rollouts = await this.database.getRollouts(100);
    const rollout = rollouts.find(r => r.id === rolloutId);
    const stages = await this.database.getRolloutStages(rolloutId);
    const stage = stages.find(s => s.id === stageId);

    if (!rollout || !stage) {
      throw new Error('Rollout or stage not found');
    }

    console.log(`[Rollout] Starting stage ${stage.groupName} for rollout ${rolloutId}`);

    // Update stage status
    await this.database.updateRolloutStageStatus(stageId, 'in_progress');

    // Get all devices in this group
    const devices = await this.database.getDevicesInUpdateGroup(stage.groupId);

    // Create rollout_devices entries and send update commands
    for (const device of devices) {
      const deviceRolloutId = uuidv4();

      // Get current version
      const currentVersion = device.agent_version || device.agentVersion || 'unknown';

      await this.database.addRolloutDevice({
        id: deviceRolloutId,
        rolloutId,
        stageId,
        deviceId: device.id,
        fromVersion: currentVersion,
        toVersion: rollout.releaseVersion
      });

      // Send update command to agent
      const updatePayload = {
        version: rollout.releaseVersion,
        downloadUrl: rollout.downloadUrl,
        checksum: rollout.checksum,
        rolloutId,
        stageId,
        deviceRolloutId
      };

      // Queue if offline, send immediately if online
      await this.agentManager.executeCommand(
        device.id,
        JSON.stringify(updatePayload),
        'update_agent',
        { queueIfOffline: true, priority: 80, expiresInMinutes: 60 * 24 }
      );
    }

    await this.logEvent(rolloutId, 'stage_started', `Started stage: ${stage.groupName}`, {
      stageId,
      deviceCount: devices.length
    });

    console.log(`[Rollout] Sent update commands to ${devices.length} devices in ${stage.groupName}`);
  }

  /**
   * Monitor active rollouts and evaluate stages
   */
  private async monitorActiveRollouts(): Promise<void> {
    const rollouts = await this.database.getRollouts(100);
    const activeRollouts = rollouts.filter(r => r.status === 'in_progress');

    for (const rollout of activeRollouts) {
      await this.evaluateRollout(rollout);
    }
  }

  /**
   * Evaluate a rollout and its current stage
   */
  private async evaluateRollout(rollout: any): Promise<void> {
    const stages = await this.database.getRolloutStages(rollout.id);
    const currentStage = stages.find(s => s.status === 'in_progress');

    if (!currentStage) {
      // No active stage - check if all are complete
      const allComplete = stages.every(s => s.status === 'completed' || s.status === 'skipped');
      if (allComplete) {
        await this.completeRollout(rollout.id);
      }
      return;
    }

    // Evaluate the current stage
    const evaluation = await this.evaluateStage(this.mapStage(currentStage));

    if (evaluation.shouldRollback) {
      console.log(`[Rollout] Stage ${currentStage.groupName} failed evaluation: ${evaluation.message}`);
      await this.rollbackRollout(rollout.id, evaluation.message);
      return;
    }

    if (evaluation.canAutoPromote) {
      console.log(`[Rollout] Stage ${currentStage.groupName} auto-promoting: ${evaluation.message}`);
      await this.promoteStage(rollout.id, currentStage.id);
    }
  }

  /**
   * Evaluate a stage's progress and determine next action
   */
  async evaluateStage(stage: RolloutStage): Promise<StageEvaluation> {
    // Get the group settings
    const groups = await this.database.getUpdateGroups();
    const group = groups.find(g => g.id === stage.groupId);

    if (!group) {
      return {
        stage,
        successRate: 0,
        failureRate: 0,
        completionRate: 0,
        canAutoPromote: false,
        shouldRollback: false,
        waitTimeRemaining: 0,
        message: 'Update group not found'
      };
    }

    // Calculate rates
    const totalResponded = stage.completedDevices + stage.failedDevices;
    const successRate = stage.totalDevices > 0
      ? (stage.completedDevices / stage.totalDevices) * 100
      : 0;
    const failureRate = stage.totalDevices > 0
      ? (stage.failedDevices / stage.totalDevices) * 100
      : 0;
    const completionRate = stage.totalDevices > 0
      ? (totalResponded / stage.totalDevices) * 100
      : 0;

    // Calculate wait time
    const stageStartTime = stage.startedAt ? new Date(stage.startedAt).getTime() : Date.now();
    const waitTimeMs = (group.wait_time_minutes || group.waitTimeMinutes || 60) * 60 * 1000;
    const elapsed = Date.now() - stageStartTime;
    const waitTimeRemaining = Math.max(0, waitTimeMs - elapsed);

    // Get thresholds from group
    const successThreshold = group.success_threshold_percent || group.successThresholdPercent || 95;
    const failureThreshold = group.failure_threshold_percent || group.failureThresholdPercent || 10;
    const minDevices = group.min_devices_for_decision || group.minDevicesForDecision || 3;
    const autoPromote = group.auto_promote ?? group.autoPromote ?? false;

    // Check rollback condition
    if (failureRate >= failureThreshold && totalResponded >= minDevices) {
      return {
        stage,
        successRate,
        failureRate,
        completionRate,
        canAutoPromote: false,
        shouldRollback: true,
        waitTimeRemaining,
        message: `Failure rate ${failureRate.toFixed(1)}% exceeds threshold ${failureThreshold}%`
      };
    }

    // Check auto-promote conditions
    const canAutoPromote =
      autoPromote &&
      successRate >= successThreshold &&
      waitTimeRemaining === 0 &&
      totalResponded >= minDevices;

    let message = '';
    if (canAutoPromote) {
      message = `Success rate ${successRate.toFixed(1)}% meets threshold ${successThreshold}%`;
    } else if (!autoPromote) {
      message = 'Waiting for manual promotion';
    } else if (waitTimeRemaining > 0) {
      message = `Waiting ${Math.ceil(waitTimeRemaining / 60000)} more minutes`;
    } else if (totalResponded < minDevices) {
      message = `Waiting for ${minDevices - totalResponded} more device responses`;
    } else {
      message = `Success rate ${successRate.toFixed(1)}% below threshold ${successThreshold}%`;
    }

    return {
      stage,
      successRate,
      failureRate,
      completionRate,
      canAutoPromote,
      shouldRollback: false,
      waitTimeRemaining,
      message
    };
  }

  /**
   * Mark a rollout as completed
   */
  private async completeRollout(rolloutId: string): Promise<void> {
    await this.database.updateRolloutStatus(rolloutId, 'completed');
    await this.logEvent(rolloutId, 'completed', 'Rollout completed successfully');
    console.log(`[Rollout] Completed rollout ${rolloutId}`);
  }

  /**
   * Handle device update result (called when agent reports update status)
   */
  async handleDeviceUpdateResult(
    deviceId: string,
    rolloutId: string,
    success: boolean,
    error?: string
  ): Promise<void> {
    // Update the device status
    const status = success ? 'completed' : 'failed';
    await this.database.updateRolloutDeviceStatus(deviceId, rolloutId, status, error);

    // Get the device record to find stage
    const devices = await this.database.getRolloutDevices(rolloutId);
    const deviceRecord = devices.find(d => d.deviceId === deviceId);

    if (deviceRecord) {
      const stages = await this.database.getRolloutStages(rolloutId);
      const stage = stages.find(s => s.id === deviceRecord.stageId);

      if (stage) {
        // Increment the appropriate counter
        const completed = success ? stage.completedDevices + 1 : stage.completedDevices;
        const failed = success ? stage.failedDevices : stage.failedDevices + 1;
        await this.database.updateRolloutStageDeviceCounts(stage.id, completed, failed);
      }
    }

    console.log(`[Rollout] Device ${deviceId} update ${success ? 'succeeded' : 'failed'} for rollout ${rolloutId}`);
  }

  /**
   * Log a rollout event
   */
  private async logEvent(
    rolloutId: string,
    eventType: RolloutEvent['eventType'],
    message: string,
    metadata?: Record<string, any>
  ): Promise<void> {
    await this.database.addRolloutEvent({
      id: uuidv4(),
      rolloutId,
      eventType,
      message,
      metadata
    });
  }

  /**
   * Get rollout events
   */
  async getRolloutEvents(rolloutId: string): Promise<RolloutEvent[]> {
    const events = await this.database.getRolloutEvents(rolloutId);
    return events.map(e => ({
      id: e.id,
      rolloutId: e.rolloutId,
      eventType: e.eventType,
      message: e.message,
      metadata: e.metadata,
      createdAt: e.createdAt
    }));
  }

  /**
   * Get rollout statistics
   */
  async getRolloutStats(rolloutId: string): Promise<{
    totalDevices: number;
    completedDevices: number;
    failedDevices: number;
    pendingDevices: number;
    overallProgress: number;
    currentStage?: string;
  }> {
    const stages = await this.database.getRolloutStages(rolloutId);

    let totalDevices = 0;
    let completedDevices = 0;
    let failedDevices = 0;
    let currentStage: string | undefined;

    for (const stage of stages) {
      totalDevices += stage.totalDevices;
      completedDevices += stage.completedDevices;
      failedDevices += stage.failedDevices;

      if (stage.status === 'in_progress') {
        currentStage = stage.groupName;
      }
    }

    const pendingDevices = totalDevices - completedDevices - failedDevices;
    const overallProgress = totalDevices > 0
      ? ((completedDevices + failedDevices) / totalDevices) * 100
      : 0;

    return {
      totalDevices,
      completedDevices,
      failedDevices,
      pendingDevices,
      overallProgress,
      currentStage
    };
  }

  /**
   * Map database rollout to interface
   */
  private mapRollout(row: any): Rollout {
    return {
      id: row.id,
      releaseVersion: row.releaseVersion || row.release_version,
      name: row.name,
      status: row.status,
      downloadUrl: row.downloadUrl || row.download_url,
      checksum: row.checksum,
      startedAt: row.startedAt || row.started_at,
      completedAt: row.completedAt || row.completed_at,
      createdAt: row.createdAt || row.created_at
    };
  }

  /**
   * Map database stage to interface
   */
  private mapStage(row: any): RolloutStage {
    return {
      id: row.id,
      rolloutId: row.rolloutId || row.rollout_id,
      groupId: row.groupId || row.group_id,
      groupName: row.groupName || row.group_name,
      groupPriority: row.groupPriority || row.group_priority,
      status: row.status,
      totalDevices: row.totalDevices || row.total_devices,
      completedDevices: row.completedDevices || row.completed_devices,
      failedDevices: row.failedDevices || row.failed_devices,
      startedAt: row.startedAt || row.started_at,
      completedAt: row.completedAt || row.completed_at
    };
  }
}
