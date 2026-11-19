import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyTheme, loadTheme, saveTheme } from './theme';

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
});

describe('theme preference', () => {
  it('defaults to system', () => {
    expect(loadTheme()).toBe('system');
  });

  it('ignores unknown stored values', () => {
    window.localStorage.setItem('simple-cd.theme', 'neon');
    expect(loadTheme()).toBe('system');
  });

  it('persists an explicit choice and applies it to <html>', () => {
    saveTheme('dark');
    expect(loadTheme()).toBe('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
    applyTheme('system');
    expect(document.documentElement.dataset.theme).toBe('system');
  });

  it('survives localStorage throwing', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    expect(loadTheme()).toBe('system');
    expect(() => saveTheme('light')).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe('light');
  });
});
