/**
 * Shared API Types for Sentinel RMM
 * Common request/response shapes used by both web and mobile applications
 *
 * Keep these types framework-agnostic (no React, React Native, etc.)
 */

import type {
  Device,
  DeviceMetrics,
  Alert,
  AlertRule,
  Ticket,
  TicketComment,
  TicketActivity,
  TicketStats,
  User,
  Client,
} from './models';

// ============================================================================
// Generic API Response Types
// ============================================================================

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
  message?: string;
}

export interface ApiError {
  message: string;
  code?: string;
  status?: number;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface PaginationParams {
  page?: number;
  pageSize?: number;
}

// ============================================================================
// Authentication Types
// ============================================================================

export interface LoginRequest {
  identifier: string;  // username or email
  password: string;
}

export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;  // seconds until token expires
  user: User;
}

export interface RefreshTokenRequest {
  refreshToken: string;
}

export interface RefreshTokenResponse {
  token: string;
  expiresIn: number;
}

export interface RegisterRequest {
  token: string;  // invitation token
  username: string;
  email: string;
  password: string;
  firstName?: string;
  lastName?: string;
}

// ============================================================================
// Device API Types
// ============================================================================

export interface DeviceListParams extends PaginationParams {
  status?: string;
  search?: string;
  clientId?: string;
}

export interface DeviceListResponse extends PaginatedResponse<Device> {}

export interface DeviceMetricsParams {
  from?: string;  // ISO date string
  to?: string;    // ISO date string
  limit?: number;
}

export interface DeviceUpdateRequest {
  displayName?: string;
  tags?: string[];
  clientId?: string | null;
}

export interface CommandRequest {
  command: string;
  commandType?: 'shell' | 'powershell' | 'bash';
}

export interface CommandResponse {
  id: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  output?: string;
  error?: string;
  createdAt: string;
  completedAt?: string;
}

export interface PowerActionRequest {
  action: 'shutdown' | 'restart' | 'wake';
}

// ============================================================================
// Alert API Types
// ============================================================================

export interface AlertListParams {
  status?: string;
  severity?: string;
}

export interface AlertRuleCreateRequest {
  name: string;
  description?: string;
  conditions: unknown[];  // Flexible condition structure
  severity: string;
  cooldownMinutes?: number;
}

export interface AlertRuleUpdateRequest {
  name?: string;
  description?: string;
  enabled?: boolean;
  conditions?: unknown[];
  severity?: string;
  cooldownMinutes?: number;
}

// ============================================================================
// Ticket API Types
// ============================================================================

export interface TicketListParams extends PaginationParams {
  status?: string;
  priority?: string;
  assignedTo?: string;
  deviceId?: string;
}

export interface TicketCreateRequest {
  subject: string;
  description?: string;
  priority?: string;
  type?: string;
  deviceId?: string;
  categoryId?: string;
  tags?: string[];
}

export interface TicketUpdateRequest {
  subject?: string;
  description?: string;
  status?: string;
  priority?: string;
  assignedTo?: string;
  categoryId?: string;
  dueDate?: string;
  tags?: string[];
  actorName?: string;  // For activity logging
}

export interface TicketCommentRequest {
  ticketId: string;
  content: string;
  isInternal?: boolean;
  authorName: string;
  authorEmail?: string;
  attachments?: string[];
}

// ============================================================================
// Client API Types
// ============================================================================

export interface ClientCreateRequest {
  name: string;
  description?: string;
  contactEmail?: string;
  color?: string;
}

export interface ClientUpdateRequest {
  name?: string;
  description?: string;
  contactEmail?: string;
  color?: string;
}

// ============================================================================
// User API Types
// ============================================================================

export interface UserCreateRequest {
  email: string;
  password: string;
  firstName: string;
  lastName: string;
  role: string;
}

export interface UserUpdateRequest {
  firstName?: string;
  lastName?: string;
  role?: string;
  password?: string;
}

export interface UserListParams {
  search?: string;
}

// ============================================================================
// Dashboard Types
// ============================================================================

export interface DashboardStats {
  totalDevices: number;
  onlineDevices: number;
  offlineDevices: number;
  criticalDevices: number;
  totalAlerts: number;
  openAlerts: number;
  totalTickets: number;
  openTickets: number;
}

// ============================================================================
// Enrollment Types
// ============================================================================

export interface EnrollmentToken {
  id: string;
  token: string;
  name: string;
  description?: string;
  isActive: boolean;
  expiresAt?: string;
  maxUses?: number;
  usedCount: number;
  tags: string[];
  createdAt: string;
  updatedAt: string;
}

export interface EnrollmentTokenCreateRequest {
  name: string;
  description?: string;
  expiresAt?: string;
  maxUses?: number;
  tags?: string[];
}

export interface EnrollmentTokenUpdateRequest {
  name?: string;
  description?: string;
  isActive?: boolean;
}

// ============================================================================
// Installation Code Types
// ============================================================================

export interface InstallationCode {
  id: string;
  code: string;
  deviceName: string;
  status: 'pending' | 'used' | 'expired' | 'revoked';
  createdAt: string;
  expiresAt: string;
  usedAt?: string;
  createdByName?: string;
}

export interface InstallationCodeCreateRequest {
  deviceName: string;
  userName?: string;
  notes?: string;
  expirationDays?: number;
}

export interface InstallationCodeCreateResponse {
  success: boolean;
  code: string;
  deviceName: string;
  downloadUrl: string;
  expiresAt: string;
  instructions: string;
}

// ============================================================================
// Agent Link Types
// ============================================================================

export interface AgentLink {
  id: string;
  deviceName: string;
  userEmail: string;
  userName?: string;
  status: 'pending' | 'clicked' | 'installed' | 'expired' | 'revoked';
  expiresAt: string;
  notes?: string;
  createdAt: string;
  updatedAt: string;
}

export interface AgentLinkCreateRequest {
  deviceName: string;
  userEmail: string;
  userName?: string;
  expiresInHours?: number;
  sendEmail?: boolean;
  notes?: string;
}

export interface AgentLinkListResponse extends PaginatedResponse<AgentLink> {
  links: AgentLink[];  // Alias for backwards compatibility
}

// ============================================================================
// WebSocket Event Types
// ============================================================================

export interface DeviceOnlineEvent {
  agentId: string;
  deviceId?: string;
}

export interface DeviceOfflineEvent {
  agentId: string;
}

export interface DeviceUpdatedEvent {
  deviceId: string;
}

export interface MetricsUpdatedEvent {
  deviceId: string;
  source?: string;
  metrics: Partial<DeviceMetrics>;
}

export interface AlertCreatedEvent {
  alert: Alert;
}
