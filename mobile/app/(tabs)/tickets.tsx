/**
 * Tickets Screen - Ticket list with filtering and search
 * Displays all tickets with pull-to-refresh and filter chips
 */
import React, { useEffect, useCallback, useState, useMemo } from 'react';
import {
  View,
  StyleSheet,
  FlatList,
  RefreshControl,
} from 'react-native';
import {
  Text,
  useTheme,
  Chip,
  Searchbar,
  FAB,
  ActivityIndicator,
  Snackbar,
  Portal,
  Modal,
  TextInput,
  Button,
  SegmentedButtons,
} from 'react-native-paper';
import { router } from 'expo-router';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useTicketStore, Ticket } from '@/stores/ticketStore';
import { TicketListItem } from '@/components/TicketListItem';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';

type StatusFilter = 'all' | 'open' | 'in_progress' | 'waiting' | 'resolved' | 'closed';

interface FilterOption<T> {
  value: T;
  label: string;
  color?: string;
}

const STATUS_FILTERS: FilterOption<StatusFilter>[] = [
  { value: 'all', label: 'All' },
  { value: 'open', label: 'Open', color: Colors.ticketOpen },
  { value: 'in_progress', label: 'In Progress', color: Colors.ticketInProgress },
  { value: 'waiting', label: 'Waiting', color: Colors.warning },
  { value: 'resolved', label: 'Resolved', color: Colors.ticketResolved },
  { value: 'closed', label: 'Closed', color: Colors.ticketClosed },
];

const PRIORITY_OPTIONS = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'urgent', label: 'Urgent' },
];

