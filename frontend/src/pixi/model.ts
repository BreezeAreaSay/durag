import type { Card, GameState, Suit } from '../types/protocol';
import { roleOf, tablePairs, type Role, type TablePair } from '../util/rules';

/** Everything the PixiJS scene needs, derived from the sanitized state. */
export interface SceneModel {
  hand: Card[];
  table: TablePair[];
  deckCount: number;
  trumpCard: Card | null;
  trumpRevealed: boolean;
  trumpSuit: Suit | '';
  discardCount: number;
  role: Role;
  defenderTaking: boolean;
  interactive: boolean;
  lastEvent: string;
  version: number;
}

export function toSceneModel(state: GameState, selfId: string): SceneModel {
  const me = state.players.find((p) => p.id === selfId);
  const role = roleOf(state, selfId);
  return {
    hand: me?.hand ?? [],
    table: tablePairs(state),
    deckCount: state.deck_count,
    trumpCard: state.trump_card,
    trumpRevealed: state.trump_revealed,
    trumpSuit: state.trump_suit,
    discardCount: state.discard_count,
    role,
    defenderTaking: state.defender_taking,
    interactive: state.status === 'playing' && role !== 'spectator',
    lastEvent: state.log[state.log.length - 1]?.type ?? '',
    version: state.version,
  };
}
