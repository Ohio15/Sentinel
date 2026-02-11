/**
 * Alert Detail Screen - Shows full alert information and actions
 * Accessed via /alert/[id] route
 */
import React, { useEffect, useState, useCallback } from 'react';
import {
  View,
  StyleSheet,
  ScrollView,
  RefreshControl,
  Pressable,
} from 'react-native';
import {
  Text,
  useTheme,
  Button,
  ActivityIndicator,
  Snackbar,
  Portal,
  Divider,
  Card,
} from 'react-native-paper';
import { useLocalSearchParams, router, Stack } from 'expo-router';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAlertStore, Alert, AlertStatus, AlertSeverity } from '@/stores/alertStore';
import { SeverityBadge, getSeverityColor, getSeverityBackgroundColor } from '@/components/SeverityBadge';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';

const statusConfig: Record<
  AlertStatus,
  {
    color: string;
    icon: keyof typeof MaterialCommunityIcons.glyphMap;
    label: string;
  }
> = {
  open: {
    color: Colors.error,
    icon: 'alert-circle-outline',
    label: 'Open',
  },
  acknowledged: {
    color: Colors.warning,
    icon: 'eye-check-outline',
    label: 'Acknowledged',
  },
  resolved: {
    color: Colors.success,
    icon: 'check-circle-outline',
    label: 'Resolved',
  },
};

/**
 * Format date for display
 */
function formatDateTime(dateString?: string): string {
  if (!dateString) return 'N/A';
  const date = new Date(dateString);
  return date.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  });
}

/**
 * Format relative time
 */
function formatTimeAgo(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);

  if (diffMin < 1) return 'Just now';
  if (diffMin < 60) return `${diffMin} minute${diffMin !== 1 ? 's' : ''} ago`;
  if (diffHour < 24) return `${diffHour} hour${diffHour !== 1 ? 's' : ''} ago`;
  return `${diffDay} day${diffDay !== 1 ? 's' : ''} ago`;
}

