/**
 * Dashboard Screen - Sentinel Mobile
 * Overview of devices, alerts, and tickets with quick actions
 */
import React, { useEffect, useCallback } from 'react';
import {
  StyleSheet,
  View,
  ScrollView,
  RefreshControl,
  Pressable,
} from 'react-native';
import { Text, Surface, useTheme, ActivityIndicator, IconButton } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useRouter } from 'expo-router';
import { useDashboardStore } from '@/stores/dashboardStore';
import { StatCard } from '@/components/StatCard';
import { AlertListItem } from '@/components/AlertListItem';

export default function DashboardScreen() {
  const theme = useTheme();
  const router = useRouter();
  const {
    stats,
    recentAlerts,
    loading,
    refreshing,
    error,
    lastUpdated,
    fetchDashboardData,
    refresh,
  } = useDashboardStore();

  // Fetch data on mount
  useEffect(() => {
    fetchDashboardData();
  }, []);

  // Pull-to-refresh handler
  const handleRefresh = useCallback(() => {
    refresh();
  }, [refresh]);

  // Navigation handlers
  const navigateToDevices = useCallback(() => {
    router.push('/(tabs)/devices');
  }, [router]);

  const navigateToAlerts = useCallback(() => {
    router.push('/(tabs)/alerts');
  }, [router]);

  const navigateToTickets = useCallback(() => {
    router.push('/(tabs)/tickets');
  }, [router]);

  const navigateToAlert = useCallback((id: string) => {
    router.push(`/alert/${id}`);
  }, [router]);

  // Format last updated time
  const lastUpdatedText = lastUpdated
    ? `Updated ${lastUpdated.toLocaleTimeString()}`
    : '';

  if (loading && !refreshing) {
    return (
      <View style={[styles.loadingContainer, { backgroundColor: theme.colors.background }]}>
        <ActivityIndicator size="large" color={theme.colors.primary} />
        <Text style={[styles.loadingText, { color: theme.colors.onSurfaceVariant }]}>
          Loading dashboard...
        </Text>
      </View>
    );
  }

  return (
    <ScrollView
      style={[styles.container, { backgroundColor: theme.colors.background }]}
      contentContainerStyle={styles.content}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={handleRefresh}
          tintColor={theme.colors.primary}
          colors={[theme.colors.primary]}
        />
      }
    >
      {/* Error Banner */}
      {error && (
        <Surface style={[styles.errorBanner, { backgroundColor: '#ef4444' }]} elevation={1}>
          <MaterialCommunityIcons name="alert-circle" size={20} color="#ffffff" />
          <Text style={styles.errorText}>{error}</Text>
          <IconButton
            icon="refresh"
            iconColor="#ffffff"
            size={20}
            onPress={() => fetchDashboardData()}
          />
        </Surface>
      )}

      {/* Last Updated */}
      {lastUpdatedText && (
        <Text style={[styles.lastUpdated, { color: theme.colors.onSurfaceVariant }]}>
          {lastUpdatedText}
        </Text>
      )}

      {/* Stats Grid */}
      <View style={styles.statsGrid}>
        <StatCard
          label="Total Devices"
          value={stats.deviceCount}
          subtitle={`${stats.onlineCount} online`}
          icon="desktop-tower-monitor"
          color="info"
          onPress={navigateToDevices}
        />
        <StatCard
          label="Online"
          value={stats.onlineCount}
          icon="check-circle"
          color="success"
          onPress={navigateToDevices}
        />
        <StatCard
          label="Offline"
          value={stats.offlineCount}
          icon="power-plug-off"
          color="gray"
          onPress={navigateToDevices}
        />
        <StatCard
          label="Open Alerts"
          value={stats.alertCounts.open}
          subtitle={stats.alertCounts.critical > 0 ? `${stats.alertCounts.critical} critical` : undefined}
          icon="bell-alert"
          color={stats.alertCounts.critical > 0 ? 'error' : 'warning'}
          onPress={navigateToAlerts}
        />
      </View>

      {/* Recent Alerts Section */}
      <View style={styles.section}>
        <View style={styles.sectionHeader}>
          <Text style={[styles.sectionTitle, { color: theme.colors.onSurface }]}>
            Recent Alerts
          </Text>
          <Pressable onPress={navigateToAlerts} style={styles.viewAllButton}>
            <Text style={[styles.viewAllText, { color: theme.colors.primary }]}>
              View All
            </Text>
            <MaterialCommunityIcons
              name="chevron-right"
              size={18}
              color={theme.colors.primary}
            />
          </Pressable>
        </View>

        {recentAlerts.length === 0 ? (
          <Surface style={[styles.emptyState, { backgroundColor: theme.colors.surface }]} elevation={1}>
            <MaterialCommunityIcons
              name="bell-check"
              size={48}
              color={theme.colors.onSurfaceVariant}
            />
            <Text style={[styles.emptyStateText, { color: theme.colors.onSurfaceVariant }]}>
              No open alerts
            </Text>
            <Text style={[styles.emptyStateSubtext, { color: theme.colors.onSurfaceVariant }]}>
              All systems are running smoothly
            </Text>
          </Surface>
        ) : (
          <View style={styles.alertList}>
            {recentAlerts.map((alert) => (
              <AlertListItem
                key={alert.id}
                id={alert.id}
                title={alert.title}
                message={alert.message}
                severity={alert.severity}
                deviceName={alert.deviceName}
                createdAt={alert.createdAt}
                onPress={() => navigateToAlert(alert.id)}
              />
            ))}
          </View>
        )}
      </View>

      {/* Quick Actions Section */}
      <View style={styles.section}>
        <Text style={[styles.sectionTitle, { color: theme.colors.onSurface }]}>
          Quick Actions
        </Text>
        <View style={styles.quickActions}>
          <QuickActionButton
            icon="refresh"
            label="Refresh"
            onPress={handleRefresh}
            theme={theme}
          />
          <QuickActionButton
            icon="desktop-tower-monitor"
            label="Devices"
            onPress={navigateToDevices}
            theme={theme}
          />
          <QuickActionButton
            icon="bell"
            label="Alerts"
            onPress={navigateToAlerts}
            theme={theme}
          />
          <QuickActionButton
            icon="ticket"
            label="Tickets"
            onPress={navigateToTickets}
            theme={theme}
          />
        </View>
      </View>
    </ScrollView>
  );
}

