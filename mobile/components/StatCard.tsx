/**
 * StatCard Component - Sentinel Mobile
 * Reusable card for displaying statistics with icon and optional subtitle
 */
import React from 'react';
import { StyleSheet, Pressable, View } from 'react-native';
import { Surface, Text, useTheme } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';

export type StatCardColor = 'primary' | 'success' | 'warning' | 'error' | 'info' | 'gray';

interface StatCardProps {
  label: string;
  value: number | string;
  subtitle?: string;
  icon: keyof typeof MaterialCommunityIcons.glyphMap;
  color?: StatCardColor;
  onPress?: () => void;
}

const colorMap: Record<StatCardColor, { bg: string; text: string }> = {
  primary: { bg: 'rgba(16, 185, 129, 0.15)', text: '#10b981' },
  success: { bg: 'rgba(34, 197, 94, 0.15)', text: '#22c55e' },
  warning: { bg: 'rgba(234, 179, 8, 0.15)', text: '#eab308' },
  error: { bg: 'rgba(239, 68, 68, 0.15)', text: '#ef4444' },
  info: { bg: 'rgba(59, 130, 246, 0.15)', text: '#3b82f6' },
  gray: { bg: 'rgba(156, 163, 175, 0.15)', text: '#9ca3af' },
};

export function StatCard({
  label,
  value,
  subtitle,
  icon,
  color = 'primary',
  onPress,
}: StatCardProps) {
  const theme = useTheme();
  const colors = colorMap[color];

  const content = (
    <Surface style={[styles.card, { backgroundColor: theme.colors.surface }]} elevation={1}>
      <View style={styles.content}>
        <View style={styles.textContainer}>
          <Text style={[styles.label, { color: theme.colors.onSurfaceVariant }]}>
            {label}
          </Text>
          <Text style={[styles.value, { color: theme.colors.onSurface }]}>
            {value}
          </Text>
          {subtitle && (
            <Text style={[styles.subtitle, { color: colors.text }]}>
              {subtitle}
            </Text>
          )}
        </View>
        <View style={[styles.iconContainer, { backgroundColor: colors.bg }]}>
          <MaterialCommunityIcons name={icon} size={24} color={colors.text} />
        </View>
      </View>
    </Surface>
  );

  if (onPress) {
    return (
      <Pressable
        onPress={onPress}
        style={({ pressed }) => [
          styles.pressable,
          pressed && styles.pressed,
        ]}
      >
        {content}
      </Pressable>
    );
  }

  return content;
}

const styles = StyleSheet.create({
  pressable: {
    flex: 1,
    minWidth: '45%',
  },
  pressed: {
    opacity: 0.8,
  },
  card: {
    borderRadius: 12,
    padding: 16,
    flex: 1,
    minWidth: '45%',
  },
  content: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
  },
  textContainer: {
    flex: 1,
    marginRight: 12,
  },
  label: {
    fontSize: 13,
    fontWeight: '500',
    marginBottom: 4,
  },
  value: {
    fontSize: 28,
    fontWeight: '700',
    lineHeight: 34,
  },
  subtitle: {
    fontSize: 12,
    fontWeight: '500',
    marginTop: 4,
  },
  iconContainer: {
    width: 44,
    height: 44,
    borderRadius: 10,
    justifyContent: 'center',
    alignItems: 'center',
  },
});
