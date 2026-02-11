/**
 * Login Screen - Sentinel Mobile
 * Dark themed login form with username/password authentication
 */
import { useState } from 'react';
import {
  View,
  StyleSheet,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  Image,
} from 'react-native';
import {
  TextInput,
  Button,
  Surface,
  Text,
  HelperText,
  useTheme,
  ActivityIndicator,
} from 'react-native-paper';
import { useRouter } from 'expo-router';
import { useAuthStore } from '@/stores/authStore';

export default function LoginScreen() {
  const router = useRouter();
  const theme = useTheme();
  const { login, isLoading, error, clearError } = useAuthStore();

  const [identifier, setIdentifier] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);

  const handleLogin = async () => {
    // Clear any previous errors
    clearError();
    setLocalError(null);

    // Basic validation
    if (!identifier.trim()) {
      setLocalError('Please enter your username or email');
      return;
    }
    if (!password) {
      setLocalError('Please enter your password');
      return;
    }

    try {
      await login(identifier.trim(), password);
      // Navigation is handled by the root layout based on isAuthenticated
      router.replace('/(tabs)');
    } catch (err) {
      // Error is already set in the store
      console.log('[Login] Login failed');
    }
  };

  const displayError = localError || error;

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      <ScrollView
        contentContainerStyle={styles.scrollContent}
        keyboardShouldPersistTaps="handled"
      >
        {/* Logo Section */}
        <View style={styles.logoSection}>
          <View style={styles.logoContainer}>
            <Image
              source={require('@/assets/icon.png')}
              style={styles.logo}
              resizeMode="contain"
            />
          </View>
          <Text variant="headlineLarge" style={styles.title}>
            Sentinel
          </Text>
          <Text variant="bodyMedium" style={styles.subtitle}>
            Remote Monitoring & Management
          </Text>
        </View>

        {/* Login Card */}
        <Surface style={styles.card} elevation={2}>
          <Text variant="titleLarge" style={styles.cardTitle}>
            Sign In
          </Text>

          {/* Username/Email Input */}
          <TextInput
            label="Username or Email"
            value={identifier}
            onChangeText={(text) => {
              setIdentifier(text);
              if (localError) setLocalError(null);
              if (error) clearError();
            }}
            mode="outlined"
            autoCapitalize="none"
            autoCorrect={false}
            autoComplete="username"
            keyboardType="email-address"
            returnKeyType="next"
            style={styles.input}
            outlineColor={theme.colors.outline}
            activeOutlineColor={theme.colors.primary}
            textColor={theme.colors.onSurface}
            left={<TextInput.Icon icon="account" />}
            disabled={isLoading}
          />

          {/* Password Input */}
          <TextInput
            label="Password"
            value={password}
            onChangeText={(text) => {
              setPassword(text);
              if (localError) setLocalError(null);
              if (error) clearError();
            }}
            mode="outlined"
            secureTextEntry={!showPassword}
            autoCapitalize="none"
            autoComplete="password"
            returnKeyType="done"
            onSubmitEditing={handleLogin}
            style={styles.input}
            outlineColor={theme.colors.outline}
            activeOutlineColor={theme.colors.primary}
            textColor={theme.colors.onSurface}
            left={<TextInput.Icon icon="lock" />}
            right={
              <TextInput.Icon
                icon={showPassword ? 'eye-off' : 'eye'}
                onPress={() => setShowPassword(!showPassword)}
              />
            }
            disabled={isLoading}
          />

          {/* Error Display */}
          {displayError && (
            <Surface style={styles.errorContainer} elevation={0}>
              <HelperText type="error" visible={true} style={styles.errorText}>
                {displayError}
              </HelperText>
            </Surface>
          )}

          {/* Login Button */}
          <Button
            mode="contained"
            onPress={handleLogin}
            loading={isLoading}
            disabled={isLoading}
            style={styles.button}
            contentStyle={styles.buttonContent}
            labelStyle={styles.buttonLabel}
          >
            {isLoading ? 'Signing in...' : 'Sign In'}
          </Button>
        </Surface>

        {/* Footer */}
        <View style={styles.footer}>
          <Text variant="bodySmall" style={styles.footerText}>
            Secure connection to sentinelrmm.us
          </Text>
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#000000',
  },
  scrollContent: {
    flexGrow: 1,
    justifyContent: 'center',
    padding: 24,
  },
  logoSection: {
    alignItems: 'center',
    marginBottom: 32,
  },
  logoContainer: {
    width: 120,
    height: 120,
    marginBottom: 16,
    borderRadius: 24,
    backgroundColor: '#111111',
    justifyContent: 'center',
    alignItems: 'center',
    overflow: 'hidden',
  },
  logo: {
    width: 100,
    height: 100,
  },
  title: {
    color: '#ffffff',
    fontWeight: 'bold',
    marginBottom: 4,
  },
  subtitle: {
    color: '#888888',
  },
  card: {
    backgroundColor: '#111111',
    borderRadius: 16,
    padding: 24,
    marginBottom: 24,
  },
  cardTitle: {
    color: '#ffffff',
    fontWeight: '600',
    marginBottom: 24,
    textAlign: 'center',
  },
  input: {
    marginBottom: 16,
    backgroundColor: '#1a1a1a',
  },
  errorContainer: {
    backgroundColor: 'rgba(239, 68, 68, 0.1)',
    borderRadius: 8,
    borderWidth: 1,
    borderColor: 'rgba(239, 68, 68, 0.3)',
    marginBottom: 16,
    padding: 4,
  },
  errorText: {
    color: '#ef4444',
    fontSize: 14,
  },
  button: {
    marginTop: 8,
    borderRadius: 8,
  },
  buttonContent: {
    paddingVertical: 8,
  },
  buttonLabel: {
    fontSize: 16,
    fontWeight: '600',
  },
  footer: {
    alignItems: 'center',
  },
  footerText: {
    color: '#666666',
  },
});