export default function TicketsScreen() {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  const {
    tickets,
    stats,
    loading,
    refreshing,
    submitting,
    error,
    filters,
    fetchTickets,
    refreshTickets,
    fetchStats,
    createTicket,
    setStatusFilter,
    setSearchFilter,
    clearError,
  } = useTicketStore();

  // Local state
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedStatus, setSelectedStatus] = useState<StatusFilter>('all');
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [snackbarMessage, setSnackbarMessage] = useState<string | null>(null);

  // Create ticket form state
  const [newTicket, setNewTicket] = useState({
    subject: '',
    description: '',
    priority: 'medium' as 'low' | 'medium' | 'high' | 'urgent',
    type: 'incident' as 'incident' | 'request' | 'problem' | 'change',
  });

  // Fetch tickets and stats on mount
  useEffect(() => {
    fetchTickets();
    fetchStats();
  }, []);

  // Debounced search
  useEffect(() => {
    const timer = setTimeout(() => {
      if (searchQuery !== filters.search) {
        setSearchFilter(searchQuery);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [searchQuery]);

  // Filter tickets locally for status (since API might not support all filters)
  const filteredTickets = useMemo(() => {
    let result = tickets;

    // Apply status filter
    if (selectedStatus !== 'all') {
      result = result.filter((t) => t.status === selectedStatus);
    }

    // Apply search filter (client-side backup)
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase();
      result = result.filter(
        (t) =>
          t.subject.toLowerCase().includes(query) ||
          t.ticketNumber.toString().includes(query) ||
          t.requesterName?.toLowerCase().includes(query)
      );
    }

    return result;
  }, [tickets, selectedStatus, searchQuery]);

  // Handle status filter change
  const handleStatusChange = useCallback((status: StatusFilter) => {
    setSelectedStatus(status);
    if (status === 'all') {
      setStatusFilter(undefined);
    } else {
      setStatusFilter(status);
    }
  }, [setStatusFilter]);

  // Handle ticket press - navigate to detail
  const handleTicketPress = useCallback((ticket: Ticket) => {
    router.push(`/ticket/${ticket.id}`);
  }, []);

  // Handle create ticket
  const handleCreateTicket = async () => {
    if (!newTicket.subject.trim()) {
      setSnackbarMessage('Please enter a subject');
      return;
    }

    try {
      await createTicket(newTicket);
      setShowCreateModal(false);
      setNewTicket({
        subject: '',
        description: '',
        priority: 'medium',
        type: 'incident',
      });
      setSnackbarMessage('Ticket created successfully');
      fetchStats(); // Refresh stats
    } catch (err) {
      setSnackbarMessage('Failed to create ticket');
    }
  };

  // Reset create form
  const resetCreateForm = () => {
    setNewTicket({
      subject: '',
      description: '',
      priority: 'medium',
      type: 'incident',
    });
    setShowCreateModal(false);
  };

  // Render ticket item
  const renderTicketItem = useCallback(
    ({ item }: { item: Ticket }) => (
      <TicketListItem ticket={item} onPress={handleTicketPress} />
    ),
    [handleTicketPress]
  );

  // Key extractor
  const keyExtractor = useCallback((item: Ticket) => item.id, []);

  // Empty state
  const EmptyState = () => (
    <View style={styles.emptyState}>
      <MaterialCommunityIcons
        name="ticket-outline"
        size={64}
        color={theme.colors.onSurfaceVariant}
      />
      <Text style={[styles.emptyTitle, { color: theme.colors.onSurface }]}>
        No Tickets
      </Text>
      <Text style={[styles.emptySubtitle, { color: theme.colors.onSurfaceVariant }]}>
        {selectedStatus === 'all' && !searchQuery
          ? 'No tickets yet. Create your first ticket!'
          : 'No tickets match your current filters'}
      </Text>
    </View>
  );

  // Header component with stats, search, and filters
  const ListHeader = () => (
    <View style={styles.header}>
      {/* Stats row */}
      {stats && (
        <View style={styles.statsRow}>
          <View style={[styles.statBadge, { backgroundColor: 'rgba(59, 130, 246, 0.15)' }]}>
            <Text style={[styles.statValue, { color: Colors.ticketOpen }]}>
              {stats.openCount}
            </Text>
            <Text style={[styles.statLabel, { color: theme.colors.onSurfaceVariant }]}>
              Open
            </Text>
          </View>
          <View style={[styles.statBadge, { backgroundColor: 'rgba(139, 92, 246, 0.15)' }]}>
            <Text style={[styles.statValue, { color: Colors.ticketInProgress }]}>
              {stats.inProgressCount}
            </Text>
            <Text style={[styles.statLabel, { color: theme.colors.onSurfaceVariant }]}>
              In Progress
            </Text>
          </View>
          <View style={[styles.statBadge, { backgroundColor: 'rgba(34, 197, 94, 0.15)' }]}>
            <Text style={[styles.statValue, { color: Colors.ticketResolved }]}>
              {stats.resolvedCount}
            </Text>
            <Text style={[styles.statLabel, { color: theme.colors.onSurfaceVariant }]}>
              Resolved
            </Text>
          </View>
        </View>
      )}

      {/* Search bar */}
      <Searchbar
        placeholder="Search tickets..."
        onChangeText={setSearchQuery}
        value={searchQuery}
        style={styles.searchbar}
        inputStyle={styles.searchbarInput}
        iconColor={theme.colors.onSurfaceVariant}
        placeholderTextColor={theme.colors.onSurfaceVariant}
      />

      {/* Status filter chips */}
      <View style={styles.filterSection}>
        <Text style={[styles.filterLabel, { color: theme.colors.onSurfaceVariant }]}>
          Status
        </Text>
        <View style={styles.chipRow}>
          {STATUS_FILTERS.map((filter) => (
            <Chip
              key={filter.value}
              selected={selectedStatus === filter.value}
              onPress={() => handleStatusChange(filter.value)}
              mode={selectedStatus === filter.value ? 'flat' : 'outlined'}
              style={[
                styles.chip,
                selectedStatus === filter.value && {
                  backgroundColor: filter.color
                    ? `${filter.color}22`
                    : theme.colors.primaryContainer,
                },
              ]}
              textStyle={[
                styles.chipText,
                selectedStatus === filter.value && {
                  color: filter.color || theme.colors.primary,
                },
              ]}
              compact
            >
              {filter.label}
            </Chip>
          ))}
        </View>
      </View>

      {/* Results count */}
      <Text style={[styles.resultsCount, { color: theme.colors.onSurfaceVariant }]}>
        {filteredTickets.length} {filteredTickets.length === 1 ? 'ticket' : 'tickets'}
      </Text>
    </View>
  );

  // Loading state
  if (loading && tickets.length === 0) {
    return (
      <View style={[styles.container, styles.centerContent, { backgroundColor: theme.colors.background }]}>
        <ActivityIndicator size="large" color={theme.colors.primary} />
        <Text style={[styles.loadingText, { color: theme.colors.onSurfaceVariant }]}>
          Loading tickets...
        </Text>
      </View>
    );
  }

  return (
    <View style={[styles.container, { backgroundColor: theme.colors.background }]}>
      {/* Page title */}
      <View style={[styles.titleContainer, { paddingTop: insets.top + Spacing.md }]}>
        <Text style={[styles.pageTitle, { color: theme.colors.onSurface }]}>Tickets</Text>
      </View>

      {/* Ticket list */}
      <FlatList
        data={filteredTickets}
        renderItem={renderTicketItem}
        keyExtractor={keyExtractor}
        ListHeaderComponent={ListHeader}
        ListEmptyComponent={EmptyState}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={refreshTickets}
            tintColor={theme.colors.primary}
            colors={[theme.colors.primary]}
          />
        }
        contentContainerStyle={[
          styles.listContent,
          filteredTickets.length === 0 && styles.emptyListContent,
        ]}
        showsVerticalScrollIndicator={false}
      />

      {/* FAB to create new ticket */}
      <FAB
        icon="plus"
        style={[styles.fab, { backgroundColor: Colors.primary }]}
        color="#ffffff"
        onPress={() => setShowCreateModal(true)}
      />

      {/* Create Ticket Modal */}
      <Portal>
        <Modal
          visible={showCreateModal}
          onDismiss={resetCreateForm}
          contentContainerStyle={[styles.modal, { backgroundColor: theme.colors.surface }]}
        >
          <Text style={[styles.modalTitle, { color: theme.colors.onSurface }]}>
            Create New Ticket
          </Text>

          <TextInput
            label="Subject"
            value={newTicket.subject}
            onChangeText={(text) => setNewTicket({ ...newTicket, subject: text })}
            mode="outlined"
            style={styles.modalInput}
            placeholder="Brief description of the issue"
          />

          <TextInput
            label="Description"
            value={newTicket.description}
            onChangeText={(text) => setNewTicket({ ...newTicket, description: text })}
            mode="outlined"
            multiline
            numberOfLines={4}
            style={[styles.modalInput, styles.descriptionInput]}
            placeholder="Detailed description..."
          />

          <Text style={[styles.inputLabel, { color: theme.colors.onSurfaceVariant }]}>
            Priority
          </Text>
          <SegmentedButtons
            value={newTicket.priority}
            onValueChange={(value) =>
              setNewTicket({ ...newTicket, priority: value as typeof newTicket.priority })
            }
            buttons={PRIORITY_OPTIONS}
            style={styles.segmentedButtons}
          />

          <View style={styles.modalActions}>
            <Button mode="outlined" onPress={resetCreateForm} style={styles.modalButton}>
              Cancel
            </Button>
            <Button
              mode="contained"
              onPress={handleCreateTicket}
              loading={submitting}
              disabled={submitting || !newTicket.subject.trim()}
              style={styles.modalButton}
            >
              Create
            </Button>
          </View>
        </Modal>
      </Portal>

      {/* Snackbar for messages */}
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
            onPress: fetchTickets,
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
    flex: 1,
  },
  statValue: {
    fontSize: FontSizes.xl,
    fontWeight: '700',
  },
  statLabel: {
    fontSize: FontSizes.xs,
    marginTop: 2,
  },
  searchbar: {
    backgroundColor: Colors.dark.surfaceVariant,
    borderRadius: BorderRadius.lg,
    elevation: 0,
  },
  searchbarInput: {
    fontSize: FontSizes.md,
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
    paddingBottom: 100, // Space for FAB
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
  fab: {
    position: 'absolute',
    right: Spacing.md,
    bottom: Spacing.xl,
    borderRadius: BorderRadius.full,
  },
  modal: {
    margin: Spacing.lg,
    padding: Spacing.lg,
    borderRadius: BorderRadius.lg,
  },
  modalTitle: {
    fontSize: FontSizes.xl,
    fontWeight: '600',
    marginBottom: Spacing.lg,
  },
  modalInput: {
    marginBottom: Spacing.md,
    backgroundColor: Colors.dark.surfaceVariant,
  },
  descriptionInput: {
    minHeight: 100,
  },
  inputLabel: {
    fontSize: FontSizes.sm,
    fontWeight: '500',
    marginBottom: Spacing.xs,
  },
  segmentedButtons: {
    marginBottom: Spacing.lg,
  },
  modalActions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: Spacing.sm,
    marginTop: Spacing.md,
  },
  modalButton: {
    minWidth: 100,
  },
  snackbar: {
    marginBottom: 80, // Above tab bar
  },
});
