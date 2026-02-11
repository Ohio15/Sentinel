/**
 * AlertListItem Component - Sentinel Mobile
 * Displays an alert with severity indicator, device name, message, and time
 */
import React from 'react';
import { StyleSheet, Pressable, View } from 'react-native';
import { Text, useTheme } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { formatDistanceToNow } from 'date-fns';

type AlertSeverity = 'info' | 'warning' | 'critical';

interface AlertListItemProps {
  id: string;
  title: string;
  message?: string;
  severity: AlertSeverity;
  deviceName?: string;
  createdAt: string;
  onPress?: () => void;
}

const severityConfig: Record<AlertSeverity, { color: string; icon: keyof typeof MaterialCommunityIcons.glyphMap }> = {
  critical: { color: '#ef4444', icon: 'alert-circle' },
  warning: { color: '#eab308', icon: 'alert' },
  info: { color: '#3b82f6', icon: 'information' },
};

export function AlertListItem({
  id,
  title,
  message,
  severity,
  deviceName,
  createdAt,
  onPress,
}: AlertListItemProps) {
  const theme = useTheme();
  const config = severityConfig[severity] || severityConfig.info;

  const timeAgo = React.useMemo(() => {
    try {
      return formatDistanceToNow(new Date(createdAt), { addSuffix: true });
    } catch {
      return 'Unknown time';
    }
  }, [createdAt]);

  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        styles.container,
        { backgroundColor: theme.colors.surface },
        pressed && styles.pressed,
      ]}
    >
      {/* Severity indicator */}
      <View style={[styles.severityIndicator, { backgroundColor: config.color }]} />

      {/* Content */}
      <View style={styles.content}>
        <View style={styles.header}>
          <MaterialCommunityIcons
            name={config.icon}
            size={18}
            color={config.color}
            style={styles.icon}
          />
          <Text
            style={[styles.title, { color: theme.colors.onSurface }]}
            numberOfLines={1}
            ellipsizeMode="tail"
          >
            {title}
          </Text>
        </View>

        {deviceName && (
          <View style={styles.deviceRow}>
            <MaterialCommunityIcons
              name="desktop-tower-monitor"
              size={14}
              color={theme.colors.onSurfaceVariant}
            />
            <Text
              style={[styles.deviceName, { color: theme.colors.onSurfaceVariant }]}
              numberOfLines={1}
            >
              {deviceName}
            </Text>
          </View>
        )}

        {message && (
          <Text
            style={[styles.message, { color: theme.colors.onSurfaceVariant }]}
            numberOfLines={2}
            ellipsizeMode="tail"
          >
            {message}
          </Text>
        )}

        <Text style={[styles.time, { color: theme.colors.onSurfaceVariant }]}>
          {timeAgo}
        </Text>
      </View>

      {/* Chevron */}
      <MaterialCommunityIcons
        name="chevron-right"
        size={20}
        color={theme.colors.onSurfaceVariant}
        style={styles.chevron}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 12,
    paddingHorizontal: 16,
    marginHorizontal: 16,
    marginVertical: 4,
    borderRadius: 12,
    overflow: 'hidden',
  },
  pressed: {
    opacity: 0.7,
  },
  severityIndicator: {
    width: 4,
    height: '100%',
    minHeight: 50,
    borderRadius: 2,
    marginRight: 12,
    position: 'absolute',
    left: 0,
    top: 0,
    bottom: 0,
  },
  content: {
    flex: 1,
    paddingLeft: 8,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    marginBottom: 4,
  },
  icon: {
    marginRight: 6,
  },
  title: {
    fontSize: 15,
    fontWeight: '600',
    flex: 1,
  },
  deviceRow: {
    flexDirection: 'row',
    alignItems: 'center',
    marginBottom: 4,
  },
  deviceName: {
    fontSize: 13,
    marginLeft: 4,
  },
  message: {
    fontSize: 13,
    lineHeight: 18,
    marginBottom: 4,
  },
  time: {
    fontSize: 12,
    marginTop: 2,
  },
  chevron: {
    marginLeft: 8,
  },
});
