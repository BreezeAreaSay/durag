// Player progression, all of it pure arithmetic over one number: lifetime
// wins (first out of a finished game, counted by the server).
//
// Frames: tier k = min(20, floor(wins / 5)), one colour per tier on a single
// hue ramp, animated effects from tier 10 (50 wins) that intensify to 100.
// Skins: a table of card backs, each unlocked at a threshold.

export const TIER_STEP = 5;
export const TIER_MAX = 20; // 100 wins
export const EFFECT_TIER = 10; // 50 wins: frames start to glow and spin

export interface Skin {
  id: 'classic' | 'dots';
  unlockAt: number;
}

export const SKINS: readonly Skin[] = [
  { id: 'classic', unlockAt: 0 },
  { id: 'dots', unlockAt: 10 },
] as const;

export type SkinId = Skin['id'];

export function tierOf(wins: number): number {
  const w = Math.max(0, Math.floor(wins || 0));
  return Math.min(TIER_MAX, Math.floor(w / TIER_STEP));
}

/** Blue at tier 1, through magenta and red to gold and green at tier 20. */
export function frameHue(tier: number): number {
  return (220 + 14 * tier) % 360;
}

/** CSS custom properties for a sticker; empty below the first tier. */
export function frameStyle(wins: number): Record<string, string> {
  const tier = tierOf(wins);
  if (tier === 0) return {};
  const fx = Math.max(0, tier - EFFECT_TIER);
  return {
    '--tier': String(tier),
    '--frame': `hsl(${frameHue(tier)} 90% 55%)`,
    '--fx': String(fx), // 0 (no effects) .. 10 (maximum)
    '--spin': `${Math.max(1.2, 6 - 0.4 * fx)}s`,
    '--glow': `${2 * fx}px`,
  };
}

export function frameClass(wins: number): string {
  const tier = tierOf(wins);
  if (tier === 0) return '';
  return tier >= EFFECT_TIER ? 'sticker--framed sticker--fx' : 'sticker--framed';
}

/** Wins until the next tier (0 at the cap). */
export function toNextTier(wins: number): number {
  const tier = tierOf(wins);
  if (tier >= TIER_MAX) return 0;
  return (tier + 1) * TIER_STEP - Math.max(0, Math.floor(wins || 0));
}

export function isUnlocked(skin: Skin, wins: number): boolean {
  return wins >= skin.unlockAt;
}

export function nextSkin(wins: number): Skin | null {
  return SKINS.find((s) => !isUnlocked(s, wins)) ?? null;
}

/** Progress to the next skin in [0, 1]; 1 when everything is unlocked. */
export function skinProgress(wins: number): number {
  const next = nextSkin(wins);
  if (!next) return 1;
  return Math.min(1, Math.max(0, wins) / next.unlockAt);
}

// --- persistence: the server counts, the client remembers -----------------

const WINS_KEY = 'durag.wins';
const SKIN_KEY = 'durag.skin';

export function loadWins(): number {
  try {
    const n = Number(localStorage.getItem(WINS_KEY));
    return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
  } catch {
    return 0;
  }
}

export function saveWins(wins: number): void {
  try {
    localStorage.setItem(WINS_KEY, String(Math.max(0, Math.floor(wins))));
  } catch {
    // private mode: nothing to do
  }
}

export function loadSkin(wins: number): SkinId {
  let id: string | null = null;
  try {
    id = localStorage.getItem(SKIN_KEY);
  } catch {
    // ignore
  }
  const skin = SKINS.find((s) => s.id === id);
  return skin && isUnlocked(skin, wins) ? skin.id : 'classic';
}

export function saveSkin(id: SkinId): void {
  try {
    localStorage.setItem(SKIN_KEY, id);
  } catch {
    // ignore
  }
}
