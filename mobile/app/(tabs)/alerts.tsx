/**
 * Alerts Screen - Alert list with filtering and swipe actions
 * Displays all alerts with pull-to-refresh and filter chips
 */
import React, { useEffect, useCallback, useState, useRef } from 'react';
import {
  View,
  StyleSheet,
  FlatList,
  RefreshControl,
  Animated,
  Dimensions,
} from 'react-native';
import {
  Text,
  useTheme,
  Chip,
  ActivityIndicator,
  Snackbar,
  Portal,
  IconButton,
} from 'react-native-paper';
import { router } from 'expo-router';
import { Swipeable, GestureHandlerRootView } from 'react-native-gesture-handler';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAlertStore, AlertStatus, AlertSeverity, Alert } from '@/stores/alertStore';
import { AlertCard } from '@/components/AlertCard';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';

const { width: SCREEN_WIDTH } = Dimensions.get('window');
const SWIPE_ACTION_WIDTH = 100;

type StatusFilter = AlertStatus | 'all';
type SeverityFilter = AlertSeverity | 'all';

interface FilterOption<T> {
  value: T;
  label: string;
  icon?: keyof typeof MaterialCommunityIcons.glyphMap;
  color?: string;
}

const STATUS_FILTERS: FilterOption<StatusFilter>[] = [
  { value: 'all', label: 'All' },
  { value: 'open', label: 'Open', icon: 'alert-circle-outline', color: Colors.error },
  { value: 'acknowledged', label: 'Ack\'d', icon: 'eye-check-outline', color: Colors.warning },
  { value: 'resolved', label: 'Resolved', icon: 'check-circle-outline', color: Colors.success },
];

const SEVERITY_FILTERS: FilterOption<SeverityFilter>[] = [
  { value: 'all', label: 'All Severity' },
  { value: 'critical', label: 'Critical', icon: 'alert-circle', color: Colors.critical },
  { value: 'warning', label: 'Warning', icon: 'alert', color: Colors.warning },
  { value: 'info', label: 'Info', icon: 'information', color: Colors.info },
];

