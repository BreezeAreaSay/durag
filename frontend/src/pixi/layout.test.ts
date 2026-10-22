import { describe, expect, it } from 'vitest';
import { computeMetrics, deckSlot, discardSlot, handSlots, hitSlot, nextAttackSlot, tableSlots, trumpSlot, jitterSlot, HAND_INSET } from './layout';

const phone = computeMetrics(390, 844);
const tablet = computeMetrics(1024, 768);

describe('metrics', () => {
  it('scales the card with the viewport and keeps zones ordered', () => {
    for (const m of [phone, tablet, computeMetrics(320, 480)]) {
      expect(m.cardW).toBeGreaterThanOrEqual(44);
      expect(m.cardW).toBeLessThanOrEqual(112);
      expect(m.cardH).toBe(Math.round(m.cardW * 1.45));
      expect(m.tableTop).toBeLessThan(m.tableBottom);
      expect(m.tableBottom).toBeLessThan(m.handTop);
      expect(m.handTop + m.cardH).toBeLessThanOrEqual(m.height);
    }
  });
});

describe('top inset', () => {
  it('pushes the table below a tall HTML panel', () => {
    const tall = computeMetrics(390, 844, 240);
    expect(tall.tableTop).toBeGreaterThanOrEqual(246);
    expect(tall.tableTop).toBeLessThan(tall.tableBottom);
    expect(computeMetrics(390, 844, 0).topZone).toBe(169);
  });
});

describe('hand', () => {
  it('centres the fan and never leaves the screen', () => {
    for (const n of [1, 2, 6, 12, 20]) {
      const slots = handSlots(n, phone);
      expect(slots).toHaveLength(n);
      const xs = slots.map((s) => s.x);
      const mid = (Math.min(...xs) + Math.max(...xs)) / 2;
      // centred in the room right of the player's sticker
      expect(Math.abs(mid - (HAND_INSET + (phone.width - HAND_INSET) / 2))).toBeLessThan(1);
      expect(Math.min(...xs) - phone.cardW / 2).toBeGreaterThanOrEqual(HAND_INSET);
      expect(Math.max(...xs) + phone.cardW / 2).toBeLessThanOrEqual(phone.width);
    }
    expect(handSlots(0, phone)).toEqual([]);
  });
  it('fans symmetrically', () => {
    const [a, , c] = handSlots(3, phone);
    expect(a!.rotation).toBeCloseTo(-c!.rotation);
  });
});

describe('table', () => {
  it('keeps pairs inside the table zone and shrinks crowded tables', () => {
    for (const n of [1, 3, 6, 12, 20]) {
      const slots = tableSlots(n, phone);
      expect(slots).toHaveLength(n);
      for (const { attack, defense } of slots) {
        const hw = (phone.cardW * attack.scale) / 2;
        expect(attack.x - hw).toBeGreaterThanOrEqual(0);
        expect(defense.x + hw).toBeLessThanOrEqual(phone.width + 1);
        expect(attack.y - (phone.cardH * attack.scale) / 2).toBeGreaterThanOrEqual(phone.tableTop - 1);
        expect(defense.y + (phone.cardH * attack.scale) / 2).toBeLessThanOrEqual(phone.handTop);
        expect(defense.x).toBeGreaterThan(attack.x);
        expect(defense.y).toBeGreaterThan(attack.y);
      }
    }
    expect(tableSlots(20, phone)[0]!.attack.scale).toBeLessThan(tableSlots(1, phone)[0]!.attack.scale);
  });
  it('next attack slot matches the layout with one more card', () => {
    const next = nextAttackSlot(2, phone);
    expect(next).toEqual(tableSlots(3, phone)[2]!.attack);
  });
  it('hit testing', () => {
    const slot = tableSlots(1, phone)[0]!.attack;
    expect(hitSlot(slot, phone, slot.x, slot.y)).toBe(true);
    expect(hitSlot(slot, phone, slot.x + phone.cardW, slot.y)).toBe(false);
  });
});

describe('piles', () => {
  it('deck, trump and discard do not overlap the hand', () => {
    for (const s of [deckSlot(phone), trumpSlot(phone), discardSlot(phone)]) {
      expect(s.y).toBeLessThan(phone.tableBottom);
      expect(s.x).toBeGreaterThan(0);
      expect(s.x).toBeLessThan(phone.width);
    }
    expect(trumpSlot(phone).x).toBeGreaterThan(deckSlot(phone).x);
  });
});

describe('jitterSlot', () => {
  const base = { x: 100, y: 100, rotation: 0, scale: 1 };
  it('is deterministic per card id', () => {
    expect(jitterSlot(base, 'H_7')).toEqual(jitterSlot(base, 'H_7'));
    expect(jitterSlot(base, 'H_7')).not.toEqual(jitterSlot(base, 'S_7'));
  });
  it('stays within the careless bounds and scales with amount', () => {
    const ids = ['H_2', 'H_7', 'S_14', 'RJ', 'BJ', 'SC', 'C_10', 'D_12', 'S_3', 'C_4'];
    for (const id of ids) {
      const j = jitterSlot(base, id);
      expect(Math.abs(j.rotation)).toBeLessThanOrEqual(0.35);
      expect(Math.abs(j.x - 100)).toBeLessThanOrEqual(4);
      expect(Math.abs(j.y - 100)).toBeLessThanOrEqual(4);
      const small = jitterSlot(base, id, 0.3);
      expect(Math.abs(small.rotation)).toBeCloseTo(Math.abs(j.rotation) * 0.3, 6);
    }
    expect(jitterSlot(base, 'H_7', 0)).toEqual(base);
  });
});
