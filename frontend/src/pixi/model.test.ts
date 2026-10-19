import { describe, expect, it } from 'vitest';
import { sortHand } from './model';
import type { Card } from '../types/protocol';

const c = (id: string, suit: Card['suit'], rank: number): Card => ({ id, suit, rank });

describe('sortHand', () => {
  const hand = [c('S_10', 'Spades', 10), c('H_7', 'Hearts', 7), c('RJ', 'None', 15), c('C_7', 'Clubs', 7), c('H_14', 'Hearts', 14)];
  it('by rank: low to high, specials last', () => {
    expect(sortHand(hand, 'rank').map((x) => x.id)).toEqual(['H_7', 'C_7', 'S_10', 'H_14', 'RJ']);
  });
  it('by suit: hearts, diamonds, clubs, spades, each low to high, specials last', () => {
    expect(sortHand(hand, 'suit').map((x) => x.id)).toEqual(['H_7', 'H_14', 'C_7', 'S_10', 'RJ']);
  });
  it('does not touch the input', () => {
    const copy = [...hand];
    sortHand(hand, 'suit');
    expect(hand).toEqual(copy);
  });
});
