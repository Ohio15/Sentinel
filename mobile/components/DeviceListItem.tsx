/**
 * DeviceListItem Component
 * Displays a single device in the device list with status, OS icon, and navigation
 */
import React from 'react';
import { View, StyleSheet, TouchableOpacity } from 'react-native';
import { Text, useTheme } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';
import { Device, DeviceStatus } from '@/stores/deviceStore';
import { formatDistanceToNow } from 'date-fns';

interface DeviceListItemProps {
  device: Device;
  onPress: (device: Device) => void;
}

// Status dot colors
const statusColors: Record<DeviceStatus, string> = {
  online: Colors.online,
  offline: Colors.offline,
  warning: Colors.warning,
  critical: Colors.critical,
  disabled: Colors.dark.textMuted,
  uninstalling: Colors.warning,
};

// OS icon mapping
type OsIconName = 'microsoft-windows' | 'apple' | 'linux' | 'desktop-classic';
const getOsIcon = (osType: string): OsIconName => {
  const os = osType.toLowerCase();
  if (os.includes('windows')) return 'microsoft-windows';
  if (os.includes('mac') || os.includes('darwin')) return 'apple';
  if (os.includes('linux') || os.includes('ubuntu') || os.includes('debian')) return 'linux';
  return 'desktop-classic';
};

const getOsIconColor = (osType: string): string => {
  const os = osType.toLowerCase();
  if (os.includes('windows')) return '#0078D4';
  if (os.includes('mac') || os.includes('darwin')) return '#A2AAAD';
  if (os.includes('linux')) return '#FCC624';
  return Colors.dark.textSecondary;
};

export function DeviceListItem({ device, onPress }: DeviceListItemProps) {
  const theme = useTheme();

  const formatLastSeen = (lastSeen?: string): string => {
    if (!lastSeen) return 'Never';
    try {
      return formatDistanceToNow(new Date(lastSeen), { addSuffix: true });
    } catch {
      return 'Unknown';
    }
  };

  const statusColor = statusColors[device.status] || Colors.dark.textMuted;
  const osIcon = getOsIcon(device.osType);
  const osIconColor = getOsIconColor(device.osType);

  return (
    <TouchableOpacity
      style={[styles.container, { backgroundColor: theme.colors.surface }]}
      onPress={() => onPress(device)}
      activeOpacity={0.7}
    >
      {/* OS Icon */}
      <View style={styles.iconContainer}>
        <MaterialCommunityIcons
          name={osIcon}
          size={28}
          color={osIconColor}
        />
      </View>

      {/* Device Info */}
      <View style={styles.infoContainer}>
        <View style={styles.nameRow}>
          <Text style={styles.hostname} numberOfLines={1}>
            {device.displayName || device.hostname}
          </Text>
          {/* Status Dot */}
          <View style={[styles.statusDot, { backgroundColor: statusColor }]} />
        </View>

        <Text style={[styles.details, { color: theme.colors.onSurfaceVariant }]} numberOfLines={1}>
          {device.ipAddress || 'No IP'} | Last seen: {formatLastSeen(device.lastSeen)}
        </Text>
      </View>

      {/* Chevron */}
      <MaterialCommunityIcons
        name="chevron-right"
        size={24}
        color={Colors.dark.textMuted}
      />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.md,
    borderRadius: BorderRadius.md,
    marginHorizontal: Spacing.md,
    marginVertical: Spacing.xs,
  },
  iconContainer: {
    width: 44,
    height: 44,
    borderRadius: BorderRadius.md,
    backgroundColor: Colors.dark.surfaceVariant,
    justifyContent: 'center',
    alignItems: 'center',
    marginRight: Spacing.md,
  },
  infoContainer: {
    flex: 1,
    marginRight: Spacing.sm,
  },
  nameRow: {
    flexDirection: 'row',
    alignItems: 'center',
    marginBottom: Spacing.xs,
  },
  hostname: {
    fontSize: FontSizes.md,
    fontWeight: '600',
    color: Colors.dark.text,
    flex: 1,
  },
  statusDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
    marginLeft: Spacing.sm,
  },
  details: {
    fontSize: FontSizes.sm,
  },
});
