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
    try {
      tg.ready();
      tg.expand();
      tg.setHeaderColor('#f2f0ea');
      tg.setBackgroundColor('#f2f0ea');
      tg.disableVerticalSwipes?.();
    } catch {
      // older clients may not support every call
    }
  }, []);
  return info;
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
