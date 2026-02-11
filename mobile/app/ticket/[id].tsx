/**
 * Ticket Detail Screen - View and manage a single ticket
 * Shows ticket info, comments, and allows status updates
 */
import React, { useEffect, useState, useRef } from 'react';
import {
  View,
  StyleSheet,
  ScrollView,
  KeyboardAvoidingView,
  Platform,
  RefreshControl,
} from 'react-native';
import {
  Text,
  Surface,
  useTheme,
  ActivityIndicator,
  Chip,
  Divider,
  Menu,
  Button,
  Snackbar,
  Portal,
} from 'react-native-paper';
import { useLocalSearchParams, Stack, router } from 'expo-router';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useTicketStore, Ticket } from '@/stores/ticketStore';
import { useAuthStore } from '@/stores/authStore';
import { CommentBubble } from '@/components/CommentBubble';
import { CommentInput } from '@/components/CommentInput';
import { Colors, Spacing, BorderRadius, FontSizes } from '@/constants/theme';

// Format ticket number
const formatTicketNumber = (num: number): string => {
  return `TKT-${String(num).padStart(6, '0')}`;
};

// Format date
const formatDate = (dateString: string): string => {
  const date = new Date(dateString);
  return date.toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

// Format relative time
const formatRelativeTime = (dateString: string): string => {
  const date = new Date(dateString);
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const hours = Math.floor(diff / (1000 * 60 * 60));
  const days = Math.floor(hours / 24);

  if (hours < 1) return 'Just now';
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  return formatDate(dateString);
};

// Status options
const STATUS_OPTIONS = [
  { value: 'open', label: 'Open', color: Colors.ticketOpen },
  { value: 'in_progress', label: 'In Progress', color: Colors.ticketInProgress },
  { value: 'waiting', label: 'Waiting', color: Colors.warning },
  { value: 'resolved', label: 'Resolved', color: Colors.ticketResolved },
  { value: 'closed', label: 'Closed', color: Colors.ticketClosed },
];

// Priority colors
const getPriorityColor = (priority: string): string => {
  switch (priority) {
    case 'urgent':
      return Colors.priorityCritical;
    case 'high':
      return Colors.priorityHigh;
    case 'medium':
      return Colors.priorityMedium;
    case 'low':
      return Colors.priorityLow;
    default:
      return Colors.priorityMedium;
  }
};

// Get status color
const getStatusColor = (status: string): string => {
  const option = STATUS_OPTIONS.find((o) => o.value === status);
  return option?.color || Colors.ticketOpen;
};

// Get status label
const getStatusLabel = (status: string): string => {
  const option = STATUS_OPTIONS.find((o) => o.value === status);
  return option?.label || status;
};

export default function TicketDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const theme = useTheme();
  const scrollViewRef = useRef<ScrollView>(null);

  const { user } = useAuthStore();
  const {
    selectedTicket: ticket,
    comments,
    loading,
    loadingComments,
    submitting,
    error,
    fetchTicket,
    updateTicket,
    addComment,
    clearSelectedTicket,
    clearError,
  } = useTicketStore();

  const [refreshing, setRefreshing] = useState(false);
  const [statusMenuVisible, setStatusMenuVisible] = useState(false);
  const [snackbarMessage, setSnackbarMessage] = useState<string | null>(null);

  // Fetch ticket on mount
  useEffect(() => {
    if (id) {
      fetchTicket(id);
    }
    return () => {
      clearSelectedTicket();
    };
  }, [id]);

  // Handle refresh
  const handleRefresh = async () => {
    if (!id) return;
    setRefreshing(true);
    await fetchTicket(id);
    setRefreshing(false);
  };

  // Handle status change
  const handleStatusChange = async (newStatus: string) => {
    if (!ticket) return;
    setStatusMenuVisible(false);

    try {
      await updateTicket(ticket.id, { status: newStatus as Ticket['status'] });
      setSnackbarMessage('Status updated successfully');
    } catch (err) {
      setSnackbarMessage('Failed to update status');
    }
  };

  // Handle add comment
  const handleAddComment = async (content: string) => {
    if (!ticket) return;

    try {
      await addComment(ticket.id, content);
      // Scroll to bottom after adding comment
      setTimeout(() => {
        scrollViewRef.current?.scrollToEnd({ animated: true });
      }, 100);
    } catch (err) {
      throw err; // Let CommentInput handle the error
    }
  };

  // Loading state
  if (loading && !ticket) {
    return (
      <View style={[styles.container, styles.centerContent, { backgroundColor: theme.colors.background }]}>
        <Stack.Screen
          options={{
            title: 'Loading...',
          }}
        />
        <ActivityIndicator size="large" color={theme.colors.primary} />
        <Text style={[styles.loadingText, { color: theme.colors.onSurfaceVariant }]}>
          Loading ticket...
        </Text>
      </View>
    );
  }

  // Error or not found
  if (!ticket && !loading) {
    return (
      <View style={[styles.container, styles.centerContent, { backgroundColor: theme.colors.background }]}>
        <Stack.Screen
          options={{
            title: 'Not Found',
          }}
        />
        <MaterialCommunityIcons
          name="ticket-outline"
          size={64}
          color={theme.colors.onSurfaceVariant}
        />
        <Text style={[styles.errorTitle, { color: theme.colors.onSurface }]}>
          Ticket Not Found
        </Text>
        <Button mode="contained" onPress={() => router.back()} style={styles.backButton}>
          Go Back
        </Button>
      </View>
    );
  }

  if (!ticket) return null;

  const statusColor = getStatusColor(ticket.status);
  const priorityColor = getPriorityColor(ticket.priority);

  return (
    <KeyboardAvoidingView
      style={[styles.container, { backgroundColor: theme.colors.background }]}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      keyboardVerticalOffset={Platform.OS === 'ios' ? 90 : 0}
    >
      <Stack.Screen
        options={{
          title: formatTicketNumber(ticket.ticketNumber),
          headerRight: () => (
            <Menu
              visible={statusMenuVisible}
              onDismiss={() => setStatusMenuVisible(false)}
              anchor={
                <Button
                  mode="text"
                  onPress={() => setStatusMenuVisible(true)}
                  compact
                  textColor={statusColor}
                >
                  {getStatusLabel(ticket.status)}
                </Button>
              }
            >
              {STATUS_OPTIONS.map((option) => (
                <Menu.Item
                  key={option.value}
                  onPress={() => handleStatusChange(option.value)}
                  title={option.label}
                  leadingIcon={() => (
                    <View
                      style={[styles.statusDot, { backgroundColor: option.color }]}
                    />
                  )}
                  disabled={ticket.status === option.value}
                />
              ))}
            </Menu>
          ),
        }}
      />

      <ScrollView
        ref={scrollViewRef}
        style={styles.scrollView}
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
        {/* Ticket Header */}
        <Surface style={styles.headerCard} elevation={1}>
          {/* Status and Priority row */}
          <View style={styles.badgeRow}>
            <Chip
              mode="flat"
              style={[styles.statusChip, { backgroundColor: `${statusColor}22` }]}
              textStyle={{ color: statusColor, fontWeight: '600' }}
            >
              {getStatusLabel(ticket.status)}
            </Chip>
            <Chip
              mode="flat"
              style={[styles.priorityChip, { backgroundColor: `${priorityColor}22` }]}
              textStyle={{ color: priorityColor, fontWeight: '600' }}
              icon={() => (
                <MaterialCommunityIcons
                  name={
                    ticket.priority === 'urgent'
                      ? 'alert-circle'
                      : ticket.priority === 'high'
                      ? 'arrow-up-bold'
                      : ticket.priority === 'medium'
                      ? 'minus'
                      : 'arrow-down-bold'
                  }
                  size={16}
                  color={priorityColor}
                />
              )}
            >
              {ticket.priority.charAt(0).toUpperCase() + ticket.priority.slice(1)}
            </Chip>
          </View>

          {/* Subject */}
          <Text style={[styles.subject, { color: theme.colors.onSurface }]}>
            {ticket.subject}
          </Text>

          {/* Description */}
          {ticket.description && (
            <Text style={[styles.description, { color: theme.colors.onSurfaceVariant }]}>
              {ticket.description}
            </Text>
          )}

          <Divider style={styles.divider} />

          {/* Meta info */}
          <View style={styles.metaGrid}>
            {/* Requester */}
            <View style={styles.metaItem}>
              <MaterialCommunityIcons
                name="account-outline"
                size={18}
                color={theme.colors.onSurfaceVariant}
              />
              <View style={styles.metaText}>
                <Text style={[styles.metaLabel, { color: theme.colors.onSurfaceVariant }]}>
                  Requester
                </Text>
                <Text style={[styles.metaValue, { color: theme.colors.onSurface }]}>
                  {ticket.requesterName || 'Unknown'}
                </Text>
              </View>
            </View>

            {/* Assigned To */}
            <View style={styles.metaItem}>
              <MaterialCommunityIcons
                name="account-check-outline"
                size={18}
                color={theme.colors.onSurfaceVariant}
              />
              <View style={styles.metaText}>
                <Text style={[styles.metaLabel, { color: theme.colors.onSurfaceVariant }]}>
                  Assigned To
                </Text>
                <Text style={[styles.metaValue, { color: theme.colors.onSurface }]}>
                  {ticket.assignedTo || 'Unassigned'}
                </Text>
              </View>
            </View>

            {/* Created */}
            <View style={styles.metaItem}>
              <MaterialCommunityIcons
                name="calendar-plus"
                size={18}
                color={theme.colors.onSurfaceVariant}
              />
              <View style={styles.metaText}>
                <Text style={[styles.metaLabel, { color: theme.colors.onSurfaceVariant }]}>
                  Created
                </Text>
                <Text style={[styles.metaValue, { color: theme.colors.onSurface }]}>
                  {formatRelativeTime(ticket.createdAt)}
                </Text>
              </View>
            </View>

            {/* Updated */}
            <View style={styles.metaItem}>
              <MaterialCommunityIcons
                name="calendar-edit"
                size={18}
                color={theme.colors.onSurfaceVariant}
              />
              <View style={styles.metaText}>
                <Text style={[styles.metaLabel, { color: theme.colors.onSurfaceVariant }]}>
                  Updated
                </Text>
                <Text style={[styles.metaValue, { color: theme.colors.onSurface }]}>
                  {formatRelativeTime(ticket.updatedAt)}
                </Text>
              </View>
            </View>

            {/* Category */}
            {ticket.categoryName && (
              <View style={styles.metaItem}>
                <MaterialCommunityIcons
                  name="folder-outline"
                  size={18}
                  color={theme.colors.onSurfaceVariant}
                />
                <View style={styles.metaText}>
                  <Text style={[styles.metaLabel, { color: theme.colors.onSurfaceVariant }]}>
                    Category
                  </Text>
                  <Text style={[styles.metaValue, { color: theme.colors.onSurface }]}>
                    {ticket.categoryName}
                  </Text>
                </View>
              </View>
            )}

            {/* Type */}
            <View style={styles.metaItem}>
              <MaterialCommunityIcons
                name="tag-outline"
                size={18}
                color={theme.colors.onSurfaceVariant}
              />
              <View style={styles.metaText}>
                <Text style={[styles.metaLabel, { color: theme.colors.onSurfaceVariant }]}>
                  Type
                </Text>
                <Text style={[styles.metaValue, { color: theme.colors.onSurface }]}>
                  {ticket.type.charAt(0).toUpperCase() + ticket.type.slice(1)}
                </Text>
              </View>
            </View>
          </View>
        </Surface>

        {/* Comments Section */}
        <View style={styles.commentsSection}>
          <Text style={[styles.sectionTitle, { color: theme.colors.onSurface }]}>
            Comments ({comments.length})
          </Text>

          {loadingComments ? (
            <View style={styles.commentsLoading}>
              <ActivityIndicator size="small" color={theme.colors.primary} />
              <Text style={[styles.loadingText, { color: theme.colors.onSurfaceVariant }]}>
                Loading comments...
              </Text>
            </View>
          ) : comments.length === 0 ? (
            <View style={styles.noComments}>
              <MaterialCommunityIcons
                name="comment-outline"
                size={40}
                color={theme.colors.onSurfaceVariant}
              />
              <Text style={[styles.noCommentsText, { color: theme.colors.onSurfaceVariant }]}>
                No comments yet. Be the first to comment!
              </Text>
            </View>
          ) : (
            <View style={styles.commentsList}>
              {comments.map((comment) => (
                <CommentBubble
                  key={comment.id}
                  comment={comment}
                  isOwnComment={user?.id === comment.authorId}
                  currentUserId={user?.id}
                />
              ))}
            </View>
          )}
        </View>
      </ScrollView>

      {/* Comment Input */}
      <CommentInput onSend={handleAddComment} disabled={submitting} />

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

      {/* Error Snackbar */}
      <Portal>
        <Snackbar
          visible={!!error}
          onDismiss={clearError}
          duration={5000}
          action={{
            label: 'Retry',
            onPress: () => id && fetchTicket(id),
          }}
          style={[styles.snackbar, { backgroundColor: Colors.error }]}
        >
          {error}
        </Snackbar>
      </Portal>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  centerContent: {
    justifyContent: 'center',
    alignItems: 'center',
    padding: Spacing.lg,
  },
  loadingText: {
    marginTop: Spacing.md,
    fontSize: FontSizes.md,
  },
  errorTitle: {
    fontSize: FontSizes.lg,
    fontWeight: '600',
    marginTop: Spacing.md,
    marginBottom: Spacing.lg,
  },
  backButton: {
    marginTop: Spacing.md,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingBottom: Spacing.lg,
  },
  headerCard: {
    margin: Spacing.md,
    padding: Spacing.md,
    borderRadius: BorderRadius.lg,
    backgroundColor: Colors.dark.surface,
  },
  badgeRow: {
    flexDirection: 'row',
    gap: Spacing.sm,
    marginBottom: Spacing.md,
  },
  statusChip: {
    height: 28,
  },
  priorityChip: {
    height: 28,
  },
  statusDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
    marginRight: Spacing.xs,
  },
  subject: {
    fontSize: FontSizes.lg,
    fontWeight: '600',
    lineHeight: 24,
    marginBottom: Spacing.sm,
  },
  description: {
    fontSize: FontSizes.md,
    lineHeight: 22,
  },
  divider: {
    marginVertical: Spacing.md,
    backgroundColor: Colors.dark.border,
  },
  metaGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: Spacing.md,
  },
  metaItem: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    width: '45%',
    gap: Spacing.xs,
  },
  metaText: {
    flex: 1,
  },
  metaLabel: {
    fontSize: FontSizes.xs,
    marginBottom: 2,
  },
  metaValue: {
    fontSize: FontSizes.sm,
    fontWeight: '500',
  },
  commentsSection: {
    paddingHorizontal: Spacing.md,
    paddingTop: Spacing.md,
  },
  sectionTitle: {
    fontSize: FontSizes.lg,
    fontWeight: '600',
    marginBottom: Spacing.md,
  },
  commentsLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    padding: Spacing.lg,
    gap: Spacing.sm,
  },
  noComments: {
    alignItems: 'center',
    padding: Spacing.xl,
  },
  noCommentsText: {
    marginTop: Spacing.sm,
    fontSize: FontSizes.md,
    textAlign: 'center',
  },
  commentsList: {
    gap: Spacing.xs,
  },
  snackbar: {
    marginBottom: 80,
  },
});
