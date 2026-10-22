import { describe, expect, it } from 'vitest';
import { cardFromId } from '../util/cards';
import type { GameState } from '../types/protocol';
import { sortHand, toSceneModel } from './model';
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

describe('playable hint mirrors the throw-in and defence rules', () => {
  const cf = cardFromId;
  const base = (over: Partial<GameState>): GameState =>
    ({
      room_id: 'R',
      status: 'playing',
      phase: 'bout',
      players: [
        { id: 'A', name: 'A', hand: [cf('H_7'), cf('C_9'), cf('S_14')], stump: [], is_ready: true, connected: true, passed: false, out: false, hand_count: 3, stump_count: 2 },
        { id: 'B', name: 'B', hand: [], stump: [], is_ready: true, connected: true, passed: false, out: false, hand_count: 4, stump_count: 2 },
      ],
      viewer_id: 'A',
      current_turn_player_id: 'A',
      defender_id: 'B',
      table_order: ['D_9'],
      table_cards: { D_9: [cf('D_10')] },
      defender_taking: false,
      stump_pending: [],
      trump_card: null,
      trump_revealed: false,
      trump_suit: '',
      trump_drawn_by: '',
      deck_count: 10,
      discard_count: 0,
      log: [],
      resolve_outcome: '',
      max_attacks: 0,
      version: 1,
      ...over,
    }) as unknown as GameState;

  it('an attacker may throw in the ranks on the table, beaten cards included', () => {
    const m = toSceneModel(base({}), 'A');
    expect([...m.playable].sort()).toEqual(['C_9']); // 9 and 10 are on the table; the hand has a 9
    const withTen = base({ players: [{ ...base({}).players[0]!, hand: [cf('H_10'), cf('H_7')] }, base({}).players[1]!] });
    expect([...toSceneModel(withTen, 'A').playable]).toEqual(['H_10']);
  });

  it('an empty table lets the lead attacker play anything, and nothing during the pause', () => {
    const open = base({ table_order: [], table_cards: {} });
    expect(toSceneModel(open, 'A').playable.size).toBe(3);
    expect(toSceneModel(base({ phase: 'resolving' }), 'A').playable.size).toBe(0);
  });

  it('a defender sees what beats an open attack or transfers', () => {
    const defending = base({ defender_id: 'A', current_turn_player_id: 'B', table_order: ['D_9'], table_cards: {}, players: [{ ...base({}).players[0]!, hand: [cf('D_10'), cf('C_9'), cf('H_2')] }, base({}).players[1]!] });
    expect([...toSceneModel(defending, 'A').playable].sort()).toEqual(['C_9', 'D_10']); // D_10 beats, C_9 transfers
  });
});