// Quick action button component
interface QuickActionButtonProps {
  icon: keyof typeof MaterialCommunityIcons.glyphMap;
  label: string;
  onPress: () => void;
  theme: ReturnType<typeof useTheme>;
}

function QuickActionButton({ icon, label, onPress, theme }: QuickActionButtonProps) {
  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        styles.quickActionButton,
        { backgroundColor: theme.colors.surface },
        pressed && styles.pressed,
      ]}
    >
      <View style={[styles.quickActionIcon, { backgroundColor: 'rgba(16, 185, 129, 0.15)' }]}>
        <MaterialCommunityIcons name={icon} size={22} color="#10b981" />
      </View>
      <Text style={[styles.quickActionLabel, { color: theme.colors.onSurface }]}>
        {label}
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  content: {
    paddingVertical: 16,
  },
  loadingContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  loadingText: {
    marginTop: 12,
    fontSize: 14,
  },
  errorBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    marginHorizontal: 16,
    marginBottom: 12,
    paddingVertical: 8,
    paddingHorizontal: 12,
    borderRadius: 8,
  },
  errorText: {
    flex: 1,
    color: '#ffffff',
    fontSize: 14,
    marginLeft: 8,
  },
  lastUpdated: {
    fontSize: 12,
    textAlign: 'center',
    marginBottom: 12,
  },
  statsGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    paddingHorizontal: 12,
    gap: 12,
  },
  section: {
    marginTop: 24,
  },
  sectionHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: 16,
    marginBottom: 12,
  },
  sectionTitle: {
    fontSize: 18,
    fontWeight: '600',
  },
  viewAllButton: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  viewAllText: {
    fontSize: 14,
    fontWeight: '500',
  },
  alertList: {
    gap: 4,
  },
  emptyState: {
    marginHorizontal: 16,
    paddingVertical: 32,
    borderRadius: 12,
    alignItems: 'center',
  },
  emptyStateText: {
    fontSize: 16,
    fontWeight: '600',
    marginTop: 12,
  },
  emptyStateSubtext: {
    fontSize: 14,
    marginTop: 4,
  },
  quickActions: {
    flexDirection: 'row',
    paddingHorizontal: 16,
    gap: 12,
  },
  quickActionButton: {
    flex: 1,
    alignItems: 'center',
    paddingVertical: 16,
    borderRadius: 12,
  },
  pressed: {
    opacity: 0.7,
  },
  quickActionIcon: {
    width: 44,
    height: 44,
    borderRadius: 22,
    justifyContent: 'center',
    alignItems: 'center',
    marginBottom: 8,
  },
  quickActionLabel: {
    fontSize: 12,
    fontWeight: '500',
  },
});
