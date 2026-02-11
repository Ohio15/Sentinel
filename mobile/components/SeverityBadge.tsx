/**
 * Severity Badge Component
 * Displays alert severity with appropriate color and icon
 */
import React from 'react';
import { View, StyleSheet } from 'react-native';
import { Text, useTheme } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing, BorderRadius, FontSizes } from '../constants/theme';
import type { AlertSeverity } from '../stores/alertStore';

interface SeverityBadgeProps {
  severity: AlertSeverity;
  size?: 'small' | 'medium' | 'large';
  showText?: boolean;
}

const severityConfig: Record<
  AlertSeverity,
  {
    color: string;
    backgroundColor: string;
    icon: keyof typeof MaterialCommunityIcons.glyphMap;
    label: string;
  }
> = {
  critical: {
    color: Colors.critical,
    backgroundColor: 'rgba(239, 68, 68, 0.15)',
    icon: 'alert-circle',
    label: 'Critical',
  },
  warning: {
    color: Colors.warning,
    backgroundColor: 'rgba(245, 158, 11, 0.15)',
    icon: 'alert',
    label: 'Warning',
  },
  info: {
    color: Colors.info,
    backgroundColor: 'rgba(59, 130, 246, 0.15)',
    icon: 'information',
    label: 'Info',
  },
};

const sizeConfig = {
  small: {
    iconSize: 12,
    fontSize: FontSizes.xs,
    paddingH: Spacing.xs,
    paddingV: 2,
  },
  medium: {
    iconSize: 14,
    fontSize: FontSizes.sm,
    paddingH: Spacing.sm,
    paddingV: 4,
  },
  large: {
    iconSize: 18,
    fontSize: FontSizes.md,
    paddingH: Spacing.md,
    paddingV: 6,
  },
};

export function SeverityBadge({
  severity,
  size = 'medium',
  showText = true,
}: SeverityBadgeProps) {
  const config = severityConfig[severity];
  const sizeProps = sizeConfig[size];

  return (
    <View
      style={[
        styles.container,
        {
          backgroundColor: config.backgroundColor,
          paddingHorizontal: sizeProps.paddingH,
          paddingVertical: sizeProps.paddingV,
        },
      ]}
    >
      <MaterialCommunityIcons
        name={config.icon}
        size={sizeProps.iconSize}
        color={config.color}
      />
      {showText && (
        <Text
          style={[
            styles.text,
            {
              color: config.color,
              fontSize: sizeProps.fontSize,
            },
          ]}
        >
          {config.label}
        </Text>
      )}
    </View>
  );
}

/**
 * Severity indicator bar - for use on the left edge of cards
 */
interface SeverityBarProps {
  severity: AlertSeverity;
  width?: number;
}

export function SeverityBar({ severity, width = 4 }: SeverityBarProps) {
  const config = severityConfig[severity];

  return (
    <View
      style={[
        styles.bar,
        {
          backgroundColor: config.color,
          width,
        },
      ]}
    />
  );
}

/**
 * Get severity color for external use
 */
export function getSeverityColor(severity: AlertSeverity): string {
  return severityConfig[severity]?.color || Colors.info;
}

/**
 * Get severity background color for external use
 */
export function getSeverityBackgroundColor(severity: AlertSeverity): string {
  return severityConfig[severity]?.backgroundColor || 'rgba(59, 130, 246, 0.15)';
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: BorderRadius.sm,
    gap: 4,
  },
  text: {
    fontWeight: '600',
  },
  bar: {
    height: '100%',
    borderTopLeftRadius: BorderRadius.md,
    borderBottomLeftRadius: BorderRadius.md,
  },
});

export default SeverityBadge;
