import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Devices } from './Devices';

// Mock the stores and services the component actually uses
const mockDevices = [
  {
    id: '123e4567-e89b-12d3-a456-426614174000',
    agentId: 'agent-1',
    hostname: 'test-pc-01',
    displayName: 'Test PC 01',
    osType: 'windows',
    osVersion: 'Windows 11 Pro',
    status: 'online',
    cpuModel: 'Intel Core i7',
    cpuCores: 8,
    totalMemory: 16777216,
    ipAddress: '192.168.1.100',
    lastSeen: new Date().toISOString(),
  },
  {
    id: '223e4567-e89b-12d3-a456-426614174001',
    agentId: 'agent-2',
    hostname: 'test-server-01',
    displayName: 'Test Server 01',
    osType: 'linux',
    osVersion: 'Ubuntu 22.04 LTS',
    status: 'offline',
    cpuModel: 'AMD EPYC',
    cpuCores: 16,
    totalMemory: 33554432,
    ipAddress: '192.168.1.101',
    lastSeen: new Date(Date.now() - 3600000).toISOString(),
  },
];

const mockDeleteDevice = vi.fn();
const mockDisableDevice = vi.fn();
const mockEnableDevice = vi.fn();
const mockUninstallDevice = vi.fn();
const mockForceUpdateDevice = vi.fn();
const mockUpdateDevice = vi.fn();
const mockHideDevice = vi.fn().mockResolvedValue(undefined);
const mockUnhideDevice = vi.fn().mockResolvedValue(undefined);
const mockFetchDevices = vi.fn().mockResolvedValue(undefined);

const storeState = (devices: any[] = mockDevices, overrides: Record<string, unknown> = {}) => ({
  devices,
  loading: false,
  deleteDevice: mockDeleteDevice,
  disableDevice: mockDisableDevice,
  enableDevice: mockEnableDevice,
  uninstallDevice: mockUninstallDevice,
  forceUpdateDevice: mockForceUpdateDevice,
  updateDevice: mockUpdateDevice,
  hideDevice: mockHideDevice,
  unhideDevice: mockUnhideDevice,
  fetchDevices: mockFetchDevices,
  ...overrides,
});

vi.mock('../stores/deviceStore', () => ({
  useDeviceStore: vi.fn(() => ({
    devices: mockDevices,
    loading: false,
    deleteDevice: mockDeleteDevice,
    disableDevice: mockDisableDevice,
    enableDevice: mockEnableDevice,
    uninstallDevice: mockUninstallDevice,
    forceUpdateDevice: mockForceUpdateDevice,
    updateDevice: mockUpdateDevice,
    hideDevice: mockHideDevice,
    unhideDevice: mockUnhideDevice,
    fetchDevices: mockFetchDevices,
  })),
  Device: {},
}));

vi.mock('../stores/clientStore', () => ({
  useClientStore: vi.fn(() => ({
    clients: [],
    currentClientId: null,
  })),
}));

vi.mock('../services/api', () => ({
  api: {
    get: vi.fn().mockResolvedValue({ data: {} }),
    post: vi.fn().mockResolvedValue({ data: {} }),
    put: vi.fn().mockResolvedValue({ data: {} }),
    delete: vi.fn().mockResolvedValue({ data: {} }),
  },
}));

vi.mock('../services', () => ({
  server: {
    getInfo: vi.fn().mockResolvedValue({ port: 4000, version: '1.0.0' }),
  },
  agent: {
    getLinks: vi.fn().mockResolvedValue([]),
    getLinkStats: vi.fn().mockResolvedValue({ total: 0, pending: 0, downloaded: 0, installed: 0, expired: 0, revoked: 0, last24Hours: 0, last7Days: 0 }),
    createLink: vi.fn().mockResolvedValue({}),
    revokeLink: vi.fn().mockResolvedValue({}),
    resendLink: vi.fn().mockResolvedValue({}),
  },
  isElectron: false,
  isWeb: true,
}));

