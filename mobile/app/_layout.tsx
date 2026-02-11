/**
 * Root Layout - Sentinel Mobile
 * Sets up providers, theme, navigation container, and push notifications
 */
import { useEffect, useCallback } from 'react';
import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PaperProvider, MD3DarkTheme, MD3LightTheme } from 'react-native-paper';
import { useColorScheme, View, StyleSheet } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import * as SplashScreen from 'expo-splash-screen';
import { useAuthStore } from '@/stores/authStore';
import { useNotifications, NotificationPayload } from '@/hooks/useNotifications';
import { NotificationBanner } from '@/components/NotificationBanner';
import { notificationService } from '@/services/notifications';

// Keep splash screen visible while we load resources
SplashScreen.preventAutoHideAsync();

// Create React Query client
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000, // 30 seconds
      retry: 2,
    },
  },
});

// Custom dark theme matching Sentinel web
const darkTheme = {
  ...MD3DarkTheme,
  colors: {
    ...MD3DarkTheme.colors,
    primary: '#10b981',
    primaryContainer: '#10b981',
    secondary: '#6366f1',
    background: '#000000',
    surface: '#111111',
    surfaceVariant: '#1a1a1a',
    error: '#ef4444',
  },
};

const lightTheme = {
  ...MD3LightTheme,
  colors: {
    ...MD3LightTheme.colors,
    primary: '#10b981',
    primaryContainer: '#10b981',
    secondary: '#6366f1',
  },
};

/**
 * Notification Provider Component
 * Handles notification setup after authentication
 */
function NotificationProvider({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuthStore();

  // Handle notification received callback
  const handleNotificationReceived = useCallback((payload: NotificationPayload) => {
    console.log('[Layout] Notification received:', payload.title);
  }, []);

  // Handle notification tapped callback
  const handleNotificationTapped = useCallback((payload: NotificationPayload) => {
    console.log('[Layout] Notification tapped:', payload.title);
  }, []);

  // Set up notification listeners and registration
  const {
    latestNotification,
    dismissBanner,
  } = useNotifications({
    autoRegister: isAuthenticated,
    onNotificationReceived: handleNotificationReceived,
    onNotificationTapped: handleNotificationTapped,
    enableInAppBanner: true,
  });

  // Clean up notifications on logout
  useEffect(() => {
    if (!isAuthenticated) {
      // User logged out - clean up notification registration
      notificationService.cleanup();
    }
  }, [isAuthenticated]);

  return (
    <View style={styles.container}>
      {children}
      {/* In-app notification banner */}
      <NotificationBanner
        notification={latestNotification}
        onDismiss={dismissBanner}
        autoDismiss={true}
        dismissTimeout={5000}
      />
    </View>
  );
}

/**
 * Main Navigation Component
 */
function AppNavigator() {
  const colorScheme = useColorScheme();
  const theme = colorScheme === 'dark' ? darkTheme : lightTheme;
  const { isAuthenticated, checkAuth } = useAuthStore();

  useEffect(() => {
    async function prepare() {
      try {
        // Check for existing auth token
        await checkAuth();
      } finally {
        await SplashScreen.hideAsync();
      }
    }
    prepare();
  }, []);

  return (
    <PaperProvider theme={theme}>
      <StatusBar style="auto" />
      <NotificationProvider>
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: theme.colors.background },
          }}
        >
          {isAuthenticated ? (
            <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
          ) : (
            <Stack.Screen name="login" options={{ headerShown: false }} />
          )}
          <Stack.Screen
            name="device/[id]"
            options={{
              headerShown: true,
              title: 'Device Details',
              headerStyle: { backgroundColor: theme.colors.surface },
              headerTintColor: theme.colors.primary,
            }}
          />
          <Stack.Screen
            name="alert/[id]"
            options={{
              headerShown: true,
              title: 'Alert Details',
              headerStyle: { backgroundColor: theme.colors.surface },
              headerTintColor: theme.colors.primary,
            }}
          />
          <Stack.Screen
            name="ticket/[id]"
            options={{
              headerShown: true,
              title: 'Ticket Details',
              headerStyle: { backgroundColor: theme.colors.surface },
              headerTintColor: theme.colors.primary,
            }}
          />
        </Stack>
      </NotificationProvider>
    </PaperProvider>
  );
}

/**
 * Root Layout - wraps everything with necessary providers
 */
export default function RootLayout() {
  return (
    <GestureHandlerRootView style={styles.container}>
      <SafeAreaProvider>
        <QueryClientProvider client={queryClient}>
          <AppNavigator />
        </QueryClientProvider>
      </SafeAreaProvider>
    </GestureHandlerRootView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
});