export default function AlertsScreen() {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const swipeableRefs = useRef<Map<string, Swipeable>>(new Map());

  const {
    alerts,
    loading,
    refreshing,
    error,
    filters,
    openCount,
    criticalCount,
    fetchAlerts,
    refreshAlerts,
    acknowledgeAlert,
    setFilters,
    clearError,
    getFilteredAlerts,
  } = useAlertStore();

  const [snackbarMessage, setSnackbarMessage] = useState<string | null>(null);

  // Fetch alerts on mount
  useEffect(() => {
    fetchAlerts();
  }, []);

  // Get filtered alerts
  const filteredAlerts = getFilteredAlerts();

  // Handle alert press - navigate to detail
  const handleAlertPress = useCallback((alert: Alert) => {
    router.push(`/alert/${alert.id}`);
  }, []);

  // Handle swipe acknowledge
  const handleAcknowledge = useCallback(async (alert: Alert) => {
    try {
      await acknowledgeAlert(alert.id);
      setSnackbarMessage('Alert acknowledged');
      // Close the swipeable
      swipeableRefs.current.get(alert.id)?.close();
    } catch (err) {
      setSnackbarMessage('Failed to acknowledge alert');
    }
  }, [acknowledgeAlert]);

  // Render swipe action for acknowledge
  const renderRightActions = useCallback(
    (alert: Alert, progress: Animated.AnimatedInterpolation<number>) => {
      // Only show swipe action for open alerts
      if (alert.status !== 'open') {
        return null;
      }

      const translateX = progress.interpolate({
        inputRange: [0, 1],
        outputRange: [SWIPE_ACTION_WIDTH, 0],
      });

      return (
        <Animated.View
          style={[
            styles.swipeAction,
            {
              transform: [{ translateX }],
              backgroundColor: Colors.warning,
            },
          ]}
        >
          <MaterialCommunityIcons name="eye-check" size={24} color="#fff" />
          <Text style={styles.swipeActionText}>Acknowledge</Text>
        </Animated.View>
      );
    },
    []
  );

  // Render alert item with swipeable
  const renderAlertItem = useCallback(
    ({ item: alert }: { item: Alert }) => {
      const canSwipe = alert.status === 'open';

      return (
        <Swipeable
          ref={(ref) => {
            if (ref) {
              swipeableRefs.current.set(alert.id, ref);
            }
          }}
          enabled={canSwipe}
          renderRightActions={
            canSwipe
              ? (progress) => renderRightActions(alert, progress)
              : undefined
          }
          onSwipeableOpen={(direction) => {
            if (direction === 'right') {
              handleAcknowledge(alert);
            }
          }}
          rightThreshold={SWIPE_ACTION_WIDTH / 2}
          overshootRight={false}
        >
          <AlertCard alert={alert} onPress={() => handleAlertPress(alert)} />
        </Swipeable>
      );
    },
    [handleAlertPress, handleAcknowledge, renderRightActions]
  );

  // Key extractor for FlatList
  const keyExtractor = useCallback((item: Alert) => item.id, []);

  // Empty state component
  const EmptyState = () => (
    <View style={styles.emptyState}>
      <MaterialCommunityIcons
        name="bell-off-outline"
        size={64}
        color={theme.colors.onSurfaceVariant}
      />
      <Text style={[styles.emptyTitle, { color: theme.colors.onSurface }]}>
        No Alerts
      </Text>
      <Text style={[styles.emptySubtitle, { color: theme.colors.onSurfaceVariant }]}>
        {filters.status === 'all' && filters.severity === 'all'
          ? 'All systems are running smoothly'
          : 'No alerts match your current filters'}
      </Text>
    </View>
  );

  // Header component with filters
  const ListHeader = () => (
    <View style={styles.header}>
      {/* Summary stats */}
      <View style={styles.statsRow}>
        <View style={[styles.statBadge, { backgroundColor: 'rgba(239, 68, 68, 0.15)' }]}>
          <Text style={[styles.statValue, { color: Colors.critical }]}>{openCount}</Text>
          <Text style={[styles.statLabel, { color: theme.colors.onSurfaceVariant }]}>
            Open
          </Text>
        </View>
        <View style={[styles.statBadge, { backgroundColor: 'rgba(239, 68, 68, 0.15)' }]}>
          <Text style={[styles.statValue, { color: Colors.critical }]}>{criticalCount}</Text>
          <Text style={[styles.statLabel, { color: theme.colors.onSurfaceVariant }]}>
            Critical
          </Text>
        </View>
      </View>

      {/* Status filter chips */}
      <View style={styles.filterSection}>
        <Text style={[styles.filterLabel, { color: theme.colors.onSurfaceVariant }]}>
          Status
        </Text>
        <View style={styles.chipRow}>
          {STATUS_FILTERS.map((filter) => (
            <Chip
              key={filter.value}
              selected={filters.status === filter.value}
              onPress={() => setFilters({ status: filter.value })}
              mode={filters.status === filter.value ? 'flat' : 'outlined'}
              style={[
                styles.chip,
                filters.status === filter.value && {
                  backgroundColor: filter.color
                    ? `${filter.color}22`
                    : theme.colors.primaryContainer,
                },
              ]}
              textStyle={[
                styles.chipText,
                filters.status === filter.value && {
                  color: filter.color || theme.colors.primary,
                },
              ]}
              icon={filter.icon}
              compact
            >
              {filter.label}
            </Chip>
          ))}
        </View>
      </View>

      {/* Severity filter chips */}
      <View style={styles.filterSection}>
        <Text style={[styles.filterLabel, { color: theme.colors.onSurfaceVariant }]}>
          Severity
        </Text>
        <View style={styles.chipRow}>
          {SEVERITY_FILTERS.map((filter) => (
            <Chip
              key={filter.value}
              selected={filters.severity === filter.value}
              onPress={() => setFilters({ severity: filter.value })}
              mode={filters.severity === filter.value ? 'flat' : 'outlined'}
              style={[
                styles.chip,
                filters.severity === filter.value && {
                  backgroundColor: filter.color
                    ? `${filter.color}22`
                    : theme.colors.primaryContainer,
                },
              ]}
              textStyle={[
                styles.chipText,
                filters.severity === filter.value && {
                  color: filter.color || theme.colors.primary,
                },
              ]}
              icon={filter.icon}
              compact
            >
              {filter.label}
            </Chip>
          ))}
        </View>
      </View>

      {/* Results count */}
      <Text style={[styles.resultsCount, { color: theme.colors.onSurfaceVariant }]}>
        {filteredAlerts.length} {filteredAlerts.length === 1 ? 'alert' : 'alerts'}
      </Text>
    </View>
  );

  // Loading state
  if (loading && alerts.length === 0) {
    return (
      <View style={[styles.container, styles.centerContent, { backgroundColor: theme.colors.background }]}>
        <ActivityIndicator size="large" color={theme.colors.primary} />
        <Text style={[styles.loadingText, { color: theme.colors.onSurfaceVariant }]}>
          Loading alerts...
        </Text>
      </View>
    );
  }

  return (
    <View style={[styles.container, { backgroundColor: theme.colors.background }]}>
      {/* Page title */}
      <View style={[styles.titleContainer, { paddingTop: insets.top + Spacing.md }]}>
        <Text style={[styles.pageTitle, { color: theme.colors.onSurface }]}>Alerts</Text>
      </View>

      <FlatList
        data={filteredAlerts}
        renderItem={renderAlertItem}
        keyExtractor={keyExtractor}
        ListHeaderComponent={ListHeader}
        ListEmptyComponent={EmptyState}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={refreshAlerts}
            tintColor={theme.colors.primary}
            colors={[theme.colors.primary]}
          />
        }
        contentContainerStyle={[
          styles.listContent,
          filteredAlerts.length === 0 && styles.emptyListContent,
        ]}
        showsVerticalScrollIndicator={false}
      />

      {/* Snackbar for actions */}
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
            label: 'Retry',
            onPress: fetchAlerts,
          }}
          style={[styles.snackbar, { backgroundColor: Colors.error }]}
        >
          {error}
        </Snackbar>
      </Portal>
    </View>
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
  titleContainer: {
    paddingHorizontal: Spacing.md,
    paddingBottom: Spacing.sm,
  },
  pageTitle: {
    fontSize: FontSizes.xxl,
    fontWeight: '700',
  },
  loadingText: {
    marginTop: Spacing.md,
    fontSize: FontSizes.md,
  },
  header: {
    paddingHorizontal: Spacing.md,
    paddingBottom: Spacing.md,
    gap: Spacing.md,
  },
  statsRow: {
    flexDirection: 'row',
    gap: Spacing.md,
  },
  statBadge: {
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.sm,
    borderRadius: BorderRadius.md,
    alignItems: 'center',
    minWidth: 80,
  },
  statValue: {
    fontSize: FontSizes.xl,
    fontWeight: '700',
  },
  statLabel: {
    fontSize: FontSizes.xs,
    marginTop: 2,
  },
  filterSection: {
    gap: Spacing.xs,
  },
  filterLabel: {
    fontSize: FontSizes.xs,
    fontWeight: '600',
    textTransform: 'uppercase',
    letterSpacing: 0.5,
  },
  chipRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.xs,
  },
  chip: {
    height: 32,
  },
  chipText: {
    fontSize: FontSizes.sm,
  },
  resultsCount: {
    fontSize: FontSizes.sm,
    fontStyle: 'italic',
  },
  listContent: {
    paddingBottom: Spacing.xl,
  },
  emptyListContent: {
    flexGrow: 1,
  },
  emptyState: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xxl,
  },
  emptyTitle: {
    fontSize: FontSizes.lg,
    fontWeight: '600',
    marginTop: Spacing.md,
  },
  emptySubtitle: {
    fontSize: FontSizes.md,
    textAlign: 'center',
    marginTop: Spacing.xs,
  },
  swipeAction: {
    justifyContent: 'center',
    alignItems: 'center',
    width: SWIPE_ACTION_WIDTH,
    marginVertical: Spacing.xs,
    marginRight: Spacing.md,
    borderRadius: BorderRadius.md,
  },
  swipeActionText: {
    color: '#fff',
    fontSize: FontSizes.xs,
    fontWeight: '600',
    marginTop: 4,
  },
  snackbar: {
    marginBottom: 80, // Above tab bar
  },
});
