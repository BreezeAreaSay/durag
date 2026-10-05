import { describe, expect, it } from 'vitest';
import { canBeat, canTransferWith, roleOf, tableRanks } from './rules';
import { cardFromId } from './cards';
import type { GameState } from '../types/protocol';

const c = cardFromId;

describe('canBeat mirrors the server rules', () => {
  it('same suit higher / lower', () => {
    expect(canBeat(c('H_7'), c('H_10'), '')).toBe(true);
    expect(canBeat(c('H_10'), c('H_7'), '')).toBe(false);
    expect(canBeat(c('H_7'), c('S_14'), '')).toBe(false);
  });
  it('trumps only when revealed', () => {
    expect(canBeat(c('H_14'), c('S_2'), 'Spades')).toBe(true);
    expect(canBeat(c('H_14'), c('S_2'), '')).toBe(false);
    expect(canBeat(c('S_2'), c('H_14'), 'Spades')).toBe(false);
  });
  it('super card beats everything and is unbeatable', () => {
    for (const id of ['H_14', 'RJ', 'BJ', 'S_2']) {
      expect(canBeat(c(id), c('SC'), 'Spades')).toBe(true);
      expect(canBeat(c('SC'), c(id), 'Spades')).toBe(false);
    }
  });
  it('jokers beat by colour, never each other, never beaten by trumps', () => {
    expect(canBeat(c('H_7'), c('RJ'), '')).toBe(true);
    expect(canBeat(c('S_7'), c('RJ'), '')).toBe(false);
    expect(canBeat(c('S_7'), c('BJ'), '')).toBe(true);
    expect(canBeat(c('D_7'), c('BJ'), '')).toBe(false);
    expect(canBeat(c('RJ'), c('BJ'), '')).toBe(false);
    expect(canBeat(c('BJ'), c('H_14'), 'Hearts')).toBe(false);
  });
});

function state(partial: Partial<GameState>): GameState {
  return {
    room_id: 'R',
    players: [
      { id: 'A', name: 'A', hand: [], stump: [], is_ready: true, connected: true, passed: false, out: false, hand_count: 6, stump_count: 2 },
      { id: 'B', name: 'B', hand: [], stump: [], is_ready: true, connected: true, passed: false, out: false, hand_count: 6, stump_count: 2 },
    ],
    deck: [],
    trump_card: null,
    table_cards: {},
    current_turn_player_id: 'A',
    status: 'playing',
    table_order: [],
    defender_id: 'B',
    defender_taking: false,
    trump_revealed: false,
    trump_drawn_by: '',
    trump_suit: '',
    deck_count: 30,
    discard_count: 0,
    host_id: 'A',
    max_players: 6,
    finished_order: [],
    version: 1,
    updated_at: 0,
    log: [],
    viewer_id: 'A',
    transfer_count: 0,
    bout_number: 1,
    phase: '',
    resolve_at: 0,
    resolve_outcome: '',
    ...partial,
  };
}

describe('table helpers', () => {
  it('roles', () => {
    const s = state({});
    expect(roleOf(s, 'A')).toBe('attacker');
    expect(roleOf(s, 'B')).toBe('defender');
    expect(roleOf(s, 'C')).toBe('spectator');
    expect(roleOf(state({ status: 'waiting' }), 'A')).toBe('spectator');
  });
  it('ranks on the table include defences', () => {
    const s = state({ table_order: ['H_7'], table_cards: { H_7: [c('H_10')] } });
    expect([...tableRanks(s)].sort()).toEqual([10, 7]);
  });
  it('transfer hints', () => {
    const open = state({ table_order: ['H_7', 'S_7'], table_cards: { H_7: [], S_7: [] } });
    expect(canTransferWith(open, c('D_7'))).toBe(true);
    expect(canTransferWith(open, c('D_8'))).toBe(false);
    expect(canTransferWith(open, c('SC'))).toBe(false);
    const defended = state({ table_order: ['H_7'], table_cards: { H_7: [c('H_9')] } });
    expect(canTransferWith(defended, c('D_7'))).toBe(false);
  });
});
