// Reactions the server accepts (mirror of backend/internal/ws AllowedEmoji / PremiumEmoji).
export const ALLOWED_EMOJI = ['👍', '👎', '😂', '😡', '😎', '🤔', '🔥', '💀', '🤡', '❤️', '👏', '🙏', '👊', '🚬'] as const;
/** A fist on the table: every client's cards jump. */
export const SLAM_EMOJI = '👊';
/** A cigarette by the avatar instead of a flight to the table. */
export const SMOKE_EMOJI = '🚬';

export type Effect = 'cry' | 'finger' | 'fanfare' | 'laugh' | 'boom' | 'shake' | 'firecracker' | 'rooster' | 'crown' | 'clink';

const EFFECTS: readonly string[] = ['cry', 'finger', 'fanfare', 'laugh', 'boom', 'shake', 'firecracker', 'rooster', 'crown', 'clink'];

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

/** The second page: reactions that lifetime wins unlock. */
export const WINS_EMOJI: { emoji: string; wins: number; effect: Effect }[] = [
  { emoji: '🤝', wins: 5, effect: 'shake' },
  { emoji: '🧨', wins: 10, effect: 'firecracker' },
  { emoji: '🐓', wins: 15, effect: 'rooster' },
  { emoji: '👑', wins: 20, effect: 'crown' },
  { emoji: '🥂', wins: 30, effect: 'clink' },
];

/** Quick phrases a human can drop on the table (mirror of AllowedPhrases). */
export const PHRASES = ['easy', 'бери давай', 'ну и ход', 'не спи', 'это всё?', 'ха', 'заберёшь', 'ой'] as const;
/** Wins that open the quick phrases. */
export const PHRASE_WINS = 10;

/** The next XP threshold above the current XP, or null when everything is open. */
export function nextUnlock(xp: number): number | null {
  const next = PREMIUM_EMOJI.map((p) => p.xp).find((n) => n > xp);
  return next ?? null;
}

/** The next wins threshold on the second page, or null when it is all open. */
export function nextWinsUnlock(wins: number): number | null {
  const next = WINS_EMOJI.map((p) => p.wins).find((n) => n > wins);
  return next ?? null;
}
