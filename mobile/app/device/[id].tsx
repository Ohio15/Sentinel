/**
 * Device Detail Screen
 * Shows device information, metrics, alerts, and quick actions
 */
import React, { useEffect, useState, useCallback } from 'react';
import {
  View,
  StyleSheet,
  ScrollView,
  RefreshControl,
  useColorScheme,
  Alert,
} from 'react-native';
import {
  Text,
  Card,
  ActivityIndicator,
  Divider,
  Switch,
  Snackbar,
  ProgressBar,
} from 'react-native-paper';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useLocalSearchParams, Stack, useRouter } from 'expo-router';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';
import { useDeviceStore, DeviceStatus } from '@/stores/deviceStore';
import { QuickActionButton } from '@/components/QuickActionButton';
import { formatDistanceToNow, format } from 'date-fns';

// Status configuration
const statusConfig: Record<DeviceStatus, { color: string; label: string; icon: keyof typeof MaterialCommunityIcons.glyphMap }> = {
  online: { color: Colors.online, label: 'Online', icon: 'check-circle' },
  offline: { color: Colors.offline, label: 'Offline', icon: 'close-circle' },
  warning: { color: Colors.warning, label: 'Warning', icon: 'alert-circle' },
  critical: { color: Colors.critical, label: 'Critical', icon: 'alert-octagon' },
  disabled: { color: Colors.dark.textMuted, label: 'Disabled', icon: 'pause-circle' },
  uninstalling: { color: Colors.warning, label: 'Uninstalling', icon: 'delete-clock' },
};

// OS Icon mapping
const getOsIcon = (osType: string): keyof typeof MaterialCommunityIcons.glyphMap => {
  const os = osType.toLowerCase();
  if (os.includes('windows')) return 'microsoft-windows';
  if (os.includes('mac') || os.includes('darwin')) return 'apple';
  if (os.includes('linux')) return 'linux';
  return 'desktop-classic';
};

