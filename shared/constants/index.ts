/**
 * Shared Constants for Sentinel RMM
 * Used by both web and mobile applications
 *
 * Keep these framework-agnostic (no React, React Native, etc.)
 */

// ============================================================================
// Device Constants
// ============================================================================

export const DEVICE_STATUS = {
  ONLINE: 'online',
  OFFLINE: 'offline',
  WARNING: 'warning',
  CRITICAL: 'critical',
  DISABLED: 'disabled',
  UNINSTALLING: 'uninstalling',
} as const;

export const DEVICE_STATUS_VALUES = Object.values(DEVICE_STATUS);

export const DEVICE_TYPE = {
  DESKTOP: 'desktop',
  LAPTOP: 'laptop',
  SERVER: 'server',
  TABLET: 'tablet',
  VIRTUAL: 'virtual',
} as const;

export const DEVICE_TYPE_VALUES = Object.values(DEVICE_TYPE);

export const DEVICE_STATUS_LABELS: Record<string, string> = {
  online: 'Online',
  offline: 'Offline',
  warning: 'Warning',
  critical: 'Critical',
  disabled: 'Disabled',
  uninstalling: 'Uninstalling',
};

export const DEVICE_TYPE_LABELS: Record<string, string> = {
  desktop: 'Desktop',
  laptop: 'Laptop',
  server: 'Server',
  tablet: 'Tablet',
  virtual: 'Virtual Machine',
};

// ============================================================================
// Alert Constants
// ============================================================================

export const ALERT_SEVERITY = {
  INFO: 'info',
  WARNING: 'warning',
  CRITICAL: 'critical',
} as const;

export const ALERT_SEVERITY_VALUES = Object.values(ALERT_SEVERITY);

export const ALERT_STATUS = {
  OPEN: 'open',
  ACKNOWLEDGED: 'acknowledged',
  RESOLVED: 'resolved',
} as const;

export const ALERT_STATUS_VALUES = Object.values(ALERT_STATUS);

export const ALERT_OPERATOR = {
  GT: 'gt',
  LT: 'lt',
  EQ: 'eq',
  GTE: 'gte',
  LTE: 'lte',
} as const;

export const ALERT_OPERATOR_VALUES = Object.values(ALERT_OPERATOR);

export const ALERT_SEVERITY_LABELS: Record<string, string> = {
  info: 'Info',
  warning: 'Warning',
  critical: 'Critical',
};

export const ALERT_STATUS_LABELS: Record<string, string> = {
  open: 'Open',
  acknowledged: 'Acknowledged',
  resolved: 'Resolved',
};

export const ALERT_OPERATOR_LABELS: Record<string, string> = {
  gt: 'Greater than',
  lt: 'Less than',
  eq: 'Equal to',
  gte: 'Greater than or equal',
  lte: 'Less than or equal',
};

// ============================================================================
// Ticket Constants
// ============================================================================

export const TICKET_STATUS = {
  OPEN: 'open',
  IN_PROGRESS: 'in_progress',
  WAITING: 'waiting',
  RESOLVED: 'resolved',
  CLOSED: 'closed',
} as const;

export const TICKET_STATUS_VALUES = Object.values(TICKET_STATUS);

export const TICKET_PRIORITY = {
  LOW: 'low',
  MEDIUM: 'medium',
  HIGH: 'high',
  URGENT: 'urgent',
} as const;

export const TICKET_PRIORITY_VALUES = Object.values(TICKET_PRIORITY);

export const TICKET_TYPE = {
  INCIDENT: 'incident',
  REQUEST: 'request',
  PROBLEM: 'problem',
  CHANGE: 'change',
} as const;

export const TICKET_TYPE_VALUES = Object.values(TICKET_TYPE);

export const TICKET_STATUS_LABELS: Record<string, string> = {
  open: 'Open',
  in_progress: 'In Progress',
  waiting: 'Waiting',
  resolved: 'Resolved',
  closed: 'Closed',
};

export const TICKET_PRIORITY_LABELS: Record<string, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  urgent: 'Urgent',
};

export const TICKET_TYPE_LABELS: Record<string, string> = {
  incident: 'Incident',
  request: 'Service Request',
  problem: 'Problem',
  change: 'Change Request',
};

// Priority order for sorting (lower number = higher priority)
export const TICKET_PRIORITY_ORDER: Record<string, number> = {
  urgent: 1,
  high: 2,
  medium: 3,
  low: 4,
};

// ============================================================================
// User Role Constants
// ============================================================================

export const USER_ROLE = {
  ADMIN: 'admin',
  TECHNICIAN: 'technician',
  VIEWER: 'viewer',
  CLIENT: 'client',
} as const;

export const USER_ROLE_VALUES = Object.values(USER_ROLE);

export const USER_ROLE_LABELS: Record<string, string> = {
  admin: 'Administrator',
  technician: 'Technician',
  viewer: 'Viewer',
  client: 'Client',
};

// ============================================================================
// API Constants
// ============================================================================

export const DEFAULT_PAGE_SIZE = 25;
export const MAX_PAGE_SIZE = 100;

export const API_TIMEOUT_MS = 30000;
export const WEBSOCKET_RECONNECT_INTERVAL_MS = 5000;
export const TOKEN_REFRESH_BUFFER_SECONDS = 300; // Refresh 5 minutes before expiry

// ============================================================================
// Metrics Constants
// ============================================================================

export const METRIC_THRESHOLDS = {
  CPU_WARNING: 80,
  CPU_CRITICAL: 95,
  MEMORY_WARNING: 80,
  MEMORY_CRITICAL: 95,
  DISK_WARNING: 80,
  DISK_CRITICAL: 95,
} as const;

export const METRICS_POLL_INTERVAL_MS = 5000;
export const METRICS_HISTORY_LIMIT = 120; // Number of data points to keep

// ============================================================================
// Installation Link Constants
// ============================================================================

export const INSTALLATION_STATUS = {
  PENDING: 'pending',
  CLICKED: 'clicked',
  INSTALLED: 'installed',
  EXPIRED: 'expired',
  REVOKED: 'revoked',
} as const;

export const INSTALLATION_STATUS_VALUES = Object.values(INSTALLATION_STATUS);

export const INSTALLATION_STATUS_LABELS: Record<string, string> = {
  pending: 'Pending',
  clicked: 'Clicked',
  installed: 'Installed',
  expired: 'Expired',
  revoked: 'Revoked',
};

export const INSTALLATION_CODE_STATUS = {
  PENDING: 'pending',
  USED: 'used',
  EXPIRED: 'expired',
  REVOKED: 'revoked',
} as const;

export const INSTALLATION_CODE_STATUS_VALUES = Object.values(INSTALLATION_CODE_STATUS);

// ============================================================================
// Power Management Constants
// ============================================================================

export const POWER_ACTION = {
  SHUTDOWN: 'shutdown',
  RESTART: 'restart',
  WAKE: 'wake',
} as const;

export const POWER_ACTION_VALUES = Object.values(POWER_ACTION);

export const POWER_ACTION_LABELS: Record<string, string> = {
  shutdown: 'Shutdown',
  restart: 'Restart',
  wake: 'Wake on LAN',
};
