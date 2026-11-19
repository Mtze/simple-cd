export type ThemePref = 'system' | 'light' | 'dark';

const KEY = 'simple-cd.theme';

export function loadTheme(): ThemePref {
  try {
    const v = window.localStorage.getItem(KEY);
    if (v === 'light' || v === 'dark' || v === 'system') return v;
  } catch {
    // storage blocked: fall back to the OS setting
  }
  return 'system';
}

export function applyTheme(pref: ThemePref) {
  document.documentElement.dataset.theme = pref;
}

export function saveTheme(pref: ThemePref) {
  try {
    window.localStorage.setItem(KEY, pref);
  } catch {
    // storage blocked: the choice lasts until reload
  }
  applyTheme(pref);
}
