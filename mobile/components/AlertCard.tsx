/**
 * Alert Card Component
 * Displays an alert in a card format with severity indicator and actions
 */
import React from 'react';
import { View, StyleSheet, Pressable } from 'react-native';
import { Text, useTheme, Chip } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing, BorderRadius, FontSizes } from '../constants/theme';
import { SeverityBadge, SeverityBar, getSeverityColor } from './SeverityBadge';
import type { Alert, AlertStatus } from '../stores/alertStore';

interface AlertCardProps {
  alert: Alert;
  onPress?: () => void;
  onAcknowledge?: () => void;
  onResolve?: () => void;
}

const statusConfig: Record<
  AlertStatus,
  {
    color: string;
    backgroundColor: string;
    label: string;
    icon: keyof typeof MaterialCommunityIcons.glyphMap;
  }
> = {
  open: {
    color: Colors.error,
    backgroundColor: 'rgba(239, 68, 68, 0.15)',
    label: 'Open',
    icon: 'alert-circle-outline',
  },
  acknowledged: {
    color: Colors.warning,
    backgroundColor: 'rgba(245, 158, 11, 0.15)',
    label: 'Acknowledged',
    icon: 'eye-check-outline',
  },
  resolved: {
    color: Colors.success,
    backgroundColor: 'rgba(34, 197, 94, 0.15)',
    label: 'Resolved',
    icon: 'check-circle-outline',
  },
};

/**
 * Format a timestamp to relative time (e.g., "5 min ago")
 */
function formatTimeAgo(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);

  if (diffSec < 60) {
    return 'Just now';
  } else if (diffMin < 60) {
    return `${diffMin} min ago`;
  } else if (diffHour < 24) {
    return `${diffHour}h ago`;
  } else if (diffDay < 7) {
    return `${diffDay}d ago`;
  } else {
    return date.toLocaleDateString();
  }
}

export function AlertCard({ alert, onPress, onAcknowledge, onResolve }: AlertCardProps) {
  const theme = useTheme();
  const statusInfo = statusConfig[alert.status];
  const severityColor = getSeverityColor(alert.severity);

  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        styles.container,
        { backgroundColor: theme.colors.surface },
        pressed && styles.pressed,
      ]}
    >
      {/* Severity color bar on left */}
      <SeverityBar severity={alert.severity} />

      <View style={styles.content}>
        {/* Header row with badges */}
        <View style={styles.header}>
          <View style={styles.badges}>
            <SeverityBadge severity={alert.severity} size="small" />
            <StatusBadge status={alert.status} />
          </View>
          <Text style={[styles.timeAgo, { color: theme.colors.onSurfaceVariant }]}>
            {formatTimeAgo(alert.createdAt)}
          </Text>
        </View>

        {/* Alert title */}
        <Text style={[styles.title, { color: theme.colors.onSurface }]} numberOfLines={2}>
          {alert.title}
        </Text>

        {/* Alert message */}
        {alert.message && (
          <Text
            style={[styles.message, { color: theme.colors.onSurfaceVariant }]}
            numberOfLines={2}
          >
            {alert.message}
          </Text>
        )}

        {/* Device info */}
        {alert.deviceName && (
          <View style={styles.deviceRow}>
            <MaterialCommunityIcons
              name="laptop"
              size={14}
              color={theme.colors.onSurfaceVariant}
            />
            <Text style={[styles.deviceName, { color: theme.colors.onSurfaceVariant }]}>
              {alert.deviceName}
            </Text>
          </View>
        )}
      </View>

      {/* Right arrow for navigation */}
      <View style={styles.chevron}>
        <MaterialCommunityIcons
          name="chevron-right"
          size={24}
          color={theme.colors.onSurfaceVariant}
        />
      </View>
    </Pressable>
  );
}

/**
 * Status Badge sub-component
 */
function StatusBadge({ status }: { status: AlertStatus }) {
  const config = statusConfig[status];

  return (
    <View
      style={[
        styles.statusBadge,
        { backgroundColor: config.backgroundColor },
      ]}
    >
      <MaterialCommunityIcons
        name={config.icon}
        size={12}
        color={config.color}
      />
      <Text style={[styles.statusText, { color: config.color }]}>
        {config.label}
      </Text>
    </View>
  );
}

/**
 * Compact alert card for dashboard or summary views
 */
interface CompactAlertCardProps {
  alert: Alert;
  onPress?: () => void;
}

export function CompactAlertCard({ alert, onPress }: CompactAlertCardProps) {
  const theme = useTheme();
  const severityColor = getSeverityColor(alert.severity);

  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        styles.compactContainer,
        { backgroundColor: theme.colors.surface },
        pressed && styles.pressed,
      ]}
    >
      <View style={[styles.compactIndicator, { backgroundColor: severityColor }]} />
      <View style={styles.compactContent}>
        <Text
          style={[styles.compactTitle, { color: theme.colors.onSurface }]}
          numberOfLines={1}
        >
          {alert.title}
        </Text>
        <Text
          style={[styles.compactSubtitle, { color: theme.colors.onSurfaceVariant }]}
          numberOfLines={1}
        >
          {alert.deviceName || 'Unknown device'} - {formatTimeAgo(alert.createdAt)}
        </Text>
      </View>
      <MaterialCommunityIcons
        name="chevron-right"
        size={20}
        color={theme.colors.onSurfaceVariant}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    borderRadius: BorderRadius.md,
    overflow: 'hidden',
    marginHorizontal: Spacing.md,
    marginVertical: Spacing.xs,
    elevation: 2,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.2,
    shadowRadius: 2,
  },
  pressed: {
    opacity: 0.8,
  },
  content: {
    flex: 1,
    padding: Spacing.md,
    gap: Spacing.xs,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  badges: {
    flexDirection: 'row',
    gap: Spacing.xs,
  },
  timeAgo: {
    fontSize: FontSizes.xs,
  },
  title: {
    fontSize: FontSizes.md,
    fontWeight: '600',
    marginTop: Spacing.xs,
  },
  message: {
    fontSize: FontSizes.sm,
    lineHeight: 20,
  },
  deviceRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    marginTop: Spacing.xs,
  },
  deviceName: {
    fontSize: FontSizes.xs,
  },
  chevron: {
    justifyContent: 'center',
    paddingRight: Spacing.sm,
  },
  statusBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Spacing.xs,
    paddingVertical: 2,
    borderRadius: BorderRadius.sm,
    gap: 4,
  },
  statusText: {
    fontSize: FontSizes.xs,
    fontWeight: '600',
  },
  // Compact card styles
  compactContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingRight: Spacing.md,
    borderRadius: BorderRadius.md,
    marginHorizontal: Spacing.md,
    marginVertical: Spacing.xs,
    overflow: 'hidden',
  },
  compactIndicator: {
    width: 4,
    height: '100%',
    minHeight: 48,
  },
  compactContent: {
    flex: 1,
    paddingVertical: Spacing.sm,
    paddingHorizontal: Spacing.md,
  },
  compactTitle: {
    fontSize: FontSizes.sm,
    fontWeight: '500',
  },
  compactSubtitle: {
    fontSize: FontSizes.xs,
    marginTop: 2,
  },
});

export default AlertCard;
