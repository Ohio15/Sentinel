/**
 * TicketListItem Component
 * Displays a single ticket in the list view
 */
import React from 'react';
import { View, StyleSheet, TouchableOpacity } from 'react-native';
import { Text, Surface, useTheme } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Ticket } from '@/stores/ticketStore';
import { Colors } from '@/constants/theme';

interface TicketListItemProps {
  ticket: Ticket;
  onPress: (ticket: Ticket) => void;
}

// Format ticket number to TKT-XXXXXX format
const formatTicketNumber = (num: number): string => {
  return `TKT-${String(num).padStart(6, '0')}`;
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
  return date.toLocaleDateString();
};

// Get status color
const getStatusColor = (status: string): string => {
  switch (status) {
    case 'open':
      return Colors.ticketOpen;
    case 'in_progress':
      return Colors.ticketInProgress;
    case 'waiting':
      return Colors.warning;
    case 'resolved':
      return Colors.ticketResolved;
    case 'closed':
      return Colors.ticketClosed;
    default:
      return Colors.ticketOpen;
  }
};

// Get status display name
const getStatusDisplay = (status: string): string => {
  switch (status) {
    case 'open':
      return 'Open';
    case 'in_progress':
      return 'In Progress';
    case 'waiting':
      return 'Waiting';
    case 'resolved':
      return 'Resolved';
    case 'closed':
      return 'Closed';
    default:
      return status;
  }
};

// Get priority color
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

// Get priority icon
const getPriorityIcon = (priority: string): string => {
  switch (priority) {
    case 'urgent':
      return 'alert-circle';
    case 'high':
      return 'arrow-up-bold';
    case 'medium':
      return 'minus';
    case 'low':
      return 'arrow-down-bold';
    default:
      return 'minus';
  }
};

export function TicketListItem({ ticket, onPress }: TicketListItemProps) {
  const theme = useTheme();
  const statusColor = getStatusColor(ticket.status);
  const priorityColor = getPriorityColor(ticket.priority);

  return (
    <TouchableOpacity onPress={() => onPress(ticket)} activeOpacity={0.7}>
      <Surface style={styles.container} elevation={1}>
        {/* Priority indicator bar */}
        <View style={[styles.priorityBar, { backgroundColor: priorityColor }]} />

        <View style={styles.content}>
          {/* Header row: ticket number and status */}
          <View style={styles.headerRow}>
            <View style={styles.ticketNumberContainer}>
              <MaterialCommunityIcons
                name="ticket-outline"
                size={16}
                color={Colors.dark.textSecondary}
              />
              <Text style={styles.ticketNumber}>
                {formatTicketNumber(ticket.ticketNumber)}
              </Text>
            </View>

            {/* Status badge */}
            <View style={[styles.statusBadge, { backgroundColor: `${statusColor}20` }]}>
              <View style={[styles.statusDot, { backgroundColor: statusColor }]} />
              <Text style={[styles.statusText, { color: statusColor }]}>
                {getStatusDisplay(ticket.status)}
              </Text>
            </View>
          </View>

          {/* Subject */}
          <Text style={styles.subject} numberOfLines={2}>
            {ticket.subject}
          </Text>

          {/* Footer row: priority, requester, time */}
          <View style={styles.footerRow}>
            {/* Priority */}
            <View style={styles.priorityContainer}>
              <MaterialCommunityIcons
                name={getPriorityIcon(ticket.priority) as any}
                size={14}
                color={priorityColor}
              />
              <Text style={[styles.priorityText, { color: priorityColor }]}>
                {ticket.priority.charAt(0).toUpperCase() + ticket.priority.slice(1)}
              </Text>
            </View>

            {/* Separator */}
            <View style={styles.separator} />

            {/* Requester */}
            {ticket.requesterName && (
              <>
                <View style={styles.requesterContainer}>
                  <MaterialCommunityIcons
                    name="account-outline"
                    size={14}
                    color={Colors.dark.textSecondary}
                  />
                  <Text style={styles.requesterText} numberOfLines={1}>
                    {ticket.requesterName}
                  </Text>
                </View>
                <View style={styles.separator} />
              </>
            )}

            {/* Updated time */}
            <View style={styles.timeContainer}>
              <MaterialCommunityIcons
                name="clock-outline"
                size={14}
                color={Colors.dark.textSecondary}
              />
              <Text style={styles.timeText}>
                {formatRelativeTime(ticket.updatedAt)}
              </Text>
            </View>
          </View>
        </View>
      </Surface>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    backgroundColor: Colors.dark.surface,
    borderRadius: 12,
    marginHorizontal: 16,
    marginVertical: 6,
    overflow: 'hidden',
  },
  priorityBar: {
    width: 4,
  },
  content: {
    flex: 1,
    padding: 12,
  },
  headerRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  ticketNumberContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  ticketNumber: {
    fontSize: 13,
    color: Colors.dark.textSecondary,
    fontWeight: '500',
  },
  statusBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 12,
    gap: 4,
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  statusText: {
    fontSize: 12,
    fontWeight: '600',
  },
  subject: {
    fontSize: 15,
    color: Colors.dark.text,
    fontWeight: '500',
    marginBottom: 10,
    lineHeight: 20,
  },
  footerRow: {
    flexDirection: 'row',
    alignItems: 'center',
    flexWrap: 'wrap',
  },
  priorityContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  priorityText: {
    fontSize: 12,
    fontWeight: '500',
  },
  separator: {
    width: 4,
    height: 4,
    borderRadius: 2,
    backgroundColor: Colors.dark.textMuted,
    marginHorizontal: 8,
  },
  requesterContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    maxWidth: 120,
  },
  requesterText: {
    fontSize: 12,
    color: Colors.dark.textSecondary,
  },
  timeContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  timeText: {
    fontSize: 12,
    color: Colors.dark.textSecondary,
  },
});

export default TicketListItem;
