/**
 * CommentInput Component
 * Text input for adding comments with send button
 */
import React, { useState } from 'react';
import {
  View,
  StyleSheet,
  TextInput,
  TouchableOpacity,
  Keyboard,
  KeyboardAvoidingView,
  Platform,
} from 'react-native';
import { ActivityIndicator, useTheme } from 'react-native-paper';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors } from '@/constants/theme';

interface CommentInputProps {
  onSend: (content: string) => Promise<void>;
  disabled?: boolean;
  placeholder?: string;
}

export function CommentInput({
  onSend,
  disabled = false,
  placeholder = 'Write a comment...',
}: CommentInputProps) {
  const theme = useTheme();
  const [content, setContent] = useState('');
  const [sending, setSending] = useState(false);

  const handleSend = async () => {
    const trimmedContent = content.trim();
    if (!trimmedContent || sending || disabled) return;

    setSending(true);
    Keyboard.dismiss();

    try {
      await onSend(trimmedContent);
      setContent(''); // Clear input on success
    } catch (error) {
      console.error('[CommentInput] Send failed:', error);
      // Content is preserved for retry
    } finally {
      setSending(false);
    }
  };

  const canSend = content.trim().length > 0 && !sending && !disabled;

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      keyboardVerticalOffset={Platform.OS === 'ios' ? 90 : 0}
    >
      <View style={styles.container}>
        {/* Input field */}
        <View style={styles.inputContainer}>
          <TextInput
            style={styles.input}
            value={content}
            onChangeText={setContent}
            placeholder={placeholder}
            placeholderTextColor={Colors.dark.textMuted}
            multiline
            maxLength={5000}
            editable={!sending && !disabled}
            returnKeyType="default"
            blurOnSubmit={false}
          />
        </View>

        {/* Send button */}
        <TouchableOpacity
          style={[styles.sendButton, canSend ? styles.sendButtonActive : styles.sendButtonDisabled]}
          onPress={handleSend}
          disabled={!canSend}
          activeOpacity={0.7}
        >
          {sending ? (
            <ActivityIndicator size={20} color={Colors.dark.text} />
          ) : (
            <MaterialCommunityIcons
              name="send"
              size={20}
              color={canSend ? '#ffffff' : Colors.dark.textMuted}
            />
          )}
        </TouchableOpacity>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    paddingHorizontal: 12,
    paddingVertical: 8,
    backgroundColor: Colors.dark.surface,
    borderTopWidth: 1,
    borderTopColor: Colors.dark.border,
    gap: 8,
  },
  inputContainer: {
    flex: 1,
    backgroundColor: Colors.dark.surfaceVariant,
    borderRadius: 20,
    paddingHorizontal: 16,
    paddingVertical: 8,
    minHeight: 40,
    maxHeight: 120,
  },
  input: {
    fontSize: 15,
    color: Colors.dark.text,
    lineHeight: 20,
    paddingVertical: 0,
    maxHeight: 100,
  },
  sendButton: {
    width: 40,
    height: 40,
    borderRadius: 20,
    justifyContent: 'center',
    alignItems: 'center',
    marginBottom: 0,
  },
  sendButtonActive: {
    backgroundColor: Colors.primary,
  },
  sendButtonDisabled: {
    backgroundColor: Colors.dark.surfaceVariant,
  },
});

export default CommentInput;
