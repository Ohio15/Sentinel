import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { Login } from './Login';

// Mock react-router-dom navigation
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// Mock the auth store
const mockLogin = vi.fn();
const mockClearError = vi.fn();
let mockStoreState = {
  login: mockLogin,
  isLoading: false,
  error: null as string | null,
  clearError: mockClearError,
};

vi.mock('../stores/authStore', () => ({
  useAuthStore: (selector?: (state: typeof mockStoreState) => unknown) => {
    if (typeof selector === 'function') return selector(mockStoreState);
    return mockStoreState;
  },
}));

// Mock the api service
vi.mock('../services/api', () => ({
  api: {
    beginPasskeyAuthentication: vi.fn(),
    finishPasskeyAuthentication: vi.fn(),
    getPasskeys: vi.fn().mockResolvedValue([]),
  },
}));

// Mock connection service
vi.mock('../services', () => ({
  connection: {
    connect: vi.fn(),
  },
}));

// Mock react-hot-toast
vi.mock('react-hot-toast', () => ({
  default: Object.assign(vi.fn(), {
    success: vi.fn(),
    dismiss: vi.fn(),
  }),
}));

// Mock @simplewebauthn/browser
vi.mock('@simplewebauthn/browser', () => ({
  browserSupportsWebAuthn: vi.fn().mockReturnValue(false),
  startAuthentication: vi.fn(),
}));

function renderLogin() {
  return render(
    <MemoryRouter>
      <Login />
    </MemoryRouter>
  );
}

describe('Login Page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockStoreState = {
      login: mockLogin,
      isLoading: false,
      error: null,
      clearError: mockClearError,
    };
  });

  it('renders the login page with Sentinel branding', () => {
    renderLogin();

    expect(screen.getByText('Sentinel')).toBeInTheDocument();
    expect(screen.getByText('Remote Monitoring & Management')).toBeInTheDocument();
  });

  it('renders the method selection screen with Password option', () => {
    renderLogin();

    expect(screen.getByText('Choose sign-in method')).toBeInTheDocument();
    expect(screen.getByText('Password')).toBeInTheDocument();
    expect(screen.getByText('Sign in with username and password')).toBeInTheDocument();
  });

  it('navigates to password form when Password option is clicked', async () => {
    renderLogin();

    const passwordButton = screen.getByText('Password').closest('button')!;
    await userEvent.click(passwordButton);

    expect(screen.getByText('Sign in with password')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('username or email')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Enter your password')).toBeInTheDocument();
  });

  it('submits form with identifier and password', async () => {
    mockLogin.mockResolvedValue(undefined);
    renderLogin();

    // Click Password method
    await userEvent.click(screen.getByText('Password').closest('button')!);

    // Fill in the form
    const identifierInput = screen.getByPlaceholderText('username or email');
    const passwordInput = screen.getByPlaceholderText('Enter your password');

    await userEvent.type(identifierInput, 'admin');
    await userEvent.type(passwordInput, 'secretpass');

    // Submit
    const submitButton = screen.getByRole('button', { name: /sign in/i });
    await userEvent.click(submitButton);

    expect(mockLogin).toHaveBeenCalledWith('admin', 'secretpass');
  });

  it('displays error message when login fails', async () => {
    mockStoreState.error = 'Invalid credentials';
    renderLogin();

    // Navigate to password form to see the error
    await userEvent.click(screen.getByText('Password').closest('button')!);

    expect(screen.getByText('Invalid credentials')).toBeInTheDocument();
  });

  it('shows loading state during login', async () => {
    mockStoreState.isLoading = true;
    renderLogin();

    // Navigate to password form
    await userEvent.click(screen.getByText('Password').closest('button')!);

    expect(screen.getByText('Signing in...')).toBeInTheDocument();
    const submitButton = screen.getByRole('button', { name: /signing in/i });
    expect(submitButton).toBeDisabled();
  });

  it('navigates to home on successful login', async () => {
    mockLogin.mockResolvedValue(undefined);
    renderLogin();

    await userEvent.click(screen.getByText('Password').closest('button')!);

    await userEvent.type(screen.getByPlaceholderText('username or email'), 'admin');
    await userEvent.type(screen.getByPlaceholderText('Enter your password'), 'pass');
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/');
    });
  });

  it('clears error when switching back to method selection', async () => {
    mockStoreState.error = 'Some error';
    renderLogin();

    // Go to password form
    await userEvent.click(screen.getByText('Password').closest('button')!);

    // Click back button (ArrowLeft)
    const backButton = screen.getByText('Sign in with password').parentElement!.querySelector('button')!;
    await userEvent.click(backButton);

    expect(mockClearError).toHaveBeenCalled();
  });

  it('shows invitation link for new users', () => {
    renderLogin();

    expect(screen.getByText('Have an invitation?')).toBeInTheDocument();
    expect(screen.getByText('Create an account')).toBeInTheDocument();
  });
});
