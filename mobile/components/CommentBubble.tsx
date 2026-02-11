/**
 * CommentBubble Component
 * Displays a single comment in a chat-like bubble style
 */
import React from 'react';
import { View, StyleSheet } from 'react-native';
import { Text, Surface, Avatar, useTheme } from 'react-native-paper';
import { TicketComment } from '@/stores/ticketStore';
import { Colors } from '@/constants/theme';

interface CommentBubbleProps {
  comment: TicketComment;
  isOwnComment: boolean;
  currentUserId?: string;
}

// Format timestamp
const formatTimestamp = (dateString: string): string => {
  const date = new Date(dateString);
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const minutes = Math.floor(diff / (1000 * 60));
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;

  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

// Get initials from name
const getInitials = (name: string): string => {
  if (!name) return '?';
  const parts = name.split(' ');
  if (parts.length >= 2) {
    return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
  }
  return name.substring(0, 2).toUpperCase();
};

// Generate consistent color from string
const stringToColor = (str: string): string => {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  const colors = [
    Colors.primary,
    Colors.secondary,
    Colors.info,
    '#8b5cf6', // violet
    '#ec4899', // pink
    '#14b8a6', // teal
    '#f97316', // orange
  ];
  return colors[Math.abs(hash) % colors.length];
};

export function CommentBubble({ comment, isOwnComment }: CommentBubbleProps) {
  const theme = useTheme();
  const avatarColor = stringToColor(comment.authorName || 'Unknown');

  return (
    <View style={[styles.container, isOwnComment ? styles.ownContainer : styles.otherContainer]}>
      {/* Avatar for other users */}
      {!isOwnComment && (
        <View style={styles.avatarContainer}>
          <Avatar.Text
            size={32}
            label={getInitials(comment.authorName)}
            style={{ backgroundColor: avatarColor }}
            labelStyle={styles.avatarLabel}
          />
        </View>
      )}

      <View style={[styles.bubbleContainer, isOwnComment ? styles.ownBubbleContainer : styles.otherBubbleContainer]}>
        {/* Author name (for others only) */}
        {!isOwnComment && (
          <Text style={[styles.authorName, { color: avatarColor }]}>
            {comment.authorName}
          </Text>
        )}

        {/* Comment bubble */}
        <Surface
          style={[
            styles.bubble,
            isOwnComment ? styles.ownBubble : styles.otherBubble,
            comment.isInternal && styles.internalBubble,
          ]}
          elevation={1}
        >
          {/* Internal tag */}
          {comment.isInternal && (
            <View style={styles.internalTag}>
              <Text style={styles.internalText}>Internal Note</Text>
            </View>
          )}

          {/* Comment content */}
          <Text style={[styles.content, isOwnComment ? styles.ownContent : styles.otherContent]}>
            {comment.content}
          </Text>
        </Surface>

        {/* Timestamp */}
        <Text style={[styles.timestamp, isOwnComment ? styles.ownTimestamp : styles.otherTimestamp]}>
          {formatTimestamp(comment.createdAt)}
        </Text>
      </View>

      {/* Spacer for own comments (where avatar would be) */}
      {isOwnComment && <View style={styles.avatarSpacer} />}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    marginVertical: 4,
    paddingHorizontal: 12,
  },
  ownContainer: {
    justifyContent: 'flex-end',
  },
  otherContainer: {
    justifyContent: 'flex-start',
  },
  avatarContainer: {
    marginRight: 8,
    alignSelf: 'flex-end',
    marginBottom: 20,
  },
  avatarSpacer: {
    width: 40,
  },
  avatarLabel: {
    fontSize: 13,
    fontWeight: '600',
  },
  bubbleContainer: {
    maxWidth: '75%',
    flexShrink: 1,
  },
  ownBubbleContainer: {
    alignItems: 'flex-end',
  },
  otherBubbleContainer: {
    alignItems: 'flex-start',
  },
  authorName: {
    fontSize: 12,
    fontWeight: '600',
    marginBottom: 4,
    marginLeft: 4,
  },
  bubble: {
    borderRadius: 16,
    paddingVertical: 10,
    paddingHorizontal: 14,
    maxWidth: '100%',
  },
  ownBubble: {
    backgroundColor: Colors.primary,
    borderBottomRightRadius: 4,
  },
  otherBubble: {
    backgroundColor: Colors.dark.surfaceVariant,
    borderBottomLeftRadius: 4,
  },
  internalBubble: {
    backgroundColor: '#fef3c7', // amber-100
    borderWidth: 1,
    borderColor: '#f59e0b', // amber-500
  },
  internalTag: {
    marginBottom: 6,
  },
  internalText: {
    fontSize: 10,
    fontWeight: '600',
    color: '#92400e', // amber-800
    textTransform: 'uppercase',
    letterSpacing: 0.5,
  },
  content: {
    fontSize: 14,
    lineHeight: 20,
  },
  ownContent: {
    color: '#ffffff',
  },
  otherContent: {
    color: Colors.dark.text,
  },
  timestamp: {
    fontSize: 11,
    color: Colors.dark.textMuted,
    marginTop: 4,
    marginHorizontal: 4,
  },
  ownTimestamp: {
    textAlign: 'right',
  },
  otherTimestamp: {
    textAlign: 'left',
  },
});

export default CommentBubble;