export default function DeviceDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const colorScheme = useColorScheme();
  const isDark = colorScheme === 'dark';
  const themeColors = isDark ? Colors.dark : Colors.light;

  const {
    selectedDevice: device,
    deviceAlerts,
    deviceMetrics,
    detailLoading,
    error,
    fetchDeviceDetail,
    fetchDeviceAlerts,
    pingDevice,
    rebootDevice,
    disableDevice,
    enableDevice,
    clearError,
  } = useDeviceStore();

  const [refreshing, setRefreshing] = useState(false);
  const [pingResult, setPingResult] = useState<{ success: boolean; latency?: number } | null>(null);
  const [showPingSnackbar, setShowPingSnackbar] = useState(false);

  // Fetch device details on mount
  useEffect(() => {
    if (id) {
      fetchDeviceDetail(id);
      fetchDeviceAlerts(id);
    }
  }, [id]);

  const handleRefresh = useCallback(async () => {
    if (!id) return;
    setRefreshing(true);
    await Promise.all([
      fetchDeviceDetail(id),
      fetchDeviceAlerts(id),
    ]);
    setRefreshing(false);
  }, [id, fetchDeviceDetail, fetchDeviceAlerts]);

  const handlePing = useCallback(async () => {
    if (!device) return;
    try {
      const result = await pingDevice(device.id);
      setPingResult(result);
      setShowPingSnackbar(true);
    } catch (error) {
      setPingResult({ success: false });
      setShowPingSnackbar(true);
    }
  }, [device, pingDevice]);

  const handleReboot = useCallback(async () => {
    if (!device) return;
    await rebootDevice(device.id);
    Alert.alert('Reboot Initiated', 'The device will restart shortly.');
  }, [device, rebootDevice]);

  const handleToggleEnabled = useCallback(async () => {
    if (!device) return;
    if (device.status === 'disabled') {
      await enableDevice(device.id);
    } else {
      await disableDevice(device.id);
    }
  }, [device, enableDevice, disableDevice]);

  const formatLastSeen = (lastSeen?: string): string => {
    if (!lastSeen) return 'Never';
    try {
      return formatDistanceToNow(new Date(lastSeen), { addSuffix: true });
    } catch {
      return 'Unknown';
    }
  };

  const formatUptime = (lastSeen?: string): string => {
    if (!lastSeen) return 'N/A';
    try {
      return format(new Date(lastSeen), 'MMM d, yyyy HH:mm');
    } catch {
      return 'Unknown';
    }
  };

  if (detailLoading && !refreshing) {
    return (
      <SafeAreaView style={[styles.container, { backgroundColor: themeColors.background }]}>
        <Stack.Screen options={{ title: 'Loading...' }} />
        <View style={styles.loadingContainer}>
          <ActivityIndicator size="large" color={Colors.primary} />
          <Text style={[styles.loadingText, { color: themeColors.textSecondary }]}>
            Loading device details...
          </Text>
        </View>
      </SafeAreaView>
    );
  }

  if (!device) {
    return (
      <SafeAreaView style={[styles.container, { backgroundColor: themeColors.background }]}>
        <Stack.Screen options={{ title: 'Device Not Found' }} />
        <View style={styles.errorContainer}>
          <MaterialCommunityIcons name="server-off" size={64} color={Colors.error} />
          <Text style={[styles.errorTitle, { color: themeColors.text }]}>
            Device not found
          </Text>
          <Text style={[styles.errorSubtitle, { color: themeColors.textSecondary }]}>
            The device may have been removed or is no longer accessible.
          </Text>
        </View>
      </SafeAreaView>
    );
  }

  const status = statusConfig[device.status] || statusConfig.offline;
  const isOnline = device.status === 'online';
  const isDisabled = device.status === 'disabled';

  return (
    <SafeAreaView style={[styles.container, { backgroundColor: themeColors.background }]} edges={[]}>
      <Stack.Screen
        options={{
          title: device.displayName || device.hostname,
          headerStyle: { backgroundColor: themeColors.surface },
          headerTintColor: Colors.primary,
        }}
      />

      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={styles.scrollContent}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={handleRefresh}
            tintColor={Colors.primary}
            colors={[Colors.primary]}
          />
        }
        showsVerticalScrollIndicator={false}
      >
        {/* Status Header */}
        <View style={[styles.statusHeader, { backgroundColor: themeColors.surface }]}>
          <View style={styles.statusRow}>
            <MaterialCommunityIcons
              name={getOsIcon(device.osType)}
              size={48}
              color={Colors.dark.textSecondary}
            />
            <View style={styles.statusInfo}>
              <Text style={[styles.deviceName, { color: themeColors.text }]}>
                {device.displayName || device.hostname}
              </Text>
              <View style={styles.statusBadge}>
                <MaterialCommunityIcons
                  name={status.icon}
                  size={16}
                  color={status.color}
                />
                <Text style={[styles.statusText, { color: status.color }]}>
                  {status.label}
                </Text>
              </View>
            </View>
          </View>
        </View>

        {/* Device Info Card */}
        <Card style={[styles.card, { backgroundColor: themeColors.surface }]}>
          <Card.Title
            title="Device Information"
            titleStyle={{ color: themeColors.text }}
            left={(props) => (
              <MaterialCommunityIcons
                name="information-outline"
                size={24}
                color={Colors.primary}
              />
            )}
          />
          <Card.Content>
            <InfoRow
              label="Operating System"
              value={`${device.osType} ${device.osVersion || ''}`}
              color={themeColors.textSecondary}
            />
            <Divider style={styles.divider} />
            <InfoRow
              label="IP Address"
              value={device.ipAddress || 'N/A'}
              color={themeColors.textSecondary}
            />
            <Divider style={styles.divider} />
            <InfoRow
              label="Agent Version"
              value={device.agentVersion || 'N/A'}
              color={themeColors.textSecondary}
            />
            <Divider style={styles.divider} />
            <InfoRow
              label="Last Seen"
              value={formatLastSeen(device.lastSeen)}
              color={themeColors.textSecondary}
            />
          </Card.Content>
        </Card>

        {/* Metrics Card */}
        {deviceMetrics && (deviceMetrics.cpuUsage !== undefined || deviceMetrics.memoryUsage !== undefined || deviceMetrics.diskUsage !== undefined) && (
          <Card style={[styles.card, { backgroundColor: themeColors.surface }]}>
            <Card.Title
              title="System Metrics"
              titleStyle={{ color: themeColors.text }}
              left={(props) => (
                <MaterialCommunityIcons
                  name="chart-line"
                  size={24}
                  color={Colors.primary}
                />
              )}
            />
            <Card.Content>
              {deviceMetrics.cpuUsage !== undefined && (
                <MetricRow
                  label="CPU Usage"
                  value={deviceMetrics.cpuUsage}
                  color={themeColors}
                />
              )}
              {deviceMetrics.memoryUsage !== undefined && (
                <MetricRow
                  label="Memory Usage"
                  value={deviceMetrics.memoryUsage}
                  color={themeColors}
                />
              )}
              {deviceMetrics.diskUsage !== undefined && (
                <MetricRow
                  label="Disk Usage"
                  value={deviceMetrics.diskUsage}
                  color={themeColors}
                />
              )}
            </Card.Content>
          </Card>
        )}

        {/* Recent Alerts Card */}
        {deviceAlerts.length > 0 && (
          <Card style={[styles.card, { backgroundColor: themeColors.surface }]}>
            <Card.Title
              title="Recent Alerts"
              titleStyle={{ color: themeColors.text }}
              left={(props) => (
                <MaterialCommunityIcons
                  name="bell-alert"
                  size={24}
                  color={Colors.warning}
                />
              )}
            />
            <Card.Content>
              {deviceAlerts.slice(0, 5).map((alert, index) => (
                <View key={alert.id}>
                  {index > 0 && <Divider style={styles.divider} />}
                  <View style={styles.alertRow}>
                    <MaterialCommunityIcons
                      name="alert-circle"
                      size={20}
                      color={
                        alert.severity === 'critical' ? Colors.critical :
                        alert.severity === 'high' ? Colors.high :
                        alert.severity === 'medium' ? Colors.medium :
                        Colors.low
                      }
                    />
                    <View style={styles.alertInfo}>
                      <Text style={[styles.alertTitle, { color: themeColors.text }]} numberOfLines={1}>
                        {alert.title}
                      </Text>
                      <Text style={[styles.alertTime, { color: themeColors.textMuted }]}>
                        {formatLastSeen(alert.createdAt)}
                      </Text>
                    </View>
                  </View>
                </View>
              ))}
            </Card.Content>
          </Card>
        )}

        {/* Quick Actions Card */}
        <Card style={[styles.card, { backgroundColor: themeColors.surface }]}>
          <Card.Title
            title="Quick Actions"
            titleStyle={{ color: themeColors.text }}
            left={(props) => (
              <MaterialCommunityIcons
                name="lightning-bolt"
                size={24}
                color={Colors.primary}
              />
            )}
          />
          <Card.Content>
            <QuickActionButton
              icon="access-point"
              label="Ping Device"
              onPress={handlePing}
              variant="primary"
              disabled={!isOnline}
            />

            <QuickActionButton
              icon="restart"
              label="Reboot Device"
              onPress={handleReboot}
              variant="warning"
              disabled={!isOnline}
              confirmTitle="Confirm Reboot"
              confirmMessage="Are you sure you want to reboot this device? All active sessions will be disconnected."
              confirmButtonLabel="Reboot"
            />

            <View style={styles.toggleRow}>
              <View style={styles.toggleInfo}>
                <MaterialCommunityIcons
                  name={isDisabled ? 'toggle-switch-off' : 'toggle-switch'}
                  size={24}
                  color={isDisabled ? Colors.dark.textMuted : Colors.primary}
                />
                <Text style={[styles.toggleLabel, { color: themeColors.text }]}>
                  {isDisabled ? 'Device Disabled' : 'Device Enabled'}
                </Text>
              </View>
              <Switch
                value={!isDisabled}
                onValueChange={handleToggleEnabled}
                color={Colors.primary}
              />
            </View>
          </Card.Content>
        </Card>

        {/* Spacer for bottom safe area */}
        <View style={styles.bottomSpacer} />
      </ScrollView>

      {/* Ping Result Snackbar */}
      <Snackbar
        visible={showPingSnackbar}
        onDismiss={() => setShowPingSnackbar(false)}
        duration={3000}
        style={{
          backgroundColor: pingResult?.success ? Colors.success : Colors.error,
        }}
      >
        {pingResult?.success
          ? `Ping successful${pingResult.latency ? ` (${pingResult.latency}ms)` : ''}`
          : 'Ping failed - device may be offline'}
      </Snackbar>

      {/* Error Snackbar */}
      <Snackbar
        visible={!!error}
        onDismiss={clearError}
        duration={4000}
        action={{
          label: 'Dismiss',
          onPress: clearError,
        }}
        style={{ backgroundColor: Colors.error }}
      >
        {error}
      </Snackbar>
    </SafeAreaView>
  );
}

