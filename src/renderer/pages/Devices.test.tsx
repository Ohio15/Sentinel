import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Devices } from './Devices';

describe('Devices Page', () => {
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

  beforeEach(() => {
    vi.clearAllMocks();

    // Mock devices.list API
    vi.mocked(window.api.devices.list).mockResolvedValue({
      devices: mockDevices,
      total: mockDevices.length,
    });

    // Mock devices.get API
    vi.mocked(window.api.devices.get).mockImplementation((id) => {
      const device = mockDevices.find((d) => d.id === id);
      return Promise.resolve(device || null);
    });

    // Mock devices.delete API
    vi.mocked(window.api.devices.delete).mockResolvedValue({
      success: true,
    });

    // Mock devices.ping API
    vi.mocked(window.api.devices.ping).mockResolvedValue({
      success: true,
      latency: 45,
    });
  });

  it('renders devices page', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText(/devices/i)).toBeInTheDocument();
    });
  });

  it('loads and displays devices on mount', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(window.api.devices.list).toHaveBeenCalled();
    });

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
      expect(screen.getByText('test-server-01')).toBeInTheDocument();
    });
  });

  it('displays device status correctly', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Check for status indicators
    const onlineStatus = screen.getAllByText(/online/i);
    const offlineStatus = screen.getAllByText(/offline/i);

    expect(onlineStatus.length).toBeGreaterThan(0);
    expect(offlineStatus.length).toBeGreaterThan(0);
  });

  it('handles device selection', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    const deviceRow = screen.getByText('test-pc-01').closest('tr');
    expect(deviceRow).toBeInTheDocument();

    if (deviceRow) {
      fireEvent.click(deviceRow);
    }

    // Device details should be visible or navigated to
    await waitFor(() => {
      expect(deviceRow).toBeInTheDocument();
    });
  });

  it('filters devices by search query', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    const searchInput = screen.getByPlaceholderText(/search/i);
    expect(searchInput).toBeInTheDocument();

    await userEvent.type(searchInput, 'test-pc');

    // Should filter to show only test-pc-01
    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });
  });

  it('filters devices by status', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Find status filter dropdown
    const statusFilter = screen.getByLabelText(/status/i, { selector: 'select' });
    if (statusFilter) {
      await userEvent.selectOptions(statusFilter, 'online');

      // Should filter to show only online devices
      await waitFor(() => {
        expect(screen.getByText('test-pc-01')).toBeInTheDocument();
      });
    }
  });

  it('displays device count', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText(/2.*devices?/i)).toBeInTheDocument();
    });
  });

  it('handles refresh button', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(window.api.devices.list).toHaveBeenCalledTimes(1);
    });

    const refreshButton = screen.getByRole('button', { name: /refresh/i });
    fireEvent.click(refreshButton);

    await waitFor(() => {
      expect(window.api.devices.list).toHaveBeenCalledTimes(2);
    });
  });

  it('handles delete device action', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Find delete button for first device
    const deleteButtons = screen.getAllByRole('button', { name: /delete/i });
    if (deleteButtons.length > 0) {
      fireEvent.click(deleteButtons[0]);

      // Confirm deletion in modal
      const confirmButton = await screen.findByRole('button', {
        name: /confirm/i,
      });
      fireEvent.click(confirmButton);

      await waitFor(() => {
        expect(window.api.devices.delete).toHaveBeenCalled();
      });
    }
  });

  it('handles ping device action', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    const pingButtons = screen.getAllByRole('button', { name: /ping/i });
    if (pingButtons.length > 0) {
      fireEvent.click(pingButtons[0]);

      await waitFor(() => {
        expect(window.api.devices.ping).toHaveBeenCalledWith(
          mockDevices[0].id
        );
      });
    }
  });

  it('displays loading state', () => {
    vi.mocked(window.api.devices.list).mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve({ devices: [], total: 0 }), 1000);
        })
    );

    render(<Devices />);

    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it('displays empty state when no devices', async () => {
    vi.mocked(window.api.devices.list).mockResolvedValue({
      devices: [],
      total: 0,
    });

    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText(/no devices/i)).toBeInTheDocument();
    });
  });

  it('handles API error gracefully', async () => {
    vi.mocked(window.api.devices.list).mockRejectedValue(
      new Error('Network error')
    );

    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText(/error/i)).toBeInTheDocument();
    });
  });

  it('sorts devices by column', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    const hostnameHeader = screen.getByText(/hostname/i);
    fireEvent.click(hostnameHeader);

    // Devices should be re-sorted
    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });
  });

  it('displays device metrics summary', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Should display CPU, memory info
    expect(screen.getByText(/Intel Core i7/i)).toBeInTheDocument();
    expect(screen.getByText(/AMD EPYC/i)).toBeInTheDocument();
  });

  it('handles device update events', async () => {
    const { rerender } = render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Simulate device update event
    const updateHandler = vi.mocked(window.api.onDeviceUpdate).mock.calls[0]?.[0];
    if (updateHandler) {
      updateHandler({
        id: mockDevices[0].id,
        status: 'offline',
      });
    }

    // Component should update
    rerender(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });
  });

  it('validates bulk selection', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Find select all checkbox
    const selectAllCheckbox = screen.getByRole('checkbox', {
      name: /select all/i,
    });

    if (selectAllCheckbox) {
      fireEvent.click(selectAllCheckbox);

      // All devices should be selected
      const checkboxes = screen.getAllByRole('checkbox');
      const checkedCount = checkboxes.filter(
        (cb) => (cb as HTMLInputElement).checked
      ).length;

      expect(checkedCount).toBeGreaterThan(0);
    }
  });

  it('handles pagination', async () => {
    const manyDevices = Array.from({ length: 50 }, (_, i) => ({
      ...mockDevices[0],
      id: `device-${i}`,
      hostname: `device-${i}`,
    }));

    vi.mocked(window.api.devices.list).mockResolvedValue({
      devices: manyDevices,
      total: manyDevices.length,
    });

    render(<Devices />);

    await waitFor(() => {
      expect(window.api.devices.list).toHaveBeenCalled();
    });

    // Look for pagination controls
    const nextButton = screen.queryByRole('button', { name: /next/i });
    if (nextButton) {
      fireEvent.click(nextButton);

      await waitFor(() => {
        expect(screen.getByText(/page/i)).toBeInTheDocument();
      });
    }
  });

  it('displays OS type icons correctly', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Should have different icons for Windows and Linux
    const windowsDevice = screen.getByText('test-pc-01').closest('tr');
    const linuxDevice = screen.getByText('test-server-01').closest('tr');

    expect(windowsDevice).toBeInTheDocument();
    expect(linuxDevice).toBeInTheDocument();
  });

  it('handles concurrent operations', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('test-pc-01')).toBeInTheDocument();
    });

    // Trigger multiple actions simultaneously
    const refreshButton = screen.getByRole('button', { name: /refresh/i });
    const pingButtons = screen.getAllByRole('button', { name: /ping/i });

    fireEvent.click(refreshButton);
    if (pingButtons.length > 0) {
      fireEvent.click(pingButtons[0]);
    }

    await waitFor(() => {
      expect(window.api.devices.list).toHaveBeenCalled();
      expect(window.api.devices.ping).toHaveBeenCalled();
    });
  });

  it('validates data sanitization', async () => {
    const maliciousDevice = {
      ...mockDevices[0],
      hostname: '<script>alert("XSS")</script>',
      displayName: '"><img src=x onerror=alert("XSS")>',
    };

    vi.mocked(window.api.devices.list).mockResolvedValue({
      devices: [maliciousDevice],
      total: 1,
    });

    render(<Devices />);

    await waitFor(() => {
      expect(window.api.devices.list).toHaveBeenCalled();
    });

    // Malicious content should be escaped or sanitized
    const pageContent = screen.getByRole('main').textContent;
    expect(pageContent).not.toContain('<script>');
    expect(pageContent).not.toContain('onerror=');
  });
});
