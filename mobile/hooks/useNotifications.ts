/**
 * useNotifications Hook
 * Sets up push notification listeners and handles navigation on notification tap
 */
import { useEffect, useRef, useCallback, useState } from 'react';
import * as Notifications from 'expo-notifications';
import { useRouter } from 'expo-router';
import { AppState, AppStateStatus } from 'react-native';
import { notificationService } from '../services/notifications';
import { useAuthStore } from '../stores/authStore';
import { NotificationData, NotificationCategories } from '../constants/notifications';

// ============================================
// Types
// ============================================

export interface NotificationPayload {
  title: string;
  body: string;
  data: NotificationData;
}

export interface UseNotificationsOptions {
  /**
   * Whether to auto-register for push notifications
   * Default: true (when authenticated)
   */
  autoRegister?: boolean;

  /**
   * Callback when notification is received in foreground
   */
  onNotificationReceived?: (payload: NotificationPayload) => void;

  /**
   * Callback when user taps a notification
   */
  onNotificationTapped?: (payload: NotificationPayload) => void;

  /**
   * Whether to enable in-app notification banner
   * Default: true
   */
  enableInAppBanner?: boolean;
}

export interface UseNotificationsReturn {
  /**
   * Whether push notifications are enabled
   */
  isEnabled: boolean;

  /**
   * Whether registration is in progress
   */
  isRegistering: boolean;

  /**
   * The current push token
   */
  pushToken: string | null;

  /**
   * Current badge count
   */
  badgeCount: number;

  /**
   * Latest received notification (for in-app banner)
   */
  latestNotification: NotificationPayload | null;

  /**
   * Manually register for push notifications
   */
  register: () => Promise<boolean>;

  /**
   * Clear the badge count
   */
  clearBadge: () => Promise<void>;

  /**
   * Dismiss the in-app notification banner
   */
  dismissBanner: () => void;

  /**
   * Request permission (without registering token with server)
   */
  requestPermission: () => Promise<boolean>;
}

// ============================================
// Hook Implementation
// ============================================

