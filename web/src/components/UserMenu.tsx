import { useEffect, useId, useRef, useState } from 'react';
import { api, browser, errorMessage } from '../api/client';
import type { User } from '../api/types';
import { loadTheme, saveTheme, type ThemePref } from '../lib/theme';

export function initials(name: string, fallback: string): string {
  const parts = (name || fallback).trim().split(/\s+/).filter(Boolean);
  const s = parts.length > 1 ? parts[0][0] + parts[parts.length - 1][0] : (parts[0] ?? '?').slice(0, 2);
  return s.toUpperCase();
}

const THEMES: { id: ThemePref; label: string }[] = [
  { id: 'system', label: 'System' },
  { id: 'light', label: 'Light' },
  { id: 'dark', label: 'Dark' },
];

export function UserMenu({ user }: { user: User }) {
  const [open, setOpen] = useState(false);
  const [theme, setTheme] = useState<ThemePref>(loadTheme);
  const [error, setError] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const menuId = useId();

  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false);
        button.current?.focus();
      }
    };
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const logout = async () => {
    setError(null);
    try {
      const { logoutUrl } = await api.logout();
      browser.go(logoutUrl || '/');
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <div className="user-menu" ref={root}>
      <button
        ref={button}
        type="button"
        className="avatar"
        aria-label={`Account menu for ${user.name || user.username}`}
        aria-expanded={open}
        aria-controls={menuId}
        onClick={() => setOpen((o) => !o)}
      >
        {initials(user.name, user.username)}
      </button>
      {open && (
        <div className="menu" id={menuId}>
          <div className="menu-section">
            <div className="menu-name">{user.name || user.username}</div>
            <div className="muted">{user.username}</div>
            {user.email && <div className="muted wrap">{user.email}</div>}
          </div>
          <div className="menu-section">
            <div className="section-label">Roles</div>
            {user.roles.length === 0 ? (
              <div className="muted">Read only</div>
            ) : (
              <ul className="role-list">
                {user.roles.map((r) => <li key={r}><code>{r}</code></li>)}
              </ul>
            )}
          </div>
          <div className="menu-section">
            <div className="section-label" id={`${menuId}-theme`}>Theme</div>
            <div className="segmented" role="radiogroup" aria-labelledby={`${menuId}-theme`}>
              {THEMES.map((t) => (
                <label key={t.id} className="segment">
                  <input
                    type="radio"
                    name={`${menuId}-theme`}
                    value={t.id}
                    checked={theme === t.id}
                    onChange={() => {
                      setTheme(t.id);
                      saveTheme(t.id);
                    }}
                  />
                  <span>{t.label}</span>
                </label>
              ))}
            </div>
          </div>
          <div className="menu-section">
            <button type="button" className="btn btn-block" onClick={logout}>Log out</button>
            {error && <p className="error-text" role="alert">{error}</p>}
          </div>
        </div>
      )}
    </div>
  );
}
