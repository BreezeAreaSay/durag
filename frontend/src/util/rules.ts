// Client-side mirror of the server rules. Used ONLY for hints (highlighting
// drop targets, choosing a sensible tap action). The server is authoritative.
import type { Card, GameState, Suit } from '../types/protocol';
import { cardFromId, isBlack, isJoker, isRed, isSuper } from './cards';

export function canBeat(attack: Card, defense: Card, trumpSuit: Suit | ''): boolean {
  if (isSuper(defense)) return true;
  if (isSuper(attack)) return false;
  if (isJoker(defense)) {
    if (isJoker(attack)) return false;
    return defense.id === 'RJ' ? isRed(attack) : isBlack(attack);
  }
  if (isJoker(attack)) return false;
  if (attack.suit === defense.suit) return defense.rank > attack.rank;
  return trumpSuit !== '' && defense.suit === trumpSuit;
}

export interface TablePair {
  attack: Card;
  defense?: Card;
}

export function tablePairs(state: GameState): TablePair[] {
  return state.table_order.map((id) => ({ attack: cardFromId(id), defense: state.table_cards[id]?.[0] }));
}

export function tableRanks(state: GameState): Set<number> {
  const ranks = new Set<number>();
  for (const pair of tablePairs(state)) {
    ranks.add(pair.attack.rank);
    if (pair.defense) ranks.add(pair.defense.rank);
  }
  return ranks;
}

export function activeTrump(state: GameState): Suit | '' {
  return state.trump_revealed ? state.trump_suit : '';
}

export type Role = 'attacker' | 'defender' | 'thrower' | 'spectator';

export function roleOf(state: GameState, playerId: string): Role {
  if (state.status !== 'playing') return 'spectator';
  const me = state.players.find((p) => p.id === playerId);
  if (!me || me.out) return 'spectator';
  if (playerId === state.defender_id) return 'defender';
  if (playerId === state.current_turn_player_id) return 'attacker';
  return 'thrower';
}

/** Whether the viewer could legally throw this card in (hint only). */
export function canThrowIn(state: GameState, card: Card): boolean {
  if (state.table_order.length === 0) return false;
  const defender = state.players.find((p) => p.id === state.defender_id);
  if (!state.defender_taking && (!defender || defender.hand_count === 0)) return false;
  return tableRanks(state).has(card.rank);
}

/** Whether the viewer (defender) could transfer with this card (hint only). */
export function canTransferWith(state: GameState, card: Card): boolean {
  if (state.table_order.length === 0 || state.defender_taking || isSuper(card)) return false;
  const pairs = tablePairs(state);
  if (pairs.some((p) => p.defense)) return false;
  return pairs.every((p) => p.attack.rank === card.rank);
}

/** First undefended attack card the given card can beat, if any (hint only). */
export function firstBeatableTarget(state: GameState, card: Card): Card | undefined {
  const trump = activeTrump(state);
  return tablePairs(state).find((p) => !p.defense && canBeat(p.attack, card, trump))?.attack;
}
