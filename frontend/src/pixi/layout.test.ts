import { describe, expect, it } from 'vitest';
import { computeMetrics, deckSlot, discardSlot, handSlots, hitSlot, nextAttackSlot, tableSlots, trumpSlot } from './layout';

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

describe('hand', () => {
  it('centres the fan and never leaves the screen', () => {
    for (const n of [1, 2, 6, 12, 20]) {
      const slots = handSlots(n, phone);
      expect(slots).toHaveLength(n);
      const xs = slots.map((s) => s.x);
      const mid = (Math.min(...xs) + Math.max(...xs)) / 2;
      expect(Math.abs(mid - phone.width / 2)).toBeLessThan(1);
      expect(Math.min(...xs) - phone.cardW / 2).toBeGreaterThanOrEqual(0);
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
