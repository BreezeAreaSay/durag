import { useEffect, useMemo } from 'react';

export interface TelegramInfo {
  isTelegram: boolean;
  initData: string;
  user?: TelegramWebAppUser;
  startParam?: string;
  languageCode?: string;
  colorScheme: 'light' | 'dark';
  platform: string;
}

function readTelegram(): TelegramInfo {
  const tg = typeof window !== 'undefined' ? window.Telegram?.WebApp : undefined;
  const initData = tg?.initData ?? '';
  return {
    isTelegram: Boolean(tg && initData),
    initData,
    user: tg?.initDataUnsafe?.user,
    startParam: tg?.initDataUnsafe?.start_param,
    languageCode: tg?.initDataUnsafe?.user?.language_code,
    colorScheme: tg?.colorScheme ?? 'light',
    platform: tg?.platform ?? 'web',
  };
}

/** Reads initData once and prepares the Mini App chrome. */
export function useTelegram(): TelegramInfo {
  const info = useMemo(readTelegram, []);
  useEffect(() => {
    const tg = window.Telegram?.WebApp;
    if (!tg) return;
    // Each call is optional on older clients, so every one gets its own try.
    const attempt = (fn: () => void) => {
      try {
        fn();
      } catch {
        // unsupported on this client version
      }
    };
    attempt(() => tg.ready());
    attempt(() => tg.expand()); // a Mini App opened from a button starts half-height
    // header, keyboard background and bottom bar colours follow the theme (util/theme.ts applyTheme)
    attempt(() => tg.disableVerticalSwipes?.()); // dragging a card must not pull the app down
    attempt(() => tg.lockOrientation?.()); // the table is laid out for portrait
  }, []);
  return info;
}

/**
 * Shows Telegram's native back button (the arrow in the Mini App header)
 * while a screen can go back, and runs `onBack` when it is tapped. Outside
 * Telegram it does nothing: the screens keep their own buttons.
 */
export function useBackButton(onBack: (() => void) | null): void {
  useEffect(() => {
    const bb = window.Telegram?.WebApp?.BackButton;
    if (!bb || !onBack) return;
    const handler = () => onBack();
    try {
      bb.onClick(handler);
      bb.show();
    } catch {
      return;
    }
    return () => {
      try {
        bb.offClick(handler);
        bb.hide();
      } catch {
        // older clients
      }
    };
  }, [onBack]);
}

export function haptic(kind: 'light' | 'medium' | 'heavy' | 'error' | 'success'): void {
  const h = window.Telegram?.WebApp?.HapticFeedback;
  if (!h) return;
  try {
    if (kind === 'error' || kind === 'success') h.notificationOccurred(kind);
    else h.impactOccurred(kind);
  } catch {
    // ignore
  }
}

export function closingConfirmation(enabled: boolean): void {
  const tg = window.Telegram?.WebApp;
  if (!tg) return;
  try {
    if (enabled) tg.enableClosingConfirmation();
    else tg.disableClosingConfirmation();
  } catch {
    // ignore
  }
}
