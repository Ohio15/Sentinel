/**
 * Shared Utility Functions for Sentinel RMM
 * Used by both web and mobile applications
 *
 * Keep these framework-agnostic (no React, React Native, etc.)
 * These utilities return generic values (strings, colors as hex/names)
 * that can be used with any UI framework
 */

import {
  DEVICE_STATUS,
  ALERT_SEVERITY,
  ALERT_STATUS,
  TICKET_STATUS,
  TICKET_PRIORITY,
} from '../constants';

// ============================================================================
// Date Formatting Utilities
// ============================================================================

/**
 * Format a date as relative time (e.g., "2h ago", "3d ago")
 */
export function formatRelativeTime(dateString: string | Date): string {
  const date = typeof dateString === 'string' ? new Date(dateString) : dateString;
  const now = new Date();
  const diff = now.getTime() - date.getTime();

  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const weeks = Math.floor(days / 7);
  const months = Math.floor(days / 30);

  if (seconds < 60) return 'Just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  if (weeks < 4) return `${weeks}w ago`;
  if (months < 12) return `${months}mo ago`;

  return date.toLocaleDateString();
}

/**
 * Format a date for display (localized short date)
 */
export function formatDate(dateString: string | Date): string {
  const date = typeof dateString === 'string' ? new Date(dateString) : dateString;
  return date.toLocaleDateString();
}

/**
 * Format a date with time
 */
export function formatDateTime(dateString: string | Date): string {
  const date = typeof dateString === 'string' ? new Date(dateString) : dateString;
  return date.toLocaleString();
}

/**
 * Format a date as ISO string (for API requests)
 */
export function toISOString(dateString: string | Date): string {
  const date = typeof dateString === 'string' ? new Date(dateString) : dateString;
  return date.toISOString();
}

/**
 * Format uptime duration (seconds to human readable)
 */
export function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;

  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

/**
 * Format a duration in minutes to human readable
 */
