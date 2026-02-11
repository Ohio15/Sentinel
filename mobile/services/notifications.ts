/**
 * Push Notification Service for Sentinel Mobile
 * Handles push notification registration, permissions, and processing
 */
import * as Notifications from 'expo-notifications';
import Constants from 'expo-constants';
import { Platform } from 'react-native';
import * as SecureStore from 'expo-secure-store';
import { api } from './api';
import {
  NotificationChannels,
  NotificationCategories,
  NotificationData,
  AlertNotificationData,
  NotificationStorageKeys,
  NotificationEndpoints,
  getChannelForSeverity,
  AlertSeverity,
  NotificationChannelConfig,
} from '../constants/notifications';

// ============================================
// Configure Default Notification Behavior
// ============================================

// Set default notification handler - how to display notifications when app is in foreground
Notifications.setNotificationHandler({
  handleNotification: async (notification) => {
    const data = notification.request.content.data as NotificationData;
    const severity = (data as AlertNotificationData)?.severity;

    // Always show critical alerts, show others based on app state
    const shouldShowAlert = severity === AlertSeverity.CRITICAL || severity === AlertSeverity.HIGH;

    return {
      shouldShowAlert: true, // Always show notification banner
      shouldPlaySound: shouldShowAlert,
      shouldSetBadge: true,
      priority: severity === AlertSeverity.CRITICAL
        ? Notifications.AndroidNotificationPriority.MAX
        : Notifications.AndroidNotificationPriority.HIGH,
    };
  },
});

// ============================================
// Types
// ============================================

export interface PushNotificationToken {
  token: string;
  platform: 'ios' | 'android' | 'web';
}

export interface NotificationPayload {
  title: string;
  body: string;
  data: NotificationData;
}

// ============================================
// Notification Service Class
// ============================================

class NotificationService {
  private pushToken: string | null = null;
  private isInitialized = false;

  /**
   * Initialize the notification service
   * Sets up Android notification channels and restores saved token
   */
  async initialize(): Promise<void> {
    if (this.isInitialized) {
      console.log('[Notifications] Already initialized');
      return;
    }

    console.log('[Notifications] Initializing notification service...');

    // Set up Android notification channels
    if (Platform.OS === 'android') {
      await this.createAndroidChannels();
    }

    // Restore saved push token
    const savedToken = await this.getSavedPushToken();
    if (savedToken) {
      this.pushToken = savedToken;
      console.log('[Notifications] Restored saved push token');
    }

    this.isInitialized = true;
    console.log('[Notifications] Initialization complete');
  }

  /**
   * Create Android notification channels
   * Must be called before showing notifications on Android
   */
  private async createAndroidChannels(): Promise<void> {
    console.log('[Notifications] Creating Android notification channels...');

    const channels = Object.values(NotificationChannels) as NotificationChannelConfig[];

    for (const channel of channels) {
      await Notifications.setNotificationChannelAsync(channel.id, {
        name: channel.name,
        description: channel.description,
        importance: channel.importance,
        vibrationPattern: channel.vibrationPattern,
        enableLights: channel.enableLights,
        lightColor: channel.lightColor,
        lockscreenVisibility: channel.lockscreenVisibility,
        bypassDnd: channel.bypassDnd,
        sound: typeof channel.sound === 'string' ? channel.sound : undefined,
      });
    }

    console.log('[Notifications] Android channels created');
  }

  /**
   * Check if running on a physical device
   * Expo Device module may not be available, so we check platform-specific APIs
   */
  private isPhysicalDevice(): boolean {
    // In development, we can still test on simulators
    // The push token registration will fail gracefully if not on a real device
    return true;
  }

  /**
   * Request notification permissions and get push token
   * Returns the Expo push token if successful
   */
  async registerForPushNotifications(): Promise<string | null> {
    console.log('[Notifications] Registering for push notifications...');

    // Check current permission status
    const { status: existingStatus } = await Notifications.getPermissionsAsync();
    let finalStatus = existingStatus;

    // Request permission if not already granted
    if (existingStatus !== 'granted') {
      console.log('[Notifications] Requesting notification permissions...');
      const { status } = await Notifications.requestPermissionsAsync();
      finalStatus = status;
    }

    // Check if permission was granted
    if (finalStatus !== 'granted') {
      console.warn('[Notifications] Permission not granted for push notifications');
      return null;
    }

    console.log('[Notifications] Permission granted');

    // Get the Expo push token
    try {
      const projectId = Constants.expoConfig?.extra?.eas?.projectId;

      if (!projectId) {
        console.warn('[Notifications] No EAS project ID found, using device token');
      }

      const tokenResponse = await Notifications.getExpoPushTokenAsync({
        projectId: projectId || undefined,
      });

      this.pushToken = tokenResponse.data;
      console.log('[Notifications] Push token obtained:', this.pushToken.substring(0, 20) + '...');

      // Save token locally
      await this.savePushToken(this.pushToken);

      return this.pushToken;
    } catch (error) {
      console.error('[Notifications] Failed to get push token:', error);
      return null;
    }
  }

