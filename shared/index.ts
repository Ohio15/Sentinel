/**
 * Sentinel RMM Shared Module
 *
 * Shared types, constants, and utilities for web and mobile applications.
 * This module is framework-agnostic and can be used with React, React Native,
 * or any other TypeScript-based framework.
 *
 * Usage:
 *   import { Device, DeviceStatus, formatRelativeTime } from '@sentinel/shared';
 *   // or
 *   import { Device } from '@sentinel/shared/types';
 *   import { DEVICE_STATUS } from '@sentinel/shared/constants';
 *   import { formatRelativeTime } from '@sentinel/shared/utils';
 */

// Re-export all types
export * from './types';

// Re-export all constants
export * from './constants';

// Re-export all utilities
export * from './utils';
