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

/** The page background of each theme (mirrors --bg in global.css). */
export const THEME_BG: Record<Theme, string> = { light: '#e4e0d6', dark: '#07070b' };

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // ignore
  }
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', THEME_BG[theme]);
  // Telegram's header, the area behind the keyboard and the bottom bar take the same paper
  const tg = window.Telegram?.WebApp;
  if (!tg) return;
  for (const fn of [() => tg.setHeaderColor(THEME_BG[theme]), () => tg.setBackgroundColor(THEME_BG[theme]), () => tg.setBottomBarColor?.(THEME_BG[theme])]) {
    try {
      fn();
    } catch {
      // unsupported on this client version
    }
  }
}

export function isDark(): boolean {
  return document.documentElement.dataset.theme === 'dark';
}