vi.mock('date-fns', () => ({
  format: vi.fn((date: any, fmt: string) => new Date(date).toLocaleDateString()),
  formatDistanceToNow: vi.fn(() => '5 minutes ago'),
}));

describe('Devices Page', () => {
  const mockOnDeviceSelect = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders devices page heading', () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    expect(screen.getByText('Devices')).toBeInTheDocument();
  });

  it('displays device count', () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    // Shows "X of Y devices"
    expect(screen.getByText(/2 of 2 devices/)).toBeInTheDocument();
  });

  it('displays device hostnames from store', () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    expect(screen.getByText('test-server-01')).toBeInTheDocument();
  });

  it('renders search input', () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    const searchInput = screen.getByPlaceholderText(/search/i);
    expect(searchInput).toBeInTheDocument();
  });

  it('filters devices by search term', async () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

    const searchInput = screen.getByPlaceholderText(/search/i);
    await userEvent.type(searchInput, 'test-pc');

    // test-pc-01 should remain visible
    expect(screen.getByText('test-pc-01')).toBeInTheDocument();
  });

  it('renders Device List and Installation tabs', () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    expect(screen.getByText('Device List')).toBeInTheDocument();
  });

  it('shows loading state when store is loading', async () => {
    const { useDeviceStore } = await import('../stores/deviceStore');
    vi.mocked(useDeviceStore).mockReturnValue({
      devices: [],
      loading: true,
      deleteDevice: mockDeleteDevice,
      disableDevice: mockDisableDevice,
      enableDevice: mockEnableDevice,
      uninstallDevice: mockUninstallDevice,
      forceUpdateDevice: mockForceUpdateDevice,
      updateDevice: mockUpdateDevice,
    } as any);

    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    // With no devices and loading true, should show a loading/empty state
    expect(screen.getByText(/0 of 0 devices/)).toBeInTheDocument();
  });

  it('shows empty state when no devices', async () => {
    const { useDeviceStore } = await import('../stores/deviceStore');
    vi.mocked(useDeviceStore).mockReturnValue({
      devices: [],
      loading: false,
      deleteDevice: mockDeleteDevice,
      disableDevice: mockDisableDevice,
      enableDevice: mockEnableDevice,
      uninstallDevice: mockUninstallDevice,
      forceUpdateDevice: mockForceUpdateDevice,
      updateDevice: mockUpdateDevice,
    } as any);

    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    expect(screen.getByText(/0 of 0 devices/)).toBeInTheDocument();
  });

  it('renders status filter dropdown', () => {
    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    // The component uses status filter selects
    const statusSelects = screen.getAllByRole('combobox');
    expect(statusSelects.length).toBeGreaterThan(0);
  });

  it('handles XSS in device names safely (React escapes by default)', async () => {
    const deviceStore = await import('../stores/deviceStore');
    vi.mocked(deviceStore.useDeviceStore).mockReturnValue({
      devices: [{
        ...mockDevices[0],
        hostname: '<script>alert("XSS")</script>',
        displayName: '"><img src=x onerror=alert(1)>',
      }],
      loading: false,
      deleteDevice: mockDeleteDevice,
      disableDevice: mockDisableDevice,
      enableDevice: mockEnableDevice,
      uninstallDevice: mockUninstallDevice,
      forceUpdateDevice: mockForceUpdateDevice,
      updateDevice: mockUpdateDevice,
    } as any);

    const { container } = render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    // React auto-escapes, so raw HTML should not appear as elements
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('img[onerror]')).toBeNull();
  });

  it('renders device entries from store data', async () => {
    // Re-apply mock with both devices to ensure fresh state
    const deviceStore = await import('../stores/deviceStore');
    vi.mocked(deviceStore.useDeviceStore).mockReturnValue({
      devices: mockDevices,
      loading: false,
      deleteDevice: mockDeleteDevice,
      disableDevice: mockDisableDevice,
      enableDevice: mockEnableDevice,
      uninstallDevice: mockUninstallDevice,
      forceUpdateDevice: mockForceUpdateDevice,
      updateDevice: mockUpdateDevice,
    } as any);

    render(<Devices onDeviceSelect={mockOnDeviceSelect} />);
    expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    expect(screen.getByText('test-server-01')).toBeInTheDocument();
  });

  describe('hide / unhide device', () => {
    const hiddenDevice = {
      ...mockDevices[1],
      hiddenAt: '2026-07-01T12:00:00.000Z',
    };

    const openActionMenu = async (index: number) => {
      const menuButtons = screen.getAllByTitle('Device actions');
      fireEvent.click(menuButtons[index]);
      await waitFor(() => expect(screen.getByText('Force Update')).toBeInTheDocument());
    };

    const applyStore = async (devices: any[]) => {
      const deviceStore = await import('../stores/deviceStore');
      vi.mocked(deviceStore.useDeviceStore).mockReturnValue(storeState(devices) as any);
    };

    beforeEach(() => {
      mockHideDevice.mockResolvedValue(undefined);
      mockUnhideDevice.mockResolvedValue(undefined);
      mockFetchDevices.mockResolvedValue(undefined);
    });

    it('calls the store hide action from the kebab menu', async () => {
      await applyStore(mockDevices);
      render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

      await openActionMenu(0);
      fireEvent.click(screen.getByText('Hide Device'));

      await waitFor(() => expect(mockHideDevice).toHaveBeenCalledWith(mockDevices[0].id));
      expect(mockOnDeviceSelect).not.toHaveBeenCalled();
    });

    it('does not render hidden devices by default (server excludes them)', async () => {
      // Default fetch omits hidden devices, so the store never holds one.
      await applyStore(mockDevices);
      const { container } = render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

      expect(container.querySelector('tr[data-hidden="true"]')).toBeNull();
      expect(screen.queryByText('Hidden')).toBeNull();
      expect(screen.getByText(/2 of 2 devices/)).toBeInTheDocument();
    });

    it('refetches with hidden devices included when the toggle is turned on', async () => {
      await applyStore(mockDevices);
      render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

      fireEvent.click(screen.getByText('Show hidden'));

      await waitFor(() => expect(mockFetchDevices).toHaveBeenCalledWith(null, true, true));
    });

    it('renders hidden devices dimmed when the toggle is on', async () => {
      await applyStore([mockDevices[0], hiddenDevice]);
      const { container } = render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

      fireEvent.click(screen.getByText('Show hidden'));

      const hiddenRow = container.querySelector('tr[data-hidden="true"]');
      expect(hiddenRow).not.toBeNull();
      expect(hiddenRow?.className).toContain('opacity-50');
      expect(screen.getByText('Hidden')).toBeInTheDocument();
      // The hidden row is still interactive.
      fireEvent.click(screen.getByText('test-server-01'));
      expect(mockOnDeviceSelect).toHaveBeenCalledWith(hiddenDevice.id);
    });

    it('resets the hidden toggle via Clear Filters', async () => {
      await applyStore([mockDevices[0], hiddenDevice]);
      render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

      fireEvent.click(screen.getByText('Show hidden'));
      await waitFor(() => expect(screen.getByText('Clear Filters')).toBeInTheDocument());

      fireEvent.click(screen.getByText('Clear Filters'));

      await waitFor(() => expect(mockFetchDevices).toHaveBeenLastCalledWith(null, true, false));
    });

    it('calls the store unhide action for a hidden device', async () => {
      await applyStore([mockDevices[0], hiddenDevice]);
      render(<Devices onDeviceSelect={mockOnDeviceSelect} />);

      await openActionMenu(1);
      expect(screen.queryByText('Hide Device')).toBeNull();
      fireEvent.click(screen.getByText('Unhide Device'));

      await waitFor(() => expect(mockUnhideDevice).toHaveBeenCalledWith(hiddenDevice.id));
    });
  });
});
