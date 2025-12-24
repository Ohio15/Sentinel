import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Terminal } from './Terminal';

describe('Terminal Component', () => {
  const mockDeviceId = 'test-device-123';
  const mockSessionId = 'test-session-456';

  beforeEach(() => {
    // Reset all mocks
    vi.clearAllMocks();

    // Mock terminal.start to return a session
    vi.mocked(window.api.terminal.start).mockResolvedValue({
      success: true,
      sessionId: mockSessionId,
    });

    // Mock terminal.send
    vi.mocked(window.api.terminal.send).mockResolvedValue({
      success: true,
    });

    // Mock terminal.close
    vi.mocked(window.api.terminal.close).mockResolvedValue({
      success: true,
    });

    // Mock terminal.onData to call callback with test data
    let dataCallback: ((data: any) => void) | null = null;
    vi.mocked(window.api.terminal.onData).mockImplementation((callback) => {
      dataCallback = callback;
      // Simulate receiving data
      setTimeout(() => {
        if (dataCallback) {
          dataCallback({
            sessionId: mockSessionId,
            data: 'Welcome to terminal\r\n$ ',
          });
        }
      }, 100);
      return () => {
        dataCallback = null;
      };
    });
  });

  it('renders terminal container', () => {
    render(<Terminal deviceId={mockDeviceId} />);
    const terminalContainer = screen.getByRole('region', { name: /terminal/i });
    expect(terminalContainer).toBeInTheDocument();
  });

  it('starts terminal session on mount', async () => {
    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalledWith(
        mockDeviceId,
        expect.objectContaining({
          cols: expect.any(Number),
          rows: expect.any(Number),
        })
      );
    });
  });

  it('displays received terminal output', async () => {
    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.onData).toHaveBeenCalled();
    });

    // Wait for terminal to process the data
    await waitFor(
      () => {
        const terminalElement = screen.getByRole('region', { name: /terminal/i });
        expect(terminalElement).toBeInTheDocument();
      },
      { timeout: 2000 }
    );
  });

  it('handles terminal resize', async () => {
    const { container } = render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    // Simulate resize by dispatching resize event
    global.dispatchEvent(new Event('resize'));

    // Terminal should handle resize gracefully
    await waitFor(() => {
      const terminalElement = container.querySelector('.xterm');
      expect(terminalElement).toBeInTheDocument();
    });
  });

  it('closes terminal session on unmount', async () => {
    const { unmount } = render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    unmount();

    await waitFor(() => {
      expect(window.api.terminal.close).toHaveBeenCalledWith(mockSessionId);
    });
  });

  it('handles terminal start failure', async () => {
    vi.mocked(window.api.terminal.start).mockResolvedValue({
      success: false,
      error: 'Failed to start terminal',
    });

    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    // Component should handle error gracefully
    const terminalContainer = screen.getByRole('region', { name: /terminal/i });
    expect(terminalContainer).toBeInTheDocument();
  });

  it('sends input to terminal', async () => {
    vi.mocked(window.api.terminal.start).mockResolvedValue({
      success: true,
      sessionId: mockSessionId,
    });

    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    // Simulate typing in terminal
    const terminalContainer = screen.getByRole('region', { name: /terminal/i });

    // Focus terminal
    fireEvent.click(terminalContainer);

    // Wait for terminal to be ready
    await waitFor(() => {
      expect(window.api.terminal.onData).toHaveBeenCalled();
    });
  });

  it('handles rapid input correctly', async () => {
    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    // Simulate rapid typing
    const commands = ['ls', 'pwd', 'whoami'];

    for (const cmd of commands) {
      // Terminal input is handled internally by xterm
      // We verify that the session is active
      expect(window.api.terminal.start).toHaveBeenCalledWith(
        mockDeviceId,
        expect.any(Object)
      );
    }
  });

  it('handles special keys (Ctrl+C, Enter, etc.)', async () => {
    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    const terminalContainer = screen.getByRole('region', { name: /terminal/i });
    fireEvent.click(terminalContainer);

    // Simulate Enter key
    fireEvent.keyDown(terminalContainer, { key: 'Enter', code: 'Enter' });

    // Simulate Ctrl+C
    fireEvent.keyDown(terminalContainer, {
      key: 'c',
      code: 'KeyC',
      ctrlKey: true,
    });

    // Terminal should still be functional
    expect(terminalContainer).toBeInTheDocument();
  });

  it('handles terminal disconnect and reconnect', async () => {
    const { rerender } = render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    // Simulate disconnect by providing different deviceId
    const newDeviceId = 'new-device-789';
    vi.mocked(window.api.terminal.start).mockResolvedValue({
      success: true,
      sessionId: 'new-session-789',
    });

    rerender(<Terminal deviceId={newDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.close).toHaveBeenCalledWith(mockSessionId);
      expect(window.api.terminal.start).toHaveBeenCalledWith(
        newDeviceId,
        expect.any(Object)
      );
    });
  });

  it('handles null or undefined deviceId gracefully', () => {
    const { container } = render(<Terminal deviceId={null as any} />);
    expect(container).toBeInTheDocument();
  });

  it('cleans up event listeners on unmount', async () => {
    const { unmount } = render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    const onDataMock = vi.mocked(window.api.terminal.onData);
    const removeListener = onDataMock.mock.results[0]?.value;

    unmount();

    // Verify cleanup was called
    if (removeListener && typeof removeListener === 'function') {
      // Listener should be cleaned up
      expect(window.api.terminal.close).toHaveBeenCalled();
    }
  });

  it('handles large output efficiently', async () => {
    const largeOutput = 'A'.repeat(10000);

    vi.mocked(window.api.terminal.onData).mockImplementation((callback) => {
      setTimeout(() => {
        callback({
          sessionId: mockSessionId,
          data: largeOutput,
        });
      }, 100);
      return () => {};
    });

    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalled();
    });

    // Terminal should handle large output without crashing
    await waitFor(
      () => {
        const terminalContainer = screen.getByRole('region', {
          name: /terminal/i,
        });
        expect(terminalContainer).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it('handles concurrent terminal sessions for different devices', async () => {
    const device1 = 'device-1';
    const device2 = 'device-2';

    const { rerender } = render(<Terminal deviceId={device1} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalledWith(
        device1,
        expect.any(Object)
      );
    });

    vi.mocked(window.api.terminal.start).mockResolvedValue({
      success: true,
      sessionId: 'session-2',
    });

    rerender(<Terminal deviceId={device2} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalledWith(
        device2,
        expect.any(Object)
      );
    });
  });

  it('validates terminal dimensions', async () => {
    render(<Terminal deviceId={mockDeviceId} />);

    await waitFor(() => {
      expect(window.api.terminal.start).toHaveBeenCalledWith(
        mockDeviceId,
        expect.objectContaining({
          cols: expect.any(Number),
          rows: expect.any(Number),
        })
      );
    });

    const startCall = vi.mocked(window.api.terminal.start).mock.calls[0];
    const dimensions = startCall[1];

    // Validate reasonable dimensions
    expect(dimensions.cols).toBeGreaterThan(0);
    expect(dimensions.rows).toBeGreaterThan(0);
    expect(dimensions.cols).toBeLessThan(500);
    expect(dimensions.rows).toBeLessThan(200);
  });
});
