/**
 * Stores Index - Export all Zustand stores from a single entry point
 */
export { useAuthStore } from './authStore';
export type { User } from './authStore';

export { useAlertStore } from './alertStore';
export type { Alert, AlertSeverity, AlertStatus, AlertFilters } from './alertStore';

export { useDashboardStore } from './dashboardStore';
export type { DashboardStats } from './dashboardStore';

export { useDeviceStore } from './deviceStore';

export { useTicketStore } from './ticketStore';
export type { Ticket, TicketComment, TicketFilters, TicketStats } from './ticketStore';
