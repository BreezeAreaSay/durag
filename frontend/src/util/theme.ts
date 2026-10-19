// Light paper by default, deep dark on request. The choice lives on <html>
// as data-theme so CSS tokens switch, and the PixiJS table reads it when it
// builds (the table remounts on a change).

export type Theme = 'light' | 'dark';

const KEY = 'durag.theme';

export function loadTheme(): Theme {
  try {
    return localStorage.getItem(KEY) === 'dark' ? 'dark' : 'light';
  } catch {
    return 'light';
  }
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // ignore
  }
}

export function isDark(): boolean {
  return document.documentElement.dataset.theme === 'dark';
}
