/**
 * Devices Screen - Device List Tab
 * Shows searchable, filterable list of all devices
 */
import React, { useEffect, useCallback } from 'react';
import {
  View,
  StyleSheet,
  FlatList,
  RefreshControl,
  useColorScheme,
} from 'react-native';
import { Searchbar, Chip, Text, ActivityIndicator, Snackbar } from 'react-native-paper';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';
import { useDeviceStore, Device, StatusFilter } from '@/stores/deviceStore';
import { DeviceListItem } from '@/components/DeviceListItem';

const statusFilters: { key: StatusFilter; label: string; icon: keyof typeof MaterialCommunityIcons.glyphMap }[] = [
  { key: 'all', label: 'All', icon: 'format-list-bulleted' },
  { key: 'online', label: 'Online', icon: 'check-circle' },
  { key: 'offline', label: 'Offline', icon: 'close-circle' },
  { key: 'warning', label: 'Warning', icon: 'alert-circle' },
];

export default function DevicesScreen() {
  const router = useRouter();
  const colorScheme = useColorScheme();
  const isDark = colorScheme === 'dark';
  const themeColors = isDark ? Colors.dark : Colors.light;

  const {
    devices,
    loading,
    refreshing,
    error,
    searchQuery,
    statusFilter,
    fetchDevices,
    refreshDevices,
    setSearchQuery,
    setStatusFilter,
    clearError,
    filteredDevices,
  } = useDeviceStore();

  // Initial fetch
  useEffect(() => {
    fetchDevices();
  }, []);

  const handleRefresh = useCallback(() => {
    refreshDevices();
  }, [refreshDevices]);

  const handleDevicePress = useCallback((device: Device) => {
    router.push(`/device/${device.id}`);
  }, [router]);

  const handleFilterPress = useCallback((filter: StatusFilter) => {
    setStatusFilter(filter);
  }, [setStatusFilter]);

  // Get filtered devices
  const displayDevices = filteredDevices();

  // Get counts for chips
  const onlineCount = devices.filter(d => d.status === 'online').length;
  const offlineCount = devices.filter(d => d.status === 'offline').length;
  const warningCount = devices.filter(d => d.status === 'warning' || d.status === 'critical').length;

  const getChipCount = (filter: StatusFilter): number => {
    switch (filter) {
      case 'all': return devices.length;
      case 'online': return onlineCount;
      case 'offline': return offlineCount;
      case 'warning': return warningCount;
      default: return 0;
    }
  };

  const renderEmptyState = () => (
    <View style={styles.emptyContainer}>
      <MaterialCommunityIcons
        name="server-off"
        size={64}
        color={Colors.dark.textMuted}
      />
      <Text style={[styles.emptyTitle, { color: themeColors.text }]}>
        No devices found
      </Text>
      <Text style={[styles.emptySubtitle, { color: themeColors.textSecondary }]}>
        {searchQuery || statusFilter !== 'all'
          ? 'Try adjusting your filters'
          : 'No devices have been registered yet'}
      </Text>
    </View>
  );

  const renderItem = useCallback(({ item }: { item: Device }) => (
    <DeviceListItem device={item} onPress={handleDevicePress} />
  ), [handleDevicePress]);

  const keyExtractor = useCallback((item: Device) => item.id, []);

  return (
    <SafeAreaView style={[styles.container, { backgroundColor: themeColors.background }]} edges={['top']}>
      {/* Header */}
      <View style={styles.header}>
        <Text style={[styles.title, { color: themeColors.text }]}>Devices</Text>
        <Text style={[styles.subtitle, { color: themeColors.textSecondary }]}>
          {displayDevices.length} of {devices.length} devices
        </Text>
      </View>

      {/* Search Bar */}
      <View style={styles.searchContainer}>
        <Searchbar
          placeholder="Search by name or IP..."
          onChangeText={setSearchQuery}
          value={searchQuery}
          style={[styles.searchBar, { backgroundColor: themeColors.surface }]}
          inputStyle={{ color: themeColors.text }}
          iconColor={themeColors.textSecondary}
          placeholderTextColor={themeColors.textMuted}
        />
      </View>

      {/* Filter Chips */}
      <View style={styles.chipsContainer}>
        <FlatList
          horizontal
          data={statusFilters}
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={styles.chipsList}
          keyExtractor={(item) => item.key}
          renderItem={({ item }) => {
            const isSelected = statusFilter === item.key;
            const count = getChipCount(item.key);
            return (
              <Chip
                mode={isSelected ? 'flat' : 'outlined'}
                selected={isSelected}
                onPress={() => handleFilterPress(item.key)}
                style={[
                  styles.chip,
                  isSelected && { backgroundColor: Colors.primary }
                ]}
                textStyle={[
                  styles.chipText,
                  { color: isSelected ? '#FFFFFF' : themeColors.textSecondary }
                ]}
                icon={() => (
                  <MaterialCommunityIcons
                    name={item.icon}
                    size={16}
                    color={isSelected ? '#FFFFFF' : themeColors.textSecondary}
                  />
                )}
              >
                {item.label} ({count})
              </Chip>
            );
          }}
        />
      </View>

      {/* Device List */}
      {loading && !refreshing ? (
        <View style={styles.loadingContainer}>
          <ActivityIndicator size="large" color={Colors.primary} />
          <Text style={[styles.loadingText, { color: themeColors.textSecondary }]}>
            Loading devices...
          </Text>
        </View>
      ) : (
        <FlatList
          data={displayDevices}
          renderItem={renderItem}
          keyExtractor={keyExtractor}
          contentContainerStyle={displayDevices.length === 0 ? styles.emptyList : styles.list}
          ListEmptyComponent={renderEmptyState}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              onRefresh={handleRefresh}
              tintColor={Colors.primary}
              colors={[Colors.primary]}
            />
          }
          showsVerticalScrollIndicator={false}
        />
      )}

      {/* Error Snackbar */}
      <Snackbar
        visible={!!error}
        onDismiss={clearError}
        duration={4000}
        action={{
          label: 'Dismiss',
          onPress: clearError,
        }}
        style={styles.snackbar}
      >
        {error}
      </Snackbar>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    paddingHorizontal: Spacing.md,
    paddingTop: Spacing.md,
    paddingBottom: Spacing.sm,
  },
  title: {
    fontSize: FontSizes.xxl,
    fontWeight: 'bold',
  },
  subtitle: {
    fontSize: FontSizes.sm,
    marginTop: Spacing.xs,
  },
  searchContainer: {
    paddingHorizontal: Spacing.md,
    paddingBottom: Spacing.sm,
  },
  searchBar: {
    borderRadius: BorderRadius.md,
    elevation: 0,
  },
  chipsContainer: {
    paddingBottom: Spacing.sm,
  },
  chipsList: {
    paddingHorizontal: Spacing.md,
    gap: Spacing.sm,
  },
  chip: {
    marginRight: Spacing.sm,
    borderRadius: BorderRadius.full,
  },
  chipText: {
    fontSize: FontSizes.sm,
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
  list: {
    paddingBottom: Spacing.xl,
  },
  emptyList: {
    flex: 1,
  },
  emptyContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: Spacing.xl,
  },
  emptyTitle: {
    fontSize: FontSizes.lg,
    fontWeight: '600',
    marginTop: Spacing.lg,
  },
  emptySubtitle: {
    fontSize: FontSizes.md,
    marginTop: Spacing.sm,
    textAlign: 'center',
  },
  snackbar: {
    backgroundColor: Colors.error,
  },
});
