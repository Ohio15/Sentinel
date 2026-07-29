// A device is "online" only while it holds a live WebSocket connection (the
// backend status). `last_seen`, however, is also refreshed by the gRPC
// data-plane independently of that socket — so a device can read "offline" yet
// have checked in seconds ago (common for laptops that sleep or machines on
// flaky/NAT networks). We surface that middle ground as "idle" so a
// recently-active machine is not displayed as dead.
export const IDLE_THRESHOLD_MIN = 15;
export const IDLE_THRESHOLD_MS = IDLE_THRESHOLD_MIN * 60 * 1000;

// Shared UI copy so the threshold shown to users can never drift from the
// threshold actually applied above.
export const IDLE_TOOLTIP =
  `Checked in within the last ${IDLE_THRESHOLD_MIN} min but not holding a live connection`;

export type DeviceDisplayState =
  | 'online'
  | 'idle'
  | 'offline'
  | 'warning'
  | 'critical'
  | 'disabled'
  | 'uninstalling';

/**
 * Derive the display state for a device, adding an "idle" tier between
 * online and offline. Explicit lifecycle/health states (disabled,
 * uninstalling, warning, critical) are preserved as-is.
 */
export function getDeviceState(
  device: { status: string; lastSeen?: string | null },
): DeviceDisplayState {
  if (
    device.status === 'disabled' ||
    device.status === 'uninstalling' ||
    device.status === 'warning' ||
    device.status === 'critical'
  ) {
    return device.status;
  }
  if (device.status === 'online') return 'online';
  const last = device.lastSeen ? new Date(device.lastSeen).getTime() : NaN;
  if (!Number.isNaN(last) && Date.now() - last <= IDLE_THRESHOLD_MS) {
    return 'idle';
  }
  return 'offline';
}
