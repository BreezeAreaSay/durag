import type { Card, GameState, Outcome, Suit } from '../types/protocol';
import { roleOf, tablePairs, type Role, type TablePair } from '../util/rules';

/** Everything the PixiJS scene needs, derived from the sanitized state. */
export interface SceneModel {
  hand: Card[];
  table: TablePair[];
  deckCount: number;
  trumpCard: Card | null;
  trumpRevealed: boolean;
  trumpDrawnBy: string;
  trumpSuit: Suit | '';
  discardCount: number;
  role: Role;
  defenderTaking: boolean;
  interactive: boolean;
  lastEvent: string;
  /** The finished bout is still shown on the table (server-side pause). */
  resolving: boolean;
  resolveOutcome: Outcome;
  /** Outcome of the most recent finished bout, from the log. */
  lastOutcome: Outcome;
  version: number;
}

function lastOutcomeOf(state: GameState): Outcome {
  for (let i = state.log.length - 1; i >= 0 && i >= state.log.length - 8; i--) {
    const type = state.log[i]?.type;
    if (type === 'bito' || type === 'took') return type;
  }
  return '';
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
    trumpDrawnBy: state.trump_drawn_by,
    trumpSuit: state.trump_suit,
    discardCount: state.discard_count,
    role,
    defenderTaking: state.defender_taking,
    interactive: state.status === 'playing' && role !== 'spectator' && state.phase !== 'resolving',
    lastEvent: state.log[state.log.length - 1]?.type ?? '',
    resolving: state.phase === 'resolving',
    resolveOutcome: state.resolve_outcome,
    lastOutcome: lastOutcomeOf(state),
    version: state.version,
  };
}
