import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Sidebar } from './Sidebar';

describe('Sidebar', () => {
  const mockOnNavigate = vi.fn();

  beforeEach(() => {
    mockOnNavigate.mockClear();
    // Mock the version API
    vi.mocked(window.api.updater.getVersion).mockResolvedValue('1.0.0');
  });

  it('renders the Sentinel logo and title', () => {
    render(<Sidebar currentPage="dashboard" onNavigate={mockOnNavigate} />);

    expect(screen.getByText('Sentinel')).toBeInTheDocument();
    expect(screen.getByText('RMM Platform')).toBeInTheDocument();
  });

  it('renders all menu items', () => {
    render(<Sidebar currentPage="dashboard" onNavigate={mockOnNavigate} />);

    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Clients')).toBeInTheDocument();
    expect(screen.getByText('Devices')).toBeInTheDocument();
    expect(screen.getByText('Tickets')).toBeInTheDocument();
    expect(screen.getByText('Alerts')).toBeInTheDocument();
    expect(screen.getByText('Scripts')).toBeInTheDocument();
    expect(screen.getByText('Certificates')).toBeInTheDocument();
    expect(screen.getByText('Knowledge Base')).toBeInTheDocument();
    expect(screen.getByText('Settings')).toBeInTheDocument();
  });

  it('highlights the current page', () => {
    render(<Sidebar currentPage="devices" onNavigate={mockOnNavigate} />);

    const devicesButton = screen.getByText('Devices').closest('button');
    expect(devicesButton).toHaveClass('bg-primary-light');
  });

  it('calls onNavigate when a menu item is clicked', () => {
    render(<Sidebar currentPage="dashboard" onNavigate={mockOnNavigate} />);

    fireEvent.click(screen.getByText('Devices'));
    expect(mockOnNavigate).toHaveBeenCalledWith('devices');
  });

  it('displays version in footer', async () => {
    render(<Sidebar currentPage="dashboard" onNavigate={mockOnNavigate} />);

    // Wait for the version to be loaded
    const versionText = await screen.findByText(/Version 1\.0\.0/);
    expect(versionText).toBeInTheDocument();
  });

  it('highlights devices menu when on device-detail page', () => {
    render(<Sidebar currentPage="device-detail" onNavigate={mockOnNavigate} />);

    const devicesButton = screen.getByText('Devices').closest('button');
    expect(devicesButton).toHaveClass('bg-primary-light');
  });

  it('highlights tickets menu when on tickets-kanban page', () => {
    render(<Sidebar currentPage="tickets-kanban" onNavigate={mockOnNavigate} />);

    const ticketsButton = screen.getByText('Tickets').closest('button');
    expect(ticketsButton).toHaveClass('bg-primary-light');
  });
});