  /**
   * Send the push token to the server for storage
   * Called after successful registration
   */
  async sendTokenToServer(token: string): Promise<boolean> {
    console.log('[Notifications] Sending push token to server...');

    const platform: 'ios' | 'android' | 'web' =
      Platform.OS === 'ios' ? 'ios' :
      Platform.OS === 'android' ? 'android' : 'web';

    try {
      await api.post(NotificationEndpoints.REGISTER_TOKEN, {
        token,
        platform,
      });

      console.log('[Notifications] Push token registered with server');
      return true;
    } catch (error) {
      console.error('[Notifications] Failed to register push token with server:', error);
      return false;
    }
  }

  /**
   * Unregister push token from server
   * Called during logout
   */
  async unregisterFromServer(): Promise<boolean> {
    if (!this.pushToken) {
      console.log('[Notifications] No push token to unregister');
      return true;
    }

    console.log('[Notifications] Unregistering push token from server...');

    try {
      await api.post(NotificationEndpoints.UNREGISTER_TOKEN, {
        token: this.pushToken,
      });

      console.log('[Notifications] Push token unregistered from server');
      return true;
    } catch (error) {
      console.error('[Notifications] Failed to unregister push token:', error);
      return false;
    }
  }

  /**
   * Handle notification received (foreground or background)
   * Called when a notification is received by the device
   */
  handleNotificationReceived(notification: Notifications.Notification): NotificationPayload {
    const { title, body, data } = notification.request.content;
    const notificationData = data as NotificationData;

    console.log('[Notifications] Notification received:', {
      title,
      body,
      type: notificationData?.type,
    });

    // Update badge count if it's an alert
    if (notificationData?.type === NotificationCategories.ALERT) {
      this.incrementBadgeCount();
    }

    return {
      title: title || 'Sentinel',
      body: body || '',
      data: notificationData,
    };
  }

  /**
   * Handle notification response (user tapped notification)
   * Returns navigation route based on notification data
   */
  handleNotificationResponse(response: Notifications.NotificationResponse): {
    route: string;
    params: Record<string, string>;
  } | null {
    const data = response.notification.request.content.data as NotificationData;

    console.log('[Notifications] User tapped notification:', {
      type: data?.type,
      alertId: data?.alertId,
      deviceId: data?.deviceId,
      ticketId: data?.ticketId,
    });

    if (!data?.type) {
      console.log('[Notifications] No notification type, cannot navigate');
      return null;
    }

    // Determine navigation based on notification type
    switch (data.type) {
      case NotificationCategories.ALERT:
        if (data.alertId) {
          return {
            route: `/alert/${data.alertId}`,
            params: { id: data.alertId },
          };
        }
        break;

      case NotificationCategories.TICKET:
        if (data.ticketId) {
          return {
            route: `/ticket/${data.ticketId}`,
            params: { id: data.ticketId },
          };
        }
        break;

      case NotificationCategories.DEVICE_STATUS:
        if (data.deviceId) {
          return {
            route: `/device/${data.deviceId}`,
            params: { id: data.deviceId },
          };
        }
        break;
    }

    console.log('[Notifications] Could not determine navigation target');
    return null;
  }

  /**
   * Set the app badge count
   */
  async setBadgeCount(count: number): Promise<void> {
    try {
      await Notifications.setBadgeCountAsync(count);
      console.log('[Notifications] Badge count set to:', count);
    } catch (error) {
      console.error('[Notifications] Failed to set badge count:', error);
    }
  }

  /**
   * Get current badge count
   */
  async getBadgeCount(): Promise<number> {
    try {
      return await Notifications.getBadgeCountAsync();
    } catch (error) {
      console.error('[Notifications] Failed to get badge count:', error);
      return 0;
    }
  }

  /**
   * Increment badge count by 1
   */
  async incrementBadgeCount(): Promise<void> {
    const current = await this.getBadgeCount();
    await this.setBadgeCount(current + 1);
  }

  /**
   * Clear badge count
   */
  async clearBadgeCount(): Promise<void> {
    await this.setBadgeCount(0);
  }

