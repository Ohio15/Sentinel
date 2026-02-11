/**
 * App Configuration
 */
import Constants from 'expo-constants';

// API Configuration
export const API_BASE_URL =
  Constants.expoConfig?.extra?.apiUrl ||
  process.env.EXPO_PUBLIC_API_URL ||
  'https://sentinelrmm.us/api';

export const WS_BASE_URL =
  Constants.expoConfig?.extra?.wsUrl ||
  process.env.EXPO_PUBLIC_WS_URL ||
  'wss://sentinelrmm.us/ws';

// App Configuration
export const APP_NAME = 'Sentinel';
export const APP_VERSION = Constants.expoConfig?.version || '1.0.0';

// Timeouts
export const API_TIMEOUT = 30000; // 30 seconds
export const REFRESH_INTERVAL = 30000; // 30 seconds for data refresh

// Pagination
export const DEFAULT_PAGE_SIZE = 20;

// Notification Channels (Android)
export const NOTIFICATION_CHANNELS = {
  alerts: {
    id: 'sentinel-alerts',
    name: 'Alerts',
    description: 'Device and system alerts',
    importance: 5, // MAX
    sound: true,
    vibrate: true,
  },
  tickets: {
    id: 'sentinel-tickets',
    name: 'Tickets',
    description: 'Ticket updates and comments',
    importance: 3, // DEFAULT
    sound: true,
    vibrate: false,
  },
};

// Secure Storage Keys
export const STORAGE_KEYS = {
  AUTH_TOKEN: 'sentinel_auth_token',
  REFRESH_TOKEN: 'sentinel_refresh_token',
  USER_DATA: 'sentinel_user_data',
  PUSH_TOKEN: 'sentinel_push_token',
  SETTINGS: 'sentinel_settings',
};
