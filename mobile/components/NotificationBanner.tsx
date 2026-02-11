/**
 * NotificationBanner Component
 * Animated in-app notification banner that slides down from the top
 */
import React, { useEffect, useRef } from 'react';
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  Animated,
  Dimensions,
  Platform,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useRouter } from 'expo-router';
import { NotificationPayload } from '../services/notifications';
import {
  NotificationCategories,
  AlertNotificationData,
  AlertSeverity,
  NotificationData,
} from '../constants/notifications';
import { Colors, Spacing, BorderRadius, FontSizes } from '../constants/theme';

// ============================================
// Types
// ============================================

export interface NotificationBannerProps {
  /**
   * The notification to display
   */
  notification: NotificationPayload | null;

  /**
   * Callback when banner is dismissed
   */
  onDismiss: () => void;

  /**
   * Whether to auto-dismiss after timeout
   * Default: true
   */
  autoDismiss?: boolean;

  /**
   * Auto-dismiss timeout in milliseconds
   * Default: 5000
   */
  dismissTimeout?: number;
}

// ============================================
// Helper Functions
// ============================================

const getSeverityColor = (severity?: string): string => {
  switch (severity) {
    case AlertSeverity.CRITICAL:
      return Colors.critical;
    case AlertSeverity.HIGH:
      return Colors.high;
    case AlertSeverity.MEDIUM:
      return Colors.medium;
    case AlertSeverity.LOW:
      return Colors.low;
    default:
      return Colors.info;
  }
};

const getSeverityIcon = (severity?: string): keyof typeof MaterialCommunityIcons.glyphMap => {
  switch (severity) {
    case AlertSeverity.CRITICAL:
      return 'alert-octagon';
    case AlertSeverity.HIGH:
      return 'alert';
    case AlertSeverity.MEDIUM:
      return 'alert-circle';
    case AlertSeverity.LOW:
      return 'information';
    default:
      return 'bell';
  }
};

const getTypeIcon = (type?: string): keyof typeof MaterialCommunityIcons.glyphMap => {
  switch (type) {
    case NotificationCategories.ALERT:
      return 'alert-circle';
    case NotificationCategories.TICKET:
      return 'ticket';
    case NotificationCategories.DEVICE_STATUS:
      return 'monitor';
    default:
      return 'bell';
  }
};

// ============================================
// Component
// ============================================