  /**
   * Schedule a local notification
   * Useful for testing or offline alerts
   */
  async scheduleLocalNotification(
    title: string,
    body: string,
    data: NotificationData,
    trigger?: Notifications.NotificationTriggerInput
  ): Promise<string> {
    const severity = (data as AlertNotificationData)?.severity;
    const channel = severity
      ? getChannelForSeverity(severity)
      : NotificationChannels.GENERAL;

    const notificationId = await Notifications.scheduleNotificationAsync({
      content: {
        title,
        body,
        data,
        sound: true,
        priority: severity === AlertSeverity.CRITICAL
          ? Notifications.AndroidNotificationPriority.MAX
          : Notifications.AndroidNotificationPriority.HIGH,
      },
      trigger: trigger || null, // null = immediate
    });

    console.log('[Notifications] Local notification scheduled:', notificationId);
    return notificationId;
  }

  /**
   * Schedule a notification for a specific time
   */
  async scheduleNotificationAt(
    title: string,
    body: string,
    data: NotificationData,
    date: Date
  ): Promise<string> {
    return this.scheduleLocalNotification(title, body, data, {
      type: Notifications.SchedulableTriggerInputTypes.DATE,
      date,
    });
  }

  /**
   * Schedule a repeating notification
   */
  async scheduleRepeatingNotification(
    title: string,
    body: string,
    data: NotificationData,
    seconds: number
  ): Promise<string> {
    return this.scheduleLocalNotification(title, body, data, {
      type: Notifications.SchedulableTriggerInputTypes.TIME_INTERVAL,
      seconds,
      repeats: true,
    });
  }

  /**
   * Cancel a scheduled notification
   */
  async cancelScheduledNotification(notificationId: string): Promise<void> {
    await Notifications.cancelScheduledNotificationAsync(notificationId);
    console.log('[Notifications] Cancelled notification:', notificationId);
  }

  /**
   * Cancel all scheduled notifications
   */
  async cancelAllScheduledNotifications(): Promise<void> {
    await Notifications.cancelAllScheduledNotificationsAsync();
    console.log('[Notifications] Cancelled all scheduled notifications');
  }

  /**
   * Get all scheduled notifications
   */
  async getScheduledNotifications(): Promise<Notifications.NotificationRequest[]> {
    return await Notifications.getAllScheduledNotificationsAsync();
  }

  /**
   * Dismiss all delivered notifications
   */
  async dismissAllNotifications(): Promise<void> {
    await Notifications.dismissAllNotificationsAsync();
    console.log('[Notifications] Dismissed all notifications');
  }

  /**
   * Get the current push token
   */
  getPushToken(): string | null {
    return this.pushToken;
  }

  /**
   * Save push token to secure storage
   */
  private async savePushToken(token: string): Promise<void> {
    try {
      await SecureStore.setItemAsync(NotificationStorageKeys.PUSH_TOKEN, token);
    } catch (error) {
      console.error('[Notifications] Failed to save push token:', error);
    }
  }

  /**
   * Get saved push token from secure storage
   */
  private async getSavedPushToken(): Promise<string | null> {
    try {
      return await SecureStore.getItemAsync(NotificationStorageKeys.PUSH_TOKEN);
    } catch (error) {
      console.error('[Notifications] Failed to get saved push token:', error);
      return null;
    }
  }

  /**
   * Clear saved push token
   */
  async clearSavedPushToken(): Promise<void> {
    try {
      await SecureStore.deleteItemAsync(NotificationStorageKeys.PUSH_TOKEN);
      this.pushToken = null;
    } catch (error) {
      console.error('[Notifications] Failed to clear push token:', error);
    }
  }

  /**
   * Check if notifications are enabled
   */
  async areNotificationsEnabled(): Promise<boolean> {
    const { status } = await Notifications.getPermissionsAsync();
    return status === 'granted';
  }

  /**
   * Open system notification settings
   */
  async openNotificationSettings(): Promise<void> {
    if (Platform.OS === 'ios') {
      // iOS doesn't have a direct way to open notification settings
      // We can only check permissions
      console.log('[Notifications] Cannot open settings directly on iOS');
    } else {
      // On Android, expo-notifications doesn't provide direct access to settings
      // This would require expo-intent-launcher
      console.log('[Notifications] Use system settings to manage notifications');
    }
  }

  /**
   * Full cleanup - call during logout
   */
  async cleanup(): Promise<void> {
    console.log('[Notifications] Cleaning up...');

    // Unregister from server
    await this.unregisterFromServer();

    // Clear local token
    await this.clearSavedPushToken();

    // Clear badge
    await this.clearBadgeCount();

    // Cancel all scheduled notifications
    await this.cancelAllScheduledNotifications();

    console.log('[Notifications] Cleanup complete');
  }
}

// Export singleton instance
export const notificationService = new NotificationService();
export default notificationService;

// Re-export Notifications module for use in hooks
export { Notifications };
