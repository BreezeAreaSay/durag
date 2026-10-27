import { describe, expect, it } from 'vitest';
import { EFFECT_TIER, SKINS, TIER_MAX, frameClass, frameHue, frameStyle, isUnlocked, nextSkin, skinProgress, tierOf, toNextTier } from './progress';

describe('frames by wins', () => {
  it('steps every 5 wins and caps at 100', () => {
    expect(tierOf(0)).toBe(0);
    expect(tierOf(4)).toBe(0);
    expect(tierOf(5)).toBe(1);
    expect(tierOf(9)).toBe(1);
    expect(tierOf(10)).toBe(2);
    expect(tierOf(50)).toBe(EFFECT_TIER);
    expect(tierOf(100)).toBe(TIER_MAX);
    expect(tierOf(1000)).toBe(TIER_MAX);
    expect(tierOf(-3)).toBe(0);
    expect(tierOf(NaN)).toBe(0);
  });
  it('gives every tier its own hue', () => {
    const hues = new Set<number>();
    for (let k = 1; k <= TIER_MAX; k++) hues.add(frameHue(k));
    expect(hues.size).toBe(TIER_MAX);
  });
  it('switches effects on at 50 wins and intensifies them', () => {
    expect(frameStyle(3)).toEqual({});
    expect(frameClass(3)).toBe('');
    expect(frameClass(5)).toBe('sticker--framed');
    expect(frameStyle(49)['--fx']).toBe('0');
    expect(frameClass(50)).toBe('sticker--framed sticker--fx');
    expect(frameStyle(50)['--spin']).toBe('6s');
    expect(frameStyle(100)['--spin']).toBe('2s');
    expect(frameStyle(100)['--glow']).toBe('20px');
  });
  it('counts wins to the next tier', () => {
    expect(toNextTier(0)).toBe(5);
    expect(toNextTier(7)).toBe(3);
    expect(toNextTier(100)).toBe(0);
  });
});

describe('skins', () => {
  it('unlocks the dotted back at 10 wins', () => {
    const dots = SKINS.find((s) => s.id === 'dots')!;
    expect(isUnlocked(dots, 9)).toBe(false);
    expect(isUnlocked(dots, 10)).toBe(true);
    expect(nextSkin(0)?.id).toBe('dots');
    expect(nextSkin(10)?.id).toBe('checks');
    expect(nextSkin(50)).toBeNull();
    expect(skinProgress(5)).toBe(0.5);
    expect(skinProgress(60)).toBe(1);
  });
  it('opens a back every ten wins up to the acid one', () => {
    expect(SKINS.map((s) => s.unlockAt)).toEqual([0, 10, 20, 30, 40, 50]);
    expect(SKINS.map((s) => s.id)).toEqual(['classic', 'dots', 'checks', 'stars', 'waves', 'acid']);
  });
});