// Helper Components
interface InfoRowProps {
  label: string;
  value: string;
  color: string;
}

function InfoRow({ label, value, color }: InfoRowProps) {
  return (
    <View style={styles.infoRow}>
      <Text style={[styles.infoLabel, { color }]}>{label}</Text>
      <Text style={[styles.infoValue, { color: Colors.dark.text }]}>{value}</Text>
    </View>
  );
}

interface MetricRowProps {
  label: string;
  value: number;
  color: { text: string; textSecondary: string };
}

function MetricRow({ label, value, color }: MetricRowProps) {
  const progressColor =
    value > 90 ? Colors.critical :
    value > 75 ? Colors.warning :
    Colors.primary;

  return (
    <View style={styles.metricRow}>
      <View style={styles.metricHeader}>
        <Text style={[styles.metricLabel, { color: color.textSecondary }]}>{label}</Text>
        <Text style={[styles.metricValue, { color: color.text }]}>{Math.round(value)}%</Text>
      </View>
      <ProgressBar
        progress={value / 100}
        color={progressColor}
        style={styles.progressBar}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingBottom: Spacing.xxl,
  },
  loadingContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  loadingText: {
    marginTop: Spacing.md,
    fontSize: FontSizes.md,
  },
  errorContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: Spacing.xl,
  },
  errorTitle: {
    fontSize: FontSizes.xl,
    fontWeight: '600',
    marginTop: Spacing.lg,
  },
  errorSubtitle: {
    fontSize: FontSizes.md,
    marginTop: Spacing.sm,
    textAlign: 'center',
  },
  statusHeader: {
    padding: Spacing.lg,
    marginBottom: Spacing.md,
  },
  statusRow: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  statusInfo: {
    marginLeft: Spacing.md,
    flex: 1,
  },
  deviceName: {
    fontSize: FontSizes.xl,
    fontWeight: 'bold',
  },
  statusBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    marginTop: Spacing.xs,
  },
  statusText: {
    marginLeft: Spacing.xs,
    fontSize: FontSizes.sm,
    fontWeight: '500',
  },
  card: {
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.md,
    borderRadius: BorderRadius.lg,
  },
  divider: {
    marginVertical: Spacing.sm,
  },
  infoRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: Spacing.xs,
  },
  infoLabel: {
    fontSize: FontSizes.sm,
  },
  infoValue: {
    fontSize: FontSizes.sm,
    fontWeight: '500',
  },
  metricRow: {
    marginBottom: Spacing.md,
  },
  metricHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: Spacing.xs,
  },
  metricLabel: {
    fontSize: FontSizes.sm,
  },
  metricValue: {
    fontSize: FontSizes.sm,
    fontWeight: '600',
  },
  progressBar: {
    height: 8,
    borderRadius: 4,
  },
  alertRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: Spacing.xs,
  },
  alertInfo: {
    marginLeft: Spacing.sm,
    flex: 1,
  },
  alertTitle: {
    fontSize: FontSizes.sm,
    fontWeight: '500',
  },
  alertTime: {
    fontSize: FontSizes.xs,
    marginTop: 2,
  },
  toggleRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: Spacing.md,
    marginTop: Spacing.sm,
  },
  toggleInfo: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  toggleLabel: {
    marginLeft: Spacing.sm,
    fontSize: FontSizes.md,
  },
  bottomSpacer: {
    height: Spacing.xl,
  },
});
