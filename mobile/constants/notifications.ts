/**
 * Notification Constants for Sentinel Mobile
 * Defines channels, categories, and sound configurations
 */
import { AndroidImportance, AndroidNotificationVisibility } from 'expo-notifications';

// ============================================
// Channel Configuration Type
// ============================================

export interface NotificationChannelConfig {
  id: string;
  name: string;
  description: string;
  importance: AndroidImportance;
  sound: string | boolean;
  vibrationPattern: number[];
  enableLights: boolean;
  lightColor: string;
  lockscreenVisibility: AndroidNotificationVisibility;
  bypassDnd: boolean;
}

// ============================================
// Android Notification Channels
// ============================================

export const NotificationChannels: Record<string, NotificationChannelConfig> = {
  /**
   * Critical alerts channel - highest priority
   * Used for critical device alerts that need immediate attention
   */
  CRITICAL_ALERTS: {
    id: 'sentinel-critical-alerts',
    name: 'Critical Alerts',
    description: 'Critical device and system alerts requiring immediate attention',
    importance: AndroidImportance.MAX,
    sound: 'notification.wav',
    vibrationPattern: [0, 500, 200, 500], // Urgent double vibration
    enableLights: true,
    lightColor: '#ef4444',
    lockscreenVisibility: AndroidNotificationVisibility.PUBLIC,
    bypassDnd: true,
  },

  /**
   * High priority alerts
   * Used for high severity alerts
   */
  HIGH_ALERTS: {
    id: 'sentinel-high-alerts',
    name: 'High Priority Alerts',
    description: 'High priority device alerts',
    importance: AndroidImportance.HIGH,
    sound: 'notification.wav',
    vibrationPattern: [0, 400, 200, 400],
    enableLights: true,
    lightColor: '#f97316',
    lockscreenVisibility: AndroidNotificationVisibility.PUBLIC,
    bypassDnd: false,
  },

  /**
   * Standard alerts channel
   * Used for medium and low priority alerts
   */
  ALERTS: {
    id: 'sentinel-alerts',
    name: 'Alerts',
    description: 'Device and system alerts',
    importance: AndroidImportance.DEFAULT,
    sound: 'notification.wav',
    vibrationPattern: [0, 250],
    enableLights: true,
    lightColor: '#f59e0b',
    lockscreenVisibility: AndroidNotificationVisibility.PUBLIC,
    bypassDnd: false,
  },

  /**
   * Ticket updates channel
   * Used for ticket status changes and comments
   */
  TICKETS: {
    id: 'sentinel-tickets',
    name: 'Ticket Updates',
    description: 'Ticket status changes and comment notifications',
    importance: AndroidImportance.DEFAULT,
    sound: 'notification.wav',
    vibrationPattern: [0, 200],
    enableLights: true,
    lightColor: '#3b82f6',
    lockscreenVisibility: AndroidNotificationVisibility.PRIVATE,
    bypassDnd: false,
  },

  /**
   * Device status channel
   * Used for device online/offline notifications
   */
  DEVICE_STATUS: {
    id: 'sentinel-device-status',
    name: 'Device Status',
    description: 'Device online and offline notifications',
    importance: AndroidImportance.LOW,
    sound: false,
    vibrationPattern: [],
    enableLights: false,
    lightColor: '#6b7280',
    lockscreenVisibility: AndroidNotificationVisibility.PRIVATE,
    bypassDnd: false,
  },

  /**
   * General channel
   * Used for misc notifications
   */
  GENERAL: {
    id: 'sentinel-general',
    name: 'General',
    description: 'General app notifications',
    importance: AndroidImportance.LOW,
    sound: false,
    vibrationPattern: [],
    enableLights: false,
    lightColor: '#6b7280',
    lockscreenVisibility: AndroidNotificationVisibility.PRIVATE,
    bypassDnd: false,
  },
};

// ============================================
// Notification Categories
// ============================================

export const NotificationCategories = {
  ALERT: 'alert',
  TICKET: 'ticket',
  DEVICE_STATUS: 'device_status',
  GENERAL: 'general',
} as const;

export type NotificationCategory = typeof NotificationCategories[keyof typeof NotificationCategories];

// ============================================
// Alert Severity Levels
// ============================================

export const AlertSeverity = {
  CRITICAL: 'critical',
  HIGH: 'high',
  MEDIUM: 'medium',
  LOW: 'low',
} as const;

export type AlertSeverityType = typeof AlertSeverity[keyof typeof AlertSeverity];

// ============================================
// Notification Type Definitions
// ============================================

export interface NotificationData {
  type: NotificationCategory;
  alertId?: string;
  deviceId?: string;
  ticketId?: string;
  severity?: AlertSeverityType;
  [key: string]: unknown;
}

export interface AlertNotificationData extends NotificationData {
  type: typeof NotificationCategories.ALERT;
  alertId: string;
  deviceId: string;
  severity: AlertSeverityType;
}

export interface TicketNotificationData extends NotificationData {
  type: typeof NotificationCategories.TICKET;
  ticketId: string;
  action?: 'created' | 'updated' | 'comment' | 'assigned';
}

export interface DeviceStatusNotificationData extends NotificationData {
  type: typeof NotificationCategories.DEVICE_STATUS;
  deviceId: string;
  status: 'online' | 'offline';
}

// ============================================
// Sound Configurations
// ============================================

export const NotificationSounds = {
  DEFAULT: 'notification.wav',
  CRITICAL: 'notification.wav', // Can be customized to different sound
  SILENT: null,
} as const;

// ============================================
// Helper Functions
// ============================================

/**
 * Get the appropriate notification channel for an alert severity
 */
export function getChannelForSeverity(severity: AlertSeverityType): NotificationChannelConfig {
  switch (severity) {
    case AlertSeverity.CRITICAL:
      return NotificationChannels.CRITICAL_ALERTS;
    case AlertSeverity.HIGH:
      return NotificationChannels.HIGH_ALERTS;
    case AlertSeverity.MEDIUM:
    case AlertSeverity.LOW:
    default:
      return NotificationChannels.ALERTS;
  }
}

/**
 * Get the appropriate notification channel for a notification type
 */
export function getChannelForType(type: NotificationCategory, severity?: AlertSeverityType): NotificationChannelConfig {
  switch (type) {
    case NotificationCategories.ALERT:
      return severity ? getChannelForSeverity(severity) : NotificationChannels.ALERTS;
    case NotificationCategories.TICKET:
      return NotificationChannels.TICKETS;
    case NotificationCategories.DEVICE_STATUS:
      return NotificationChannels.DEVICE_STATUS;
    default:
      return NotificationChannels.GENERAL;
  }
}

// ============================================
// Storage Keys
// ============================================

export const NotificationStorageKeys = {
  PUSH_TOKEN: 'sentinel_push_token',
  NOTIFICATION_PREFERENCES: 'sentinel_notification_prefs',
  LAST_NOTIFICATION_ID: 'sentinel_last_notification_id',
} as const;

// ============================================
// API Endpoints
// ============================================

export const NotificationEndpoints = {
  REGISTER_TOKEN: '/mobile/push/register',
  UNREGISTER_TOKEN: '/mobile/push/unregister',
} as const;