export function formatDuration(minutes: number): string {
  if (minutes < 60) return `${minutes}m`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;

  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

// ============================================================================
// Status Color Utilities
// ============================================================================

/**
 * Color values that work across web and mobile
 * Returns semantic color names that can be mapped to actual values
 */
export type SemanticColor =
  | 'success'    // Green - online, resolved
  | 'warning'    // Yellow/Orange - warning, in_progress
  | 'danger'     // Red - critical, urgent
  | 'info'       // Blue - info, open
  | 'neutral'    // Gray - offline, disabled, closed
  | 'purple';    // Purple - acknowledged

/**
 * Get semantic color for device status
 */
export function getDeviceStatusColor(status: string): SemanticColor {
  switch (status) {
    case DEVICE_STATUS.ONLINE:
      return 'success';
    case DEVICE_STATUS.OFFLINE:
      return 'neutral';
    case DEVICE_STATUS.WARNING:
      return 'warning';
    case DEVICE_STATUS.CRITICAL:
      return 'danger';
    case DEVICE_STATUS.DISABLED:
    case DEVICE_STATUS.UNINSTALLING:
      return 'neutral';
    default:
      return 'neutral';
  }
}

/**
 * Get semantic color for alert severity
 */
export function getAlertSeverityColor(severity: string): SemanticColor {
  switch (severity) {
    case ALERT_SEVERITY.INFO:
      return 'info';
    case ALERT_SEVERITY.WARNING:
      return 'warning';
    case ALERT_SEVERITY.CRITICAL:
      return 'danger';
    default:
      return 'info';
  }
}

/**
 * Get semantic color for alert status
 */
export function getAlertStatusColor(status: string): SemanticColor {
  switch (status) {
    case ALERT_STATUS.OPEN:
      return 'danger';
    case ALERT_STATUS.ACKNOWLEDGED:
      return 'purple';
    case ALERT_STATUS.RESOLVED:
      return 'success';
    default:
      return 'neutral';
  }
}

/**
 * Get semantic color for ticket status
 */
export function getTicketStatusColor(status: string): SemanticColor {
  switch (status) {
    case TICKET_STATUS.OPEN:
      return 'info';
    case TICKET_STATUS.IN_PROGRESS:
      return 'purple';
    case TICKET_STATUS.WAITING:
      return 'warning';
    case TICKET_STATUS.RESOLVED:
      return 'success';
    case TICKET_STATUS.CLOSED:
      return 'neutral';
    default:
      return 'info';
  }
}

/**
 * Get semantic color for ticket priority
 */
export function getTicketPriorityColor(priority: string): SemanticColor {
  switch (priority) {
    case TICKET_PRIORITY.LOW:
      return 'neutral';
    case TICKET_PRIORITY.MEDIUM:
      return 'warning';
    case TICKET_PRIORITY.HIGH:
      return 'warning';
    case TICKET_PRIORITY.URGENT:
      return 'danger';
    default:
      return 'neutral';
  }
}

// ============================================================================
// Number Formatting Utilities
// ============================================================================

/**
 * Format bytes to human readable size
 */
export function formatBytes(bytes: number, decimals: number = 1): string {
  if (bytes === 0) return '0 B';

  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
}

/**
 * Format bytes per second to human readable
 */
export function formatBytesPerSecond(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`;
}

/**
 * Format percentage with decimals
 */
export function formatPercent(value: number, decimals: number = 1): string {
  return `${value.toFixed(decimals)}%`;
}

/**
 * Format a large number with K/M/B suffixes
 */
export function formatNumber(num: number): string {
  if (num < 1000) return num.toString();
  if (num < 1000000) return `${(num / 1000).toFixed(1)}K`;
  if (num < 1000000000) return `${(num / 1000000).toFixed(1)}M`;
  return `${(num / 1000000000).toFixed(1)}B`;
}

// ============================================================================
// String Utilities
// ============================================================================

/**
 * Truncate a string with ellipsis
 */
export function truncate(str: string, maxLength: number): string {
  if (str.length <= maxLength) return str;
  return str.slice(0, maxLength - 3) + '...';
}

/**
 * Capitalize first letter
 */
export function capitalize(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

/**
 * Convert snake_case to Title Case
 */
export function snakeToTitleCase(str: string): string {
  return str
    .split('_')
    .map(word => capitalize(word))
    .join(' ');
}

/**
 * Get initials from a name
 */
export function getInitials(name: string, maxLength: number = 2): string {
  return name
    .split(' ')
    .map(word => word.charAt(0).toUpperCase())
    .slice(0, maxLength)
    .join('');
}

// ============================================================================
// Validation Utilities
// ============================================================================

/**
 * Simple email validation
 */
export function isValidEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return emailRegex.test(email);
}

/**
 * Check if a string is a valid UUID
 */
export function isValidUUID(str: string): boolean {
  const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  return uuidRegex.test(str);
}

// ============================================================================
// Metric Threshold Utilities
// ============================================================================

/**
 * Get status based on metric value and thresholds
 */
export function getMetricStatus(
  value: number,
  warningThreshold: number,
  criticalThreshold: number
): 'normal' | 'warning' | 'critical' {
  if (value >= criticalThreshold) return 'critical';
  if (value >= warningThreshold) return 'warning';
  return 'normal';
}

/**
 * Get color based on metric value and thresholds
 */
export function getMetricColor(
  value: number,
  warningThreshold: number,
  criticalThreshold: number
): SemanticColor {
  const status = getMetricStatus(value, warningThreshold, criticalThreshold);
  switch (status) {
    case 'critical':
      return 'danger';
    case 'warning':
      return 'warning';
    default:
      return 'success';
  }
}

// ============================================================================
// Array Utilities
// ============================================================================

/**
 * Group an array by a key
 */
export function groupBy<T>(array: T[], key: keyof T): Record<string, T[]> {
  return array.reduce((result, item) => {
    const groupKey = String(item[key]);
    if (!result[groupKey]) {
      result[groupKey] = [];
    }
    result[groupKey].push(item);
    return result;
  }, {} as Record<string, T[]>);
}

/**
 * Sort an array by a key
 */
export function sortBy<T>(array: T[], key: keyof T, order: 'asc' | 'desc' = 'asc'): T[] {
  return [...array].sort((a, b) => {
    const aVal = a[key];
    const bVal = b[key];

    if (aVal < bVal) return order === 'asc' ? -1 : 1;
    if (aVal > bVal) return order === 'asc' ? 1 : -1;
    return 0;
  });
}

/**
 * Remove duplicates from an array by a key
 */
export function uniqueBy<T>(array: T[], key: keyof T): T[] {
  const seen = new Set();
  return array.filter(item => {
    const val = item[key];
    if (seen.has(val)) return false;
    seen.add(val);
    return true;
  });
}
