// Reactions the server accepts (mirror of backend/internal/ws AllowedEmoji / PremiumEmoji).
export const ALLOWED_EMOJI = ['👍', '👎', '😂', '😡', '😎', '🤔', '🔥', '💀', '🤡', '❤️', '👏', '🙏'] as const;

export type Effect = 'cry' | 'finger' | 'fanfare' | 'laugh' | 'boom';

const EFFECTS: readonly string[] = ['cry', 'finger', 'fanfare', 'laugh', 'boom'];

export function isEffect(v: unknown): v is Effect {
  return typeof v === 'string' && EFFECTS.includes(v);
}

/** Reactions with an effect and a sound; experience (XP) unlocks them. */
export const PREMIUM_EMOJI: { emoji: string; xp: number; effect: Effect }[] = [
  { emoji: '😭', xp: 3, effect: 'cry' },
  { emoji: '🖕', xp: 6, effect: 'finger' },
  { emoji: '🏆', xp: 9, effect: 'fanfare' },
  { emoji: '🤣', xp: 12, effect: 'laugh' },
  { emoji: '💣', xp: 15, effect: 'boom' },
];

/** The next XP threshold above the current XP, or null when everything is open. */
export function nextUnlock(xp: number): number | null {
  const next = PREMIUM_EMOJI.map((p) => p.xp).find((n) => n > xp);
  return next ?? null;
}
