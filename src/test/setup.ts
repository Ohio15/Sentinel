import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, vi, beforeEach } from 'vitest';

// Cleanup after each test
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

// Mock window.api for Electron IPC
const createMockApi = () => ({
  devices: {
    list: vi.fn().mockResolvedValue({ devices: [], total: 0 }),
    get: vi.fn().mockResolvedValue(null),
    ping: vi.fn().mockResolvedValue({ success: true }),
    delete: vi.fn().mockResolvedValue({ success: true }),
    disable: vi.fn().mockResolvedValue({ success: true }),
    enable: vi.fn().mockResolvedValue({ success: true }),
    uninstall: vi.fn().mockResolvedValue({ success: true }),
    update: vi.fn().mockResolvedValue({ success: true }),
    getMetrics: vi.fn().mockResolvedValue({ metrics: [] }),
    setMetricsInterval: vi.fn().mockResolvedValue({ success: true }),
  },
  commands: {
    execute: vi.fn().mockResolvedValue({ commandId: 'test-id' }),
    getHistory: vi.fn().mockResolvedValue({ commands: [] }),
  },
  terminal: {
    start: vi.fn().mockResolvedValue({ sessionId: 'test-session' }),
    send: vi.fn().mockResolvedValue({ success: true }),
    resize: vi.fn().mockResolvedValue({ success: true }),
    close: vi.fn().mockResolvedValue({ success: true }),
    onData: vi.fn().mockReturnValue(() => {}),
  },
  files: {
    drives: vi.fn().mockResolvedValue({ drives: [] }),
    list: vi.fn().mockResolvedValue({ files: [] }),
    download: vi.fn().mockResolvedValue({ success: true }),
    upload: vi.fn().mockResolvedValue({ success: true }),
    scan: vi.fn().mockResolvedValue({ files: [] }),
    downloadToSandbox: vi.fn().mockResolvedValue({ path: '/sandbox/file' }),
    onProgress: vi.fn().mockReturnValue(() => {}),
  },
  alerts: {
    list: vi.fn().mockResolvedValue({ alerts: [] }),
    acknowledge: vi.fn().mockResolvedValue({ success: true }),
    dismiss: vi.fn().mockResolvedValue({ success: true }),
  },
  settings: {
    get: vi.fn().mockResolvedValue({}),
    update: vi.fn().mockResolvedValue({ success: true }),
  },
  scripts: {
    list: vi.fn().mockResolvedValue({ scripts: [] }),
    get: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockResolvedValue({ id: 'test-id' }),
    update: vi.fn().mockResolvedValue({ success: true }),
    delete: vi.fn().mockResolvedValue({ success: true }),
    execute: vi.fn().mockResolvedValue({ commandId: 'test-id' }),
  },
  clients: {
    list: vi.fn().mockResolvedValue({ clients: [] }),
    get: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockResolvedValue({ id: 'test-id' }),
    update: vi.fn().mockResolvedValue({ success: true }),
    delete: vi.fn().mockResolvedValue({ success: true }),
    getDevices: vi.fn().mockResolvedValue({ devices: [] }),
    assignDevice: vi.fn().mockResolvedValue({ success: true }),
  },
  tickets: {
    list: vi.fn().mockResolvedValue({ tickets: [] }),
    get: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockResolvedValue({ id: 'test-id' }),
    update: vi.fn().mockResolvedValue({ success: true }),
    delete: vi.fn().mockResolvedValue({ success: true }),
    addComment: vi.fn().mockResolvedValue({ success: true }),
  },
  updater: {
    checkForUpdates: vi.fn().mockResolvedValue({ available: false }),
    downloadUpdate: vi.fn().mockResolvedValue({ success: true }),
    installUpdate: vi.fn(),
    getVersion: vi.fn().mockResolvedValue('1.0.0'),
    onUpdateAvailable: vi.fn().mockReturnValue(() => {}),
    onDownloadProgress: vi.fn().mockReturnValue(() => {}),
    onUpdateDownloaded: vi.fn().mockReturnValue(() => {}),
    onError: vi.fn().mockReturnValue(() => {}),
    getDevice: vi.fn().mockResolvedValue(null),
    onStatus: vi.fn().mockReturnValue(() => {}),
  },
  updates: {
    checkForUpdates: vi.fn().mockResolvedValue({ available: false }),
    downloadUpdate: vi.fn().mockResolvedValue({ success: true }),
    installUpdate: vi.fn(),
    onUpdateAvailable: vi.fn().mockReturnValue(() => {}),
    onDownloadProgress: vi.fn().mockReturnValue(() => {}),
    onUpdateDownloaded: vi.fn().mockReturnValue(() => {}),
    onError: vi.fn().mockReturnValue(() => {}),
  },
  getAppVersion: vi.fn().mockResolvedValue('1.0.0'),
  onDeviceUpdate: vi.fn().mockReturnValue(() => {}),
  onAlertUpdate: vi.fn().mockReturnValue(() => {}),
  on: vi.fn().mockReturnValue(() => {}),
});

// Set up global mocks before each test
beforeEach(() => {
  // Reset window.api mock
  (window as any).api = createMockApi();
});

// Mock matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
});

// Mock ResizeObserver
class MockResizeObserver {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}
(window as any).ResizeObserver = MockResizeObserver;

// Mock IntersectionObserver
class MockIntersectionObserver {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
  root = null;
  rootMargin = '';
  thresholds: number[] = [];
}
(window as any).IntersectionObserver = MockIntersectionObserver;
