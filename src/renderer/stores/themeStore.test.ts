import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useThemeStore, initializeTheme } from './themeStore';

describe('themeStore', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove('dark');

    // Reset the store to defaults
    useThemeStore.setState({ theme: 'dark' });
  });

  it('defaults to dark theme', () => {
    const state = useThemeStore.getState();
    expect(state.theme).toBe('dark');
  });

  it('setTheme changes the current theme to light', () => {
    useThemeStore.getState().setTheme('light');

    const state = useThemeStore.getState();
    expect(state.theme).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('setTheme changes the current theme to dark and adds class', () => {
    // Start from light
    useThemeStore.getState().setTheme('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);

    // Switch to dark
    useThemeStore.getState().setTheme('dark');
    expect(useThemeStore.getState().theme).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('setTheme handles system theme correctly', () => {
    // matchMedia is mocked in setup.ts to return matches: false (light preference)
    useThemeStore.getState().setTheme('system');

    const state = useThemeStore.getState();
    expect(state.theme).toBe('system');
    // Since matchMedia.matches is false (prefers light), dark class should be removed
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('getEffectiveTheme returns correct theme for system preference', () => {
    useThemeStore.getState().setTheme('system');

    // matchMedia is mocked to return matches: false => light
    const effective = useThemeStore.getState().getEffectiveTheme();
    expect(effective).toBe('light');
  });

  it('getEffectiveTheme returns dark when theme is dark', () => {
    useThemeStore.getState().setTheme('dark');
    expect(useThemeStore.getState().getEffectiveTheme()).toBe('dark');
  });

  it('getEffectiveTheme returns light when theme is light', () => {
    useThemeStore.getState().setTheme('light');
    expect(useThemeStore.getState().getEffectiveTheme()).toBe('light');
  });

  it('persists theme to localStorage via zustand persist middleware', () => {
    useThemeStore.getState().setTheme('light');

    const stored = localStorage.getItem('sentinel-theme');
    expect(stored).toBeTruthy();
    const parsed = JSON.parse(stored!);
    expect(parsed.state.theme).toBe('light');
  });

  it('initializeTheme restores theme from localStorage', () => {
    // Set a theme in localStorage matching the persist format
    localStorage.setItem('sentinel-theme', JSON.stringify({
      state: { theme: 'dark' },
      version: 0,
    }));

    initializeTheme();

    // After initializeTheme, the dark class should be applied
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('initializeTheme handles invalid JSON in localStorage gracefully', () => {
    localStorage.setItem('sentinel-theme', 'not-valid-json');

    // Should not throw
    expect(() => initializeTheme()).not.toThrow();
  });

  it('initializeTheme registers listener for system theme changes', () => {
    const addEventListenerSpy = vi.fn();
    vi.mocked(window.matchMedia).mockReturnValue({
      matches: false,
      media: '(prefers-color-scheme: dark)',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: addEventListenerSpy,
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    } as unknown as MediaQueryList);

    initializeTheme();

    expect(addEventListenerSpy).toHaveBeenCalledWith('change', expect.any(Function));
  });
});
