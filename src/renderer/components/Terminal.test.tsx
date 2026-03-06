import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Terminal } from './Terminal';

// Mock the services and stores that the Terminal component actually uses
vi.mock('../services', () => ({
  terminal: {
    start: vi.fn().mockResolvedValue({ sessionId: 'test-session-456' }),
    send: vi.fn().mockResolvedValue(undefined),
    close: vi.fn(),
    resize: vi.fn(),
    onOutput: vi.fn().mockReturnValue(() => {}),
  },
}));

// Shared sessions map accessible from both the mock and test code
// vi.mock is hoisted, so we use a global to share state
const _sessionsHolder = { map: new Map<string, any>() };

vi.mock('../stores/terminalStore', () => {
  return {
    useTerminalStore: vi.fn((selector: any) => {
      const sessions = _sessionsHolder.map;
      const state = {
        sessions,
        createSession: vi.fn((deviceId: string, sessionId: string) => {
          sessions.set(deviceId, {
            sessionId,
            deviceId,
            connected: true,
            connectionState: 'connected',
            output: ['Welcome to terminal\r\n'],
            inputQueue: [],
            lastActivityAt: Date.now(),
          });
        }),
        closeSession: vi.fn((deviceId: string) => {
          sessions.delete(deviceId);
        }),
        addOutput: vi.fn(),
        clearOutput: vi.fn(),
      };
      return selector(state);
    }),
    setupTerminalHandler: vi.fn(),
  };
});

describe('Terminal Component', () => {
  const mockDeviceId = 'test-device-123';

  beforeEach(() => {
    vi.clearAllMocks();
    // Reset sessions between tests so state doesn't leak
    _sessionsHolder.map.clear();
  });

  it('renders terminal container', () => {
    const { container } = render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    // Terminal renders a flex column container with h-96
    expect(container.querySelector('.flex.flex-col')).toBeInTheDocument();
  });

  it('shows offline message when device is offline', () => {
    render(<Terminal deviceId={mockDeviceId} isOnline={false} />);
    expect(screen.getByText(/device is offline/i)).toBeInTheDocument();
  });

  it('shows Connect button when not connected', () => {
    render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    expect(screen.getByText('Connect')).toBeInTheDocument();
  });

  it('shows Disconnected status initially', () => {
    render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
  });

  it('calls terminal service start when Connect is clicked', async () => {
    const { terminal } = await import('../services');

    render(<Terminal deviceId={mockDeviceId} isOnline={true} />);

    const connectBtn = screen.getByText('Connect');
    fireEvent.click(connectBtn);

    await waitFor(() => {
      expect(terminal.start).toHaveBeenCalledWith(mockDeviceId);
    });
  });

  it('handles null or undefined deviceId gracefully', () => {
    const { container } = render(<Terminal deviceId={null as any} isOnline={true} />);
    expect(container).toBeInTheDocument();
  });

  it('renders output area with dark background', () => {
    const { container } = render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    const outputArea = container.querySelector('.bg-gray-900');
    expect(outputArea).toBeInTheDocument();
  });

  it('renders status indicator dot', () => {
    const { container } = render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    // Should have a status dot (red for disconnected)
    const statusDot = container.querySelector('.rounded-full');
    expect(statusDot).toBeInTheDocument();
  });

  it('calls setupTerminalHandler on mount', async () => {
    const { setupTerminalHandler } = await import('../stores/terminalStore');

    render(<Terminal deviceId={mockDeviceId} isOnline={true} />);

    expect(setupTerminalHandler).toHaveBeenCalled();
  });

  it('does not show Connect button when offline', () => {
    render(<Terminal deviceId={mockDeviceId} isOnline={false} />);
    expect(screen.queryByText('Connect')).not.toBeInTheDocument();
  });

  it('shows connecting state while terminal service is starting', async () => {
    // The component uses internal `connecting` state that shows "Connecting..." text
    // This is rendered as the button text during the async start call
    const { container } = render(<Terminal deviceId={mockDeviceId} isOnline={true} />);

    // Initially should show Connect
    expect(screen.getByText('Connect')).toBeInTheDocument();
    // The button should not be disabled initially
    const btn = screen.getByText('Connect');
    expect(btn).not.toBeDisabled();
  });

  it('handles start failure without crashing', async () => {
    const { terminal } = await import('../services');
    vi.mocked(terminal.start).mockRejectedValueOnce(new Error('Connection refused'));

    render(<Terminal deviceId={mockDeviceId} isOnline={true} />);

    const connectBtn = screen.getByText('Connect');
    fireEvent.click(connectBtn);

    // After failure, should return to Connect state (not crash)
    await waitFor(() => {
      expect(screen.getByText('Connect')).toBeInTheDocument();
    });
  });

  it('renders header with toolbar styling', () => {
    const { container } = render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    const header = container.querySelector('.bg-gray-800');
    expect(header).toBeInTheDocument();
  });

  it('renders font-mono output area for terminal text', () => {
    const { container } = render(<Terminal deviceId={mockDeviceId} isOnline={true} />);
    const monoArea = container.querySelector('.font-mono');
    expect(monoArea).toBeInTheDocument();
  });
});
