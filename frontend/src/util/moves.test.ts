import { beforeAll, describe, expect, it } from 'vitest';
import { describeMove, lastMove } from './moves';
import { setLang } from '../i18n';
import type { GameState } from '../types/protocol';

function state(log: GameState['log']): GameState {
  return {
    room_id: 'R',
    players: [
      { id: 'A', name: 'Alice', hand: [], stump: [], is_ready: true, connected: true, passed: false, out: false, hand_count: 6, stump_count: 2 },
      { id: 'B', name: 'Bob', hand: [], stump: [], is_ready: true, connected: true, passed: false, out: false, hand_count: 6, stump_count: 2 },
    ],
    deck: [],
    trump_card: null,
    table_cards: { H_7: [{ id: 'H_10', suit: 'Hearts', rank: 10 }], S_7: [] },
    current_turn_player_id: 'A',
    status: 'playing',
    table_order: ['H_7', 'S_7'],
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
    log,
    viewer_id: 'A',
    transfer_count: 0,
    bout_number: 1,
    phase: '',
    resolve_at: 0,
    resolve_outcome: '',
    stump_pending: [],
    stump_deadline: 0,
    turn_deadline: 0,
    turn_timeout_ms: 0,
    turn_actors: [],
  };
}

describe('describeMove', () => {
  beforeAll(() => setLang('en'));
  it('names players and cards', () => {
    const s = state([]);
    expect(describeMove(s, { type: 'defend', player_id: 'B', card_id: 'H_10', target_id: 'H_7' })).toBe('Bob: 10♥ beats 7♥');
    expect(describeMove(s, { type: 'attack', player_id: 'A', card_id: 'SC' })).toBe('Alice leads SUPER');
    expect(describeMove(s, { type: 'transfer', player_id: 'B', card_id: 'RJ' })).toBe('Bob transfers with RED JOKER');
    expect(describeMove(s, { type: 'bout_end', player_id: 'B', text: 'bito' })).toBe('Beaten! 3 cards go to the discard');
    expect(describeMove(s, { type: 'bout_end', player_id: 'B', text: 'took' })).toBe('Bob takes the whole table');
    expect(describeMove(s, { type: 'trump_revealed', text: 'Spades' })).toBe('Trump revealed: ♠');
    expect(describeMove(s, { type: 'out', player_id: 'A' })).toBe('Alice is out: no cards left');
  });
  it('agrees nouns with counts in both languages', () => {
    const s = state([]);
    expect(describeMove(s, { type: 'took', player_id: 'B', text: '1' })).toBe('Bob took 1 card');
    expect(describeMove(s, { type: 'took', player_id: 'B', text: '5' })).toBe('Bob took 5 cards');
    expect(describeMove(s, { type: 'stump_wait', text: '1' })).toBe('Stumps: waiting for 1 player');
    setLang('ru');
    try {
      expect(describeMove(s, { type: 'took', player_id: 'B', text: '1' })).toBe('Bob забрал 1 карту');
      expect(describeMove(s, { type: 'took', player_id: 'B', text: '3' })).toBe('Bob забрал 3 карты');
      expect(describeMove(s, { type: 'took', player_id: 'B', text: '12' })).toBe('Bob забрал 12 карт');
      expect(describeMove(s, { type: 'took', player_id: 'B', text: '21' })).toBe('Bob забрал 21 карту');
      expect(describeMove(s, { type: 'bito', text: '2' })).toBe('Бито: 2 карты в отбое');
      expect(describeMove(s, { type: 'stump_wait', text: '1' })).toBe('Пенёк: ждём 1 игрока');
      expect(describeMove(s, { type: 'stump_wait', text: '2' })).toBe('Пенёк: ждём 2 игроков');
    } finally {
      setLang('en');
    }
  });
  it('skips lobby noise and finds the last real move', () => {
    const s = state([
      { type: 'throw_in', player_id: 'A', card_id: 'S_7' },
      { type: 'disconnect', player_id: 'B' },
      { type: 'rejoin', player_id: 'B' },
    ]);
    expect(describeMove(s, { type: 'rejoin', player_id: 'B' })).toBeNull();
    expect(lastMove(s)).toBe('Alice throws in 7♠');
    expect(lastMove(state([]))).toBeNull();
  });
});
