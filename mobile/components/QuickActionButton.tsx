/**
 * QuickActionButton Component
 * Reusable action button with icon, label, loading state, and confirmation dialog support
 */
import React, { useState } from 'react';
import { View, StyleSheet, Alert } from 'react-native';
import { Button, ActivityIndicator, Portal, Dialog, Text } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing, BorderRadius } from '@/constants/theme';

interface QuickActionButtonProps {
  icon: keyof typeof MaterialCommunityIcons.glyphMap;
  label: string;
  onPress: () => Promise<void> | void;
  variant?: 'primary' | 'secondary' | 'danger' | 'warning';
  disabled?: boolean;
  confirmTitle?: string;
  confirmMessage?: string;
  confirmButtonLabel?: string;
}

const variantColors = {
  primary: {
    background: Colors.primary,
    text: '#FFFFFF',
    icon: '#FFFFFF',
  },
  secondary: {
    background: Colors.dark.surfaceVariant,
    text: Colors.dark.text,
    icon: Colors.dark.textSecondary,
  },
  danger: {
    background: Colors.error,
    text: '#FFFFFF',
    icon: '#FFFFFF',
  },
  warning: {
    background: Colors.warning,
    text: '#000000',
    icon: '#000000',
  },
};

export function QuickActionButton({
  icon,
  label,
  onPress,
  variant = 'secondary',
  disabled = false,
  confirmTitle,
  confirmMessage,
  confirmButtonLabel,
}: QuickActionButtonProps) {
  const [loading, setLoading] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);

  const colors = variantColors[variant];

  const handlePress = async () => {
    if (confirmTitle && confirmMessage) {
      setShowConfirm(true);
    } else {
      await executeAction();
    }
  };

  const executeAction = async () => {
    setLoading(true);
    try {
      await onPress();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Action failed';
      Alert.alert('Error', message);
    } finally {
      setLoading(false);
    }
  };

  const handleConfirm = async () => {
    setShowConfirm(false);
    await executeAction();
  };

  return (
    <>
      <Button
        mode="contained"
        onPress={handlePress}
        disabled={disabled || loading}
        style={[
          styles.button,
          { backgroundColor: disabled ? Colors.dark.surfaceVariant : colors.background }
        ]}
        labelStyle={[styles.label, { color: disabled ? Colors.dark.textMuted : colors.text }]}
        contentStyle={styles.buttonContent}
        icon={({ size }) =>
          loading ? (
            <ActivityIndicator size={size - 4} color={colors.text} />
          ) : (
            <MaterialCommunityIcons
              name={icon}
              size={size}
              color={disabled ? Colors.dark.textMuted : colors.icon}
            />
          )
        }
      >
        {label}
      </Button>

      {/* Confirmation Dialog */}
      <Portal>
        <Dialog
          visible={showConfirm}
          onDismiss={() => setShowConfirm(false)}
          style={styles.dialog}
        >
          <Dialog.Title style={styles.dialogTitle}>{confirmTitle}</Dialog.Title>
          <Dialog.Content>
            <Text style={styles.dialogMessage}>{confirmMessage}</Text>
          </Dialog.Content>
          <Dialog.Actions>
            <Button
              onPress={() => setShowConfirm(false)}
              textColor={Colors.dark.textSecondary}
            >
              Cancel
            </Button>
            <Button
              onPress={handleConfirm}
              textColor={variant === 'danger' ? Colors.error : Colors.primary}
            >
              {confirmButtonLabel || 'Confirm'}
            </Button>
          </Dialog.Actions>
        </Dialog>
      </Portal>
    </>
  );
}

const styles = StyleSheet.create({
  button: {
    borderRadius: BorderRadius.md,
    marginVertical: Spacing.xs,
  },
  buttonContent: {
    paddingVertical: Spacing.xs,
  },
  label: {
    fontWeight: '600',
    fontSize: 14,
  },
  dialog: {
    backgroundColor: Colors.dark.surface,
    borderRadius: BorderRadius.lg,
  },
  dialogTitle: {
    color: Colors.dark.text,
  },
  dialogMessage: {
    color: Colors.dark.textSecondary,
  },
});