export function NotificationBanner({
  notification,
  onDismiss,
  autoDismiss = true,
  dismissTimeout = 5000,
}: NotificationBannerProps) {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const translateY = useRef(new Animated.Value(-200)).current;
  const opacity = useRef(new Animated.Value(0)).current;
  const dismissTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Get notification details
  const data = notification?.data as NotificationData | undefined;
  const alertData = data?.type === NotificationCategories.ALERT ? data as AlertNotificationData : undefined;
  const severity = alertData?.severity;
  const accentColor = getSeverityColor(severity);

  // Animation: slide in
  const slideIn = () => {
    Animated.parallel([
      Animated.spring(translateY, {
        toValue: 0,
        useNativeDriver: true,
        tension: 100,
        friction: 10,
      }),
      Animated.timing(opacity, {
        toValue: 1,
        duration: 200,
        useNativeDriver: true,
      }),
    ]).start();
  };

  // Animation: slide out
  const slideOut = (callback?: () => void) => {
    Animated.parallel([
      Animated.timing(translateY, {
        toValue: -200,
        duration: 250,
        useNativeDriver: true,
      }),
      Animated.timing(opacity, {
        toValue: 0,
        duration: 200,
        useNativeDriver: true,
      }),
    ]).start(() => {
      callback?.();
    });
  };

  // Handle show/hide animations
  useEffect(() => {
    if (notification) {
      // Clear any existing timer
      if (dismissTimer.current) {
        clearTimeout(dismissTimer.current);
      }

      // Slide in
      slideIn();

      // Set auto-dismiss timer
      if (autoDismiss) {
        dismissTimer.current = setTimeout(() => {
          handleDismiss();
        }, dismissTimeout);
      }
    } else {
      // Slide out
      slideOut();
    }

    return () => {
      if (dismissTimer.current) {
        clearTimeout(dismissTimer.current);
      }
    };
  }, [notification, autoDismiss, dismissTimeout]);

  // Handle dismiss
  const handleDismiss = () => {
    if (dismissTimer.current) {
      clearTimeout(dismissTimer.current);
    }
    slideOut(() => {
      onDismiss();
    });
  };

  // Handle tap - navigate to relevant screen
  const handlePress = () => {
    handleDismiss();

    if (!data) return;

    // Navigate based on notification type
    switch (data.type) {
      case NotificationCategories.ALERT:
        if (data.alertId) {
          router.push(`/alert/${data.alertId}` as any);
        }
        break;
      case NotificationCategories.TICKET:
        if (data.ticketId) {
          router.push(`/ticket/${data.ticketId}` as any);
        }
        break;
      case NotificationCategories.DEVICE_STATUS:
        if (data.deviceId) {
          router.push(`/device/${data.deviceId}` as any);
        }
        break;
    }
  };

  // Don't render if no notification
  if (!notification) {
    return null;
  }

  // Determine icon
  const icon = alertData
    ? getSeverityIcon(severity)
    : getTypeIcon(data?.type);

  return (
    <Animated.View
      style={[
        styles.container,
        {
          transform: [{ translateY }],
          opacity,
          paddingTop: insets.top + Spacing.sm,
        },
      ]}
    >
      <TouchableOpacity
        style={styles.touchable}
        activeOpacity={0.9}
        onPress={handlePress}
      >
        <View style={[styles.banner, { borderLeftColor: accentColor }]}>
          {/* Icon */}
          <View style={[styles.iconContainer, { backgroundColor: accentColor }]}>
            <MaterialCommunityIcons
              name={icon}
              size={24}
              color={Colors.dark.text}
            />
          </View>

          {/* Content */}
          <View style={styles.content}>
            <Text style={styles.title} numberOfLines={1}>
              {notification.title}
            </Text>
            <Text style={styles.body} numberOfLines={2}>
              {notification.body}
            </Text>
          </View>

          {/* Dismiss button */}
          <TouchableOpacity
            style={styles.dismissButton}
            onPress={handleDismiss}
            hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
          >
            <MaterialCommunityIcons
              name="close"
              size={20}
              color={Colors.dark.textSecondary}
            />
          </TouchableOpacity>
        </View>
      </TouchableOpacity>
    </Animated.View>
  );
}

// ============================================
// Styles
// ============================================

const { width: SCREEN_WIDTH } = Dimensions.get('window');

const styles = StyleSheet.create({
  container: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    zIndex: 9999,
    paddingHorizontal: Spacing.md,
    paddingBottom: Spacing.sm,
    backgroundColor: 'transparent',
  },
  touchable: {
    width: '100%',
  },
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: Colors.dark.surface,
    borderRadius: BorderRadius.lg,
    borderLeftWidth: 4,
    paddingVertical: Spacing.md,
    paddingHorizontal: Spacing.md,
    ...Platform.select({
      ios: {
        shadowColor: '#000',
        shadowOffset: { width: 0, height: 4 },
        shadowOpacity: 0.3,
        shadowRadius: 8,
      },
      android: {
        elevation: 8,
      },
    }),
  },
  iconContainer: {
    width: 44,
    height: 44,
    borderRadius: BorderRadius.md,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: Spacing.md,
  },
  content: {
    flex: 1,
    marginRight: Spacing.sm,
  },
  title: {
    fontSize: FontSizes.md,
    fontWeight: '600',
    color: Colors.dark.text,
    marginBottom: 2,
  },
  body: {
    fontSize: FontSizes.sm,
    color: Colors.dark.textSecondary,
    lineHeight: 18,
  },
  dismissButton: {
    padding: Spacing.xs,
    borderRadius: BorderRadius.full,
    backgroundColor: Colors.dark.surfaceVariant,
  },
});

export default NotificationBanner;
