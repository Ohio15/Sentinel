// Global type declarations for Web mode

/// <reference types="vite/client" />

// Load the @testing-library/jest-dom matcher augmentations for Vitest's
// `Assertion` interface (e.g. toBeInTheDocument, toHaveClass, toBeDisabled).
// The Vitest setup file imports this at runtime, but it lives outside this
// tsconfig's `include` scope, so the type augmentation must be referenced here
// for `tsc --noEmit` to see the matchers on `expect(...)` in test files.
import '@testing-library/jest-dom/vitest';

// In web mode there is no Electron `window.api` IPC bridge at runtime; it only
// exists as a Vitest mock (see src/test/setup.ts). This declaration mirrors that
// mock's surface so test files can reference `window.api.*` under strict tsc.
interface SentinelWindowApi {
  devices: {
    list: (clientId?: string) => Promise<{ devices: unknown[]; total: number }>;
    get: (id: string) => Promise<unknown | null>;
    ping: (deviceId: string) => Promise<{ success: boolean }>;
    delete: (id: string) => Promise<{ success: boolean }>;
    disable: (id: string) => Promise<{ success: boolean }>;
    enable: (id: string) => Promise<{ success: boolean }>;
    uninstall: (id: string) => Promise<{ success: boolean }>;
    update: (id: string, updates: unknown) => Promise<{ success: boolean }>;
    getMetrics: (deviceId: string, hours?: number) => Promise<{ metrics: unknown[] }>;
    setMetricsInterval: (deviceId: string, intervalMs: number) => Promise<{ success: boolean }>;
  };
  commands: {
    execute: (deviceId: string, command: string, type: string) => Promise<{ commandId: string }>;
    getHistory: (deviceId: string) => Promise<{ commands: unknown[] }>;
  };
  terminal: {
    start: (deviceId: string) => Promise<{ sessionId: string }>;
    send: (sessionId: string, data: string) => Promise<{ success: boolean }>;
    resize: (sessionId: string, cols: number, rows: number) => Promise<{ success: boolean }>;
    close: (sessionId: string) => Promise<{ success: boolean }>;
    onData: (callback: (data: string) => void) => () => void;
  };
  files: {
    drives: (deviceId: string) => Promise<{ drives: unknown[] }>;
    list: (deviceId: string, path: string) => Promise<{ files: unknown[] }>;
    download: (deviceId: string, remotePath: string, localPath: string) => Promise<{ success: boolean }>;
    upload: (deviceId: string, localPath: string, remotePath: string) => Promise<{ success: boolean }>;
    scan: (deviceId: string, path: string, maxDepth?: number) => Promise<{ files: unknown[] }>;
    downloadToSandbox: (deviceId: string, remotePath: string) => Promise<{ path: string }>;
    onProgress: (callback: (progress: unknown) => void) => () => void;
  };
  alerts: {
    list: () => Promise<{ alerts: unknown[] }>;
    acknowledge: (id: string) => Promise<{ success: boolean }>;
    dismiss: (id: string) => Promise<{ success: boolean }>;
  };
  settings: {
    get: () => Promise<Record<string, unknown>>;
    update: (settings: unknown) => Promise<{ success: boolean }>;
  };
  scripts: {
    list: () => Promise<{ scripts: unknown[] }>;
    get: (id: string) => Promise<unknown | null>;
    create: (script: unknown) => Promise<{ id: string }>;
    update: (id: string, script: unknown) => Promise<{ success: boolean }>;
    delete: (id: string) => Promise<{ success: boolean }>;
    execute: (scriptId: string, deviceIds?: string[]) => Promise<{ commandId: string }>;
  };
  clients: {
    list: () => Promise<{ clients: unknown[] }>;
    get: (id: string) => Promise<unknown | null>;
    create: (client: unknown) => Promise<{ id: string }>;
    update: (id: string, client: unknown) => Promise<{ success: boolean }>;
    delete: (id: string) => Promise<{ success: boolean }>;
    getDevices: (clientId: string) => Promise<{ devices: unknown[] }>;
    assignDevice: (deviceId: string, clientId: string | null) => Promise<{ success: boolean }>;
  };
  tickets: {
    list: () => Promise<{ tickets: unknown[] }>;
    get: (id: string) => Promise<unknown | null>;
    create: (ticket: unknown) => Promise<{ id: string }>;
    update: (id: string, updates: unknown) => Promise<{ success: boolean }>;
    delete: (id: string) => Promise<{ success: boolean }>;
    addComment: (comment: unknown) => Promise<{ success: boolean }>;
  };
  updater: {
    checkForUpdates: () => Promise<{ available: boolean }>;
    downloadUpdate: () => Promise<{ success: boolean }>;
    installUpdate: () => void;
    getVersion: () => Promise<string>;
    onUpdateAvailable: (callback: (info: unknown) => void) => () => void;
    onDownloadProgress: (callback: (progress: unknown) => void) => () => void;
    onUpdateDownloaded: (callback: (info: unknown) => void) => () => void;
    onError: (callback: (error: unknown) => void) => () => void;
    getDevice: (deviceId: string) => Promise<unknown | null>;
    onStatus: (callback: (data: unknown) => void) => () => void;
  };
  updates: {
    checkForUpdates: () => Promise<{ available: boolean }>;
    downloadUpdate: () => Promise<{ success: boolean }>;
    installUpdate: () => void;
    onUpdateAvailable: (callback: (info: unknown) => void) => () => void;
    onDownloadProgress: (callback: (progress: unknown) => void) => () => void;
    onUpdateDownloaded: (callback: (info: unknown) => void) => () => void;
    onError: (callback: (error: unknown) => void) => () => void;
  };
  getAppVersion: () => Promise<string>;
  onDeviceUpdate: (callback: (data: unknown) => void) => () => void;
  onAlertUpdate: (callback: (data: unknown) => void) => () => void;
  on: (channel: string, callback: (...args: unknown[]) => void) => () => void;
}

declare global {
  interface Window {
    api: SentinelWindowApi;
  }
}

export {};
