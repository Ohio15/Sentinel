/**
 * Shared Model Types for Sentinel RMM
 * Used by both web and mobile applications
 *
 * Keep these types framework-agnostic (no React, React Native, etc.)
 */

// ============================================================================
// Device Types
// ============================================================================

export type DeviceStatus = 'online' | 'offline' | 'warning' | 'critical' | 'disabled' | 'uninstalling';

export type DeviceType = 'desktop' | 'laptop' | 'server' | 'tablet' | 'virtual';

export interface GPUInfo {
  name: string;
  vendor: string;
  memory: number;
  driverVersion: string;
}

export interface StorageInfo {
  device: string;
  mountpoint: string;
  fstype: string;
  total: number;
  used: number;
  free: number;
  percent: number;
}

export interface PowerManagement {
  wolSupported: boolean;
  wolEnabled: boolean;
  amtSupported: boolean;
  amtProvisioned: boolean;
  amtVersion?: string;
  macAddress: string;
  wolModes?: string;
}

export interface Device {
  id: string;
  agentId: string;
  hostname: string;
  displayName?: string;
  deviceType?: DeviceType;
  osType: string;
  osVersion: string;
  osBuild?: string;
  platform?: string;
  platformFamily?: string;
  architecture: string;
  cpuModel?: string;
  cpuCores?: number;
  cpuThreads?: number;
  cpuSpeed?: number;
  totalMemory?: number;
  bootTime?: number;
  gpu?: GPUInfo[];
  storage?: StorageInfo[];
  serialNumber?: string;
  manufacturer?: string;
  model?: string;
  domain?: string;
  agentVersion: string;
  lastSeen: string;
  status: DeviceStatus;
  ipAddress: string;
  publicIp?: string;
  macAddress: string;
  tags: string[];
  metadata: Record<string, unknown>;
  clientId?: string;
  isDisabled?: boolean;
  disabledAt?: string;
  powerManagement?: PowerManagement;
  createdAt: string;
  updatedAt: string;
}

export interface GPUMetric {
  name: string;
  utilization: number;
  memoryUsed: number;
  memoryTotal: number;
  temperature?: number;
  powerDraw?: number;
}

export interface NetworkInterfaceMetric {
  name: string;
  rxBytesPerSec: number;
  txBytesPerSec: number;
  rxBytes: number;
  txBytes: number;
  rxPackets: number;
  txPackets: number;
  errorsIn: number;
  errorsOut: number;
}

export interface ProcessMetric {
  pid: number;
  name: string;
  cpuPercent: number;
  memPercent: number;
  memoryRss: number;
  status: string;
  username?: string;
}

export interface DeviceMetrics {
  timestamp: string;
  cpuPercent: number;
  memoryPercent: number;
  memoryUsedBytes: number;
  memoryTotalBytes?: number;
  diskPercent: number;
  diskUsedBytes: number;
  diskTotalBytes?: number;
  networkRxBytes: number;
  networkTxBytes: number;
  processCount: number;
  uptime: number;
  // Extended metrics
  diskReadBytesPerSec?: number;
  diskWriteBytesPerSec?: number;
  memoryCommitted?: number;
  memoryCached?: number;
  memoryPagedPool?: number;
  memoryNonPagedPool?: number;
  gpuMetrics?: GPUMetric[];
  networkInterfaces?: NetworkInterfaceMetric[];
  cpuPerCore?: number[];
  topProcesses?: ProcessMetric[];
}

// ============================================================================
// Alert Types
// ============================================================================

export type AlertSeverity = 'info' | 'warning' | 'critical';

export type AlertStatus = 'open' | 'acknowledged' | 'resolved';

export type AlertOperator = 'gt' | 'lt' | 'eq' | 'gte' | 'lte';

export interface Alert {
  id: string;
  deviceId: string;
  deviceName: string;
  ruleId?: string;
  severity: AlertSeverity;
  title: string;
  message: string;
  status: AlertStatus;
  createdAt: string;
  acknowledgedAt?: string;
  resolvedAt?: string;
}

export interface AlertRule {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  metric: string;
  operator: AlertOperator;
  threshold: number;
  severity: AlertSeverity;
  cooldownMinutes: number;
  createdAt: string;
}

// ============================================================================
// Ticket Types
// ============================================================================

export type TicketStatus = 'open' | 'in_progress' | 'waiting' | 'resolved' | 'closed';

export type TicketPriority = 'low' | 'medium' | 'high' | 'urgent';

export type TicketType = 'incident' | 'request' | 'problem' | 'change';

export interface Ticket {
  id: string;
  ticketNumber: number;
  subject: string;
  description?: string;
  status: TicketStatus;
  priority: TicketPriority;
  type: TicketType;
  deviceId?: string;
  deviceName?: string;
  deviceDisplayName?: string;
  requesterName?: string;
  requesterEmail?: string;
  assignedTo?: string;
  tags: string[];
  dueDate?: string;
  resolvedAt?: string;
  closedAt?: string;
  createdAt: string;
  updatedAt: string;
  // SLA fields
  slaPolicyId?: string;
  firstResponseAt?: string;
  firstResponseDueAt?: string;
  resolutionDueAt?: string;
  slaResponseBreached?: boolean;
  slaResolutionBreached?: boolean;
  slaPausedAt?: string;
  slaPausedDurationMinutes?: number;
  // Category field
  categoryId?: string;
  categoryName?: string;
  // Custom fields
  customFields?: Record<string, unknown>;
}

export interface TicketComment {
  id: string;
  ticketId: string;
  content: string;
  isInternal: boolean;
  authorName: string;
  authorEmail?: string;
  attachments: string[];
  createdAt: string;
}

export interface TicketActivity {
  id: string;
  ticketId: string;
  action: string;
  fieldName?: string;
  oldValue?: string;
  newValue?: string;
  actorName: string;
  createdAt: string;
}

export interface TicketStats {
  openCount: number;
  inProgressCount: number;
  waitingCount: number;
  resolvedCount: number;
  closedCount: number;
  totalCount: number;
}

// ============================================================================
// User Types
// ============================================================================

export type UserRole = 'admin' | 'technician' | 'viewer' | 'client';

export interface User {
  id: string;
  username: string;
  email: string;
  firstName?: string;
  lastName?: string;
  role: string;
}

// ============================================================================
// Client Types
// ============================================================================

export interface Client {
  id: string;
  name: string;
  description?: string;
  color?: string;
  logoUrl?: string;
  logoWidth?: number;
  logoHeight?: number;
  deviceCount?: number;
  openTicketCount?: number;
  createdAt: string;
  updatedAt: string;
}

// ============================================================================
// Category Types
// ============================================================================

export interface Category {
  id: string;
  name: string;
  parentId: string | null;
  color: string | null;
  icon: string | null;
  isActive: boolean;
}