export function useNotifications(options: UseNotificationsOptions = {}): UseNotificationsReturn {
  const {
    autoRegister = true,
    onNotificationReceived,
    onNotificationTapped,
    enableInAppBanner = true,
  } = options;

  const router = useRouter();
  const { isAuthenticated } = useAuthStore();

  // State
  const [isEnabled, setIsEnabled] = useState(false);
  const [isRegistering, setIsRegistering] = useState(false);
  const [pushToken, setPushToken] = useState<string | null>(null);
  const [badgeCount, setBadgeCount] = useState(0);
  const [latestNotification, setLatestNotification] = useState<NotificationPayload | null>(null);

  // Refs for listeners
  const notificationListener = useRef<Notifications.Subscription | null>(null);
  const responseListener = useRef<Notifications.Subscription | null>(null);
  const appStateRef = useRef<AppStateStatus>(AppState.currentState);

  /**
   * Initialize notification service
   */
  useEffect(() => {
    notificationService.initialize();
  }, []);

  /**
   * Check if notifications are enabled
   */
  const checkEnabled = useCallback(async () => {
    const enabled = await notificationService.areNotificationsEnabled();
    setIsEnabled(enabled);
    return enabled;
  }, []);

  /**
   * Request notification permission
   */
  const requestPermission = useCallback(async (): Promise<boolean> => {
    const token = await notificationService.registerForPushNotifications();
    const enabled = token !== null;
    setIsEnabled(enabled);
    if (token) {
      setPushToken(token);
    }
    return enabled;
  }, []);

  /**
   * Register for push notifications and send token to server
   */
  const register = useCallback(async (): Promise<boolean> => {
    if (isRegistering) {
      console.log('[useNotifications] Registration already in progress');
      return false;
    }

    setIsRegistering(true);

    try {
      // Get push token
      const token = await notificationService.registerForPushNotifications();

      if (!token) {
        console.log('[useNotifications] Failed to get push token');
        setIsEnabled(false);
        return false;
      }

      setPushToken(token);
      setIsEnabled(true);

      // Send to server
      const success = await notificationService.sendTokenToServer(token);

      if (!success) {
        console.warn('[useNotifications] Failed to register token with server');
        // Still return true since we have the token locally
      }

      return true;
    } catch (error) {
      console.error('[useNotifications] Registration failed:', error);
      return false;
    } finally {
      setIsRegistering(false);
    }
  }, [isRegistering]);

  /**
   * Clear the badge count
   */
  const clearBadge = useCallback(async () => {
    await notificationService.clearBadgeCount();
    setBadgeCount(0);
  }, []);

  /**
   * Dismiss the in-app notification banner
   */
  const dismissBanner = useCallback(() => {
    setLatestNotification(null);
  }, []);

  /**
   * Handle navigation from notification data
   */
  const handleNavigate = useCallback((data: NotificationData) => {
    if (!data?.type) {
      console.log('[useNotifications] No notification type, cannot navigate');
      return;
    }

    // Determine navigation based on notification type
    switch (data.type) {
      case NotificationCategories.ALERT:
        if (data.alertId) {
          console.log('[useNotifications] Navigating to alert:', data.alertId);
          router.push(`/alert/${data.alertId}` as never);
        }
        break;

      case NotificationCategories.TICKET:
        if (data.ticketId) {
          console.log('[useNotifications] Navigating to ticket:', data.ticketId);
          router.push(`/ticket/${data.ticketId}` as never);
        }
        break;

      case NotificationCategories.DEVICE_STATUS:
        if (data.deviceId) {
          console.log('[useNotifications] Navigating to device:', data.deviceId);
          router.push(`/device/${data.deviceId}` as never);
        }
        break;

      default:
        console.log('[useNotifications] Unknown notification type:', data.type);
    }
  }, [router]);

  /**
   * Auto-register when authenticated
   */
  useEffect(() => {
    if (isAuthenticated && autoRegister) {
      console.log('[useNotifications] User authenticated, registering for notifications...');
      register();
    }
  }, [isAuthenticated, autoRegister, register]);

  /**
   * Set up notification listeners
   */
  useEffect(() => {
    console.log('[useNotifications] Setting up notification listeners...');

    // Listener for notifications received while app is in foreground
    notificationListener.current = Notifications.addNotificationReceivedListener(
      (notification) => {
        const { title, body, data } = notification.request.content;
        const notificationData = data as NotificationData;

        const payload: NotificationPayload = {
          title: title || 'Sentinel',
          body: body || '',
          data: notificationData,
        };

        console.log('[useNotifications] Notification received:', payload.title);

        // Update badge count
        notificationService.getBadgeCount().then(setBadgeCount);

        // Show in-app banner if enabled
        if (enableInAppBanner) {
          setLatestNotification(payload);

          // Auto-dismiss after 5 seconds
          setTimeout(() => {
            setLatestNotification((current) =>
              current?.data === payload.data ? null : current
            );
          }, 5000);
        }

        // Call custom handler
        onNotificationReceived?.(payload);
      }
    );

    // Listener for notification taps (user interaction)
    responseListener.current = Notifications.addNotificationResponseReceivedListener(
      (response) => {
        const { title, body, data } = response.notification.request.content;
        const notificationData = data as NotificationData;

        const payload: NotificationPayload = {
          title: title || 'Sentinel',
          body: body || '',
          data: notificationData,
        };

        console.log('[useNotifications] Notification tapped:', payload.title);

        // Clear badge when user taps notification
        clearBadge();

        // Navigate to appropriate screen
        if (notificationData) {
          handleNavigate(notificationData);
        }

        // Call custom handler
        onNotificationTapped?.(payload);
      }
    );

    // Check initial permission status
    checkEnabled();

    // Get initial badge count
    notificationService.getBadgeCount().then(setBadgeCount);

    // Cleanup listeners on unmount
    return () => {
      console.log('[useNotifications] Cleaning up notification listeners...');

      if (notificationListener.current) {
        Notifications.removeNotificationSubscription(notificationListener.current);
      }
      if (responseListener.current) {
        Notifications.removeNotificationSubscription(responseListener.current);
      }
    };
  }, [checkEnabled, clearBadge, enableInAppBanner, handleNavigate, onNotificationReceived, onNotificationTapped]);

  /**
   * Handle app state changes (foreground/background)
   * Update badge count when returning to foreground
   */
  useEffect(() => {
    const subscription = AppState.addEventListener('change', (nextAppState) => {
      if (
        appStateRef.current.match(/inactive|background/) &&
        nextAppState === 'active'
      ) {
        // App has come to foreground
        console.log('[useNotifications] App came to foreground, updating badge count');
        notificationService.getBadgeCount().then(setBadgeCount);
      }
      appStateRef.current = nextAppState;
    });

    return () => {
      subscription.remove();
    };
  }, []);

  /**
   * Handle notification that launched the app (cold start)
   */
  useEffect(() => {
    async function handleInitialNotification() {
      // Get the notification that opened the app (if any)
      const response = await Notifications.getLastNotificationResponseAsync();

      if (response) {
        console.log('[useNotifications] App opened from notification');
        const data = response.notification.request.content.data as NotificationData;

        if (data) {
          // Small delay to ensure navigation is ready
          setTimeout(() => {
            handleNavigate(data);
          }, 500);
        }
      }
    }

    if (isAuthenticated) {
      handleInitialNotification();
    }
  }, [isAuthenticated, handleNavigate]);

  return {
    isEnabled,
    isRegistering,
    pushToken,
    badgeCount,
    latestNotification,
    register,
    clearBadge,
    dismissBanner,
    requestPermission,
  };
}

export default useNotifications;