export default function AlertDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  const {
    getAlertById,
    fetchAlerts,
    acknowledgeAlert,
    resolveAlert,
    refreshing,
    error,
    clearError,
  } = useAlertStore();

  const [alert, setAlert] = useState<Alert | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<'acknowledge' | 'resolve' | null>(null);
  const [snackbarMessage, setSnackbarMessage] = useState<string | null>(null);

  // Load alert data
  useEffect(() => {
    const loadAlert = async () => {
      setIsLoading(true);

      // First try to get from store
      let foundAlert = getAlertById(id);

      // If not found, fetch alerts
      if (!foundAlert) {
        await fetchAlerts();
        foundAlert = getAlertById(id);
      }

      setAlert(foundAlert);
      setIsLoading(false);
    };

    loadAlert();
  }, [id, getAlertById, fetchAlerts]);

  // Update alert when store changes
  useEffect(() => {
    const foundAlert = getAlertById(id);
    if (foundAlert) {
      setAlert(foundAlert);
    }
  }, [id, getAlertById]);

  // Handle refresh
  const handleRefresh = useCallback(async () => {
    await fetchAlerts();
    const foundAlert = getAlertById(id);
    setAlert(foundAlert);
  }, [fetchAlerts, getAlertById, id]);

  // Handle acknowledge action
  const handleAcknowledge = useCallback(async () => {
    if (!alert) return;

    setActionLoading('acknowledge');
    try {
      await acknowledgeAlert(alert.id);
      setSnackbarMessage('Alert acknowledged');
      // Update local state
      setAlert(getAlertById(alert.id));
    } catch (err) {
      setSnackbarMessage('Failed to acknowledge alert');
    } finally {
      setActionLoading(null);
    }
  }, [alert, acknowledgeAlert, getAlertById]);

  // Handle resolve action
  const handleResolve = useCallback(async () => {
    if (!alert) return;

    setActionLoading('resolve');
    try {
      await resolveAlert(alert.id);
      setSnackbarMessage('Alert resolved');
      // Update local state
      setAlert(getAlertById(alert.id));
    } catch (err) {
      setSnackbarMessage('Failed to resolve alert');
    } finally {
      setActionLoading(null);
    }
  }, [alert, resolveAlert, getAlertById]);

  // Navigate to device
  const navigateToDevice = useCallback(() => {
    if (alert?.deviceId) {
      router.push(`/device/${alert.deviceId}`);
    }
  }, [alert?.deviceId]);

  // Loading state
  if (isLoading) {
    return (
      <View style={[styles.container, styles.centerContent, { backgroundColor: theme.colors.background }]}>
        <ActivityIndicator size="large" color={theme.colors.primary} />
        <Text style={[styles.loadingText, { color: theme.colors.onSurfaceVariant }]}>
          Loading alert...
        </Text>
      </View>
    );
  }

  // Not found state
  if (!alert) {
    return (
      <View style={[styles.container, styles.centerContent, { backgroundColor: theme.colors.background }]}>
        <MaterialCommunityIcons
          name="alert-circle-outline"
          size={64}
          color={theme.colors.onSurfaceVariant}
        />
        <Text style={[styles.notFoundTitle, { color: theme.colors.onSurface }]}>
          Alert Not Found
        </Text>
        <Text style={[styles.notFoundSubtitle, { color: theme.colors.onSurfaceVariant }]}>
          This alert may have been deleted or does not exist.
        </Text>
        <Button mode="contained" onPress={() => router.back()} style={styles.backButton}>
          Go Back
        </Button>
      </View>
    );
  }

  const statusInfo = statusConfig[alert.status];
  const severityColor = getSeverityColor(alert.severity);
  const severityBgColor = getSeverityBackgroundColor(alert.severity);

  return (
    <>
      {/* Custom header with severity color */}
      <Stack.Screen
        options={{
          headerTitle: 'Alert Details',
          headerStyle: { backgroundColor: theme.colors.surface },
          headerTintColor: theme.colors.primary,
        }}
      />

      <View style={[styles.container, { backgroundColor: theme.colors.background }]}>
        <ScrollView
          contentContainerStyle={styles.scrollContent}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              onRefresh={handleRefresh}
              tintColor={theme.colors.primary}
              colors={[theme.colors.primary]}
            />
          }
        >
          {/* Severity banner */}
          <View style={[styles.severityBanner, { backgroundColor: severityBgColor }]}>
            <View style={styles.severityHeader}>
              <SeverityBadge severity={alert.severity} size="large" />
              <View
                style={[
                  styles.statusBadge,
                  { backgroundColor: `${statusInfo.color}22` },
                ]}
              >
                <MaterialCommunityIcons
                  name={statusInfo.icon}
                  size={18}
                  color={statusInfo.color}
                />
                <Text style={[styles.statusText, { color: statusInfo.color }]}>
                  {statusInfo.label}
                </Text>
              </View>
            </View>
            <Text style={[styles.alertTitle, { color: theme.colors.onSurface }]}>
              {alert.title}
            </Text>
            <Text style={[styles.alertTime, { color: theme.colors.onSurfaceVariant }]}>
              {formatTimeAgo(alert.createdAt)}
            </Text>
          </View>

          {/* Alert message */}
          <Card style={[styles.card, { backgroundColor: theme.colors.surface }]}>
            <Card.Content>
              <Text style={[styles.sectionTitle, { color: theme.colors.onSurfaceVariant }]}>
                Message
              </Text>
              <Text style={[styles.messageText, { color: theme.colors.onSurface }]}>
                {alert.message || 'No additional details provided.'}
              </Text>
            </Card.Content>
          </Card>

          {/* Device info */}
          {alert.deviceId && (
            <Pressable onPress={navigateToDevice}>
              <Card style={[styles.card, { backgroundColor: theme.colors.surface }]}>
                <Card.Content style={styles.deviceCard}>
                  <View style={styles.deviceInfo}>
                    <MaterialCommunityIcons
                      name="laptop"
                      size={24}
                      color={theme.colors.primary}
                    />
                    <View style={styles.deviceTextContainer}>
                      <Text style={[styles.sectionTitle, { color: theme.colors.onSurfaceVariant }]}>
                        Device
                      </Text>
                      <Text style={[styles.deviceName, { color: theme.colors.onSurface }]}>
                        {alert.deviceName || 'Unknown Device'}
                      </Text>
                    </View>
                  </View>
                  <MaterialCommunityIcons
                    name="chevron-right"
                    size={24}
                    color={theme.colors.onSurfaceVariant}
                  />
                </Card.Content>
              </Card>
            </Pressable>
          )}

          {/* Timeline */}
          <Card style={[styles.card, { backgroundColor: theme.colors.surface }]}>
            <Card.Content>
              <Text style={[styles.sectionTitle, { color: theme.colors.onSurfaceVariant }]}>
                Timeline
              </Text>

              {/* Created */}
              <View style={styles.timelineItem}>
                <View style={[styles.timelineDot, { backgroundColor: Colors.info }]} />
                <View style={styles.timelineContent}>
                  <Text style={[styles.timelineLabel, { color: theme.colors.onSurface }]}>
                    Created
                  </Text>
                  <Text style={[styles.timelineValue, { color: theme.colors.onSurfaceVariant }]}>
                    {formatDateTime(alert.createdAt)}
                  </Text>
                </View>
              </View>

              {/* Acknowledged */}
              <View style={styles.timelineItem}>
                <View
                  style={[
                    styles.timelineDot,
                    {
                      backgroundColor: alert.acknowledgedAt
                        ? Colors.warning
                        : theme.colors.surfaceVariant,
                    },
                  ]}
                />
                <View style={styles.timelineContent}>
                  <Text
                    style={[
                      styles.timelineLabel,
                      {
                        color: alert.acknowledgedAt
                          ? theme.colors.onSurface
                          : theme.colors.onSurfaceVariant,
                      },
                    ]}
                  >
                    Acknowledged
                  </Text>
                  <Text style={[styles.timelineValue, { color: theme.colors.onSurfaceVariant }]}>
                    {alert.acknowledgedAt
                      ? formatDateTime(alert.acknowledgedAt)
                      : 'Not yet acknowledged'}
                  </Text>
                  {alert.acknowledgedBy && (
                    <Text style={[styles.timelineBy, { color: theme.colors.onSurfaceVariant }]}>
                      by {alert.acknowledgedBy}
                    </Text>
                  )}
                </View>
              </View>

              {/* Resolved */}
              <View style={[styles.timelineItem, styles.lastTimelineItem]}>
                <View
                  style={[
                    styles.timelineDot,
                    {
                      backgroundColor: alert.resolvedAt
                        ? Colors.success
                        : theme.colors.surfaceVariant,
                    },
                  ]}
                />
                <View style={styles.timelineContent}>
                  <Text
                    style={[
                      styles.timelineLabel,
                      {
                        color: alert.resolvedAt
                          ? theme.colors.onSurface
                          : theme.colors.onSurfaceVariant,
                      },
                    ]}
                  >
                    Resolved
                  </Text>
                  <Text style={[styles.timelineValue, { color: theme.colors.onSurfaceVariant }]}>
                    {alert.resolvedAt
                      ? formatDateTime(alert.resolvedAt)
                      : 'Not yet resolved'}
                  </Text>
                  {alert.resolvedBy && (
                    <Text style={[styles.timelineBy, { color: theme.colors.onSurfaceVariant }]}>
                      by {alert.resolvedBy}
                    </Text>
                  )}
                </View>
              </View>
            </Card.Content>
          </Card>

          {/* Additional info */}
          <Card style={[styles.card, { backgroundColor: theme.colors.surface }]}>
            <Card.Content>
              <Text style={[styles.sectionTitle, { color: theme.colors.onSurfaceVariant }]}>
                Details
              </Text>
              <View style={styles.detailRow}>
                <Text style={[styles.detailLabel, { color: theme.colors.onSurfaceVariant }]}>
                  Alert ID
                </Text>
                <Text style={[styles.detailValue, { color: theme.colors.onSurface }]}>
                  {alert.id.slice(0, 8)}...
                </Text>
              </View>
              {alert.ruleId && (
                <View style={styles.detailRow}>
                  <Text style={[styles.detailLabel, { color: theme.colors.onSurfaceVariant }]}>
                    Rule ID
                  </Text>
                  <Text style={[styles.detailValue, { color: theme.colors.onSurface }]}>
                    {alert.ruleId.slice(0, 8)}...
                  </Text>
                </View>
              )}
            </Card.Content>
          </Card>
        </ScrollView>

        {/* Action buttons */}
        {alert.status !== 'resolved' && (
          <View
            style={[
              styles.actionBar,
              {
                backgroundColor: theme.colors.surface,
                paddingBottom: insets.bottom + Spacing.md,
              },
            ]}
          >
            {alert.status === 'open' && (
              <Button
                mode="outlined"
                onPress={handleAcknowledge}
                loading={actionLoading === 'acknowledge'}
                disabled={actionLoading !== null}
                icon="eye-check"
                style={styles.actionButton}
                contentStyle={styles.actionButtonContent}
              >
                Acknowledge
              </Button>
            )}
            <Button
              mode="contained"
              onPress={handleResolve}
              loading={actionLoading === 'resolve'}
              disabled={actionLoading !== null}
              icon="check-circle"
              style={[styles.actionButton, styles.resolveButton]}
              contentStyle={styles.actionButtonContent}
            >
              Resolve
            </Button>
          </View>
        )}

        {/* Snackbar */}
        <Portal>
          <Snackbar
            visible={!!snackbarMessage}
            onDismiss={() => setSnackbarMessage(null)}
            duration={3000}
            style={styles.snackbar}
          >
            {snackbarMessage}
          </Snackbar>
        </Portal>

        {/* Error snackbar */}
        <Portal>
          <Snackbar
            visible={!!error}
            onDismiss={clearError}
            duration={5000}
            action={{
              label: 'Dismiss',
              onPress: clearError,
            }}
            style={[styles.snackbar, { backgroundColor: Colors.error }]}
          >
            {error}
          </Snackbar>
        </Portal>
      </View>
    </>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  centerContent: {
    justifyContent: 'center',
    alignItems: 'center',
  },
  loadingText: {
    marginTop: Spacing.md,
    fontSize: FontSizes.md,
  },
  notFoundTitle: {
    fontSize: FontSizes.lg,
    fontWeight: '600',
    marginTop: Spacing.md,
  },
  notFoundSubtitle: {
    fontSize: FontSizes.md,
    textAlign: 'center',
    marginTop: Spacing.xs,
    paddingHorizontal: Spacing.xl,
  },
  backButton: {
    marginTop: Spacing.lg,
  },
  scrollContent: {
    paddingBottom: 100,
  },
  severityBanner: {
    padding: Spacing.lg,
    marginBottom: Spacing.md,
  },
  severityHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: Spacing.md,
  },
  statusBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Spacing.sm,
    paddingVertical: 4,
    borderRadius: BorderRadius.sm,
    gap: 4,
  },
  statusText: {
    fontSize: FontSizes.sm,
    fontWeight: '600',
  },
  alertTitle: {
    fontSize: FontSizes.xl,
    fontWeight: '700',
    marginBottom: Spacing.xs,
  },
  alertTime: {
    fontSize: FontSizes.sm,
  },
  card: {
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.md,
    borderRadius: BorderRadius.md,
  },
  sectionTitle: {
    fontSize: FontSizes.xs,
    fontWeight: '600',
    textTransform: 'uppercase',
    letterSpacing: 0.5,
    marginBottom: Spacing.sm,
  },
  messageText: {
    fontSize: FontSizes.md,
    lineHeight: 24,
  },
  deviceCard: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  deviceInfo: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.md,
  },
  deviceTextContainer: {
    gap: 2,
  },
  deviceName: {
    fontSize: FontSizes.md,
    fontWeight: '500',
  },
  timelineItem: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    marginBottom: Spacing.md,
    paddingLeft: Spacing.xs,
  },
  lastTimelineItem: {
    marginBottom: 0,
  },
  timelineDot: {
    width: 12,
    height: 12,
    borderRadius: 6,
    marginTop: 4,
  },
  timelineContent: {
    marginLeft: Spacing.md,
    flex: 1,
  },
  timelineLabel: {
    fontSize: FontSizes.sm,
    fontWeight: '600',
  },
  timelineValue: {
    fontSize: FontSizes.sm,
    marginTop: 2,
  },
  timelineBy: {
    fontSize: FontSizes.xs,
    fontStyle: 'italic',
    marginTop: 2,
  },
  detailRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: Spacing.xs,
  },
  detailLabel: {
    fontSize: FontSizes.sm,
  },
  detailValue: {
    fontSize: FontSizes.sm,
    fontFamily: 'monospace',
  },
  actionBar: {
    position: 'absolute',
    bottom: 0,
    left: 0,
    right: 0,
    flexDirection: 'row',
    padding: Spacing.md,
    gap: Spacing.md,
    borderTopWidth: 1,
    borderTopColor: 'rgba(255,255,255,0.1)',
  },
  actionButton: {
    flex: 1,
  },
  actionButtonContent: {
    paddingVertical: 6,
  },
  resolveButton: {
    backgroundColor: Colors.success,
  },
  snackbar: {
    marginBottom: 100,
  },
});
