import type { Card, GameState, Outcome, Suit } from '../types/protocol';
import { canThrowIn, canTransferWith, firstBeatableTarget, roleOf, tablePairs, type Role, type TablePair } from '../util/rules';
import type { SkinId } from '../util/progress';

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
  /** the player of the last log entry */
  lastEventBy: string;
  /** the game is over: the room flares warm once */
  finished: boolean;
  /** The finished bout is still shown on the table (server-side pause). */
  resolving: boolean;
  resolveOutcome: Outcome;
  /** Outcome of the most recent finished bout, from the log. */
  lastOutcome: Outcome;
  /** viewer's own stump size (cards are hidden, only the count is known) */
  stumpCount: number;
  /** the game waits for stumps to be taken (separate step between bouts) */
  stumpStep: boolean;
  /** the viewer is one of the players who must take their stump now */
  mustTakeStump: boolean;
  version: number;
  /** the viewer's chosen card back (piles, stump, hidden trump) */
  skin: SkinId;
  /** the table's wear: games started here and flies slapped here (the cloth gets dirty) */
  roomId: string;
  gamesPlayed: number;
  fliesDead: number;
  /** stains somebody already rubbed off (indices into the drawn set) */
  wiped: number[];
  /**
   * Ids of hand cards the viewer could play right now by the client mirror
   * of the rules: throw-ins of a rank on the table, defences that beat an
   * open attack, transfers. A hint only; the server has the last word.
   */
  playable: Set<string>;
}

/** The client mirror's opinion of which hand cards may be played now. */
export function playableCards(state: GameState, role: Role, hand: Card[]): Set<string> {
  const ids = new Set<string>();
  if (state.status !== 'playing' || state.phase === 'resolving' || state.stump_pending.length > 0) return ids;
  if (role === 'attacker' && state.table_order.length === 0) {
    for (const c of hand) ids.add(c.id); // the lead attacker opens with anything
    return ids;
  }
  if (role === 'attacker' || role === 'thrower') {
    for (const c of hand) if (canThrowIn(state, c)) ids.add(c.id);
  } else if (role === 'defender' && !state.defender_taking) {
    for (const c of hand) if (firstBeatableTarget(state, c) || canTransferWith(state, c)) ids.add(c.id);
  }
  return ids;
}

function lastOutcomeOf(state: GameState): Outcome {
  for (let i = state.log.length - 1; i >= 0 && i >= state.log.length - 8; i--) {
    const type = state.log[i]?.type;
    if (type === 'bito' || type === 'took') return type;
  }
  return '';
}

/** how the viewer likes the hand laid out */
export type HandSort = 'rank' | 'suit';

const SUIT_ORDER: Record<string, number> = { Hearts: 0, Diamonds: 1, Clubs: 2, Spades: 3, None: 4 };

/** Specials go last either way; the rest by rank then suit, or by suit then rank. */
export function sortHand(cards: Card[], mode: HandSort): Card[] {
  const key = (c: Card): [number, number, number] =>
    c.rank >= 15 ? [1, c.rank, 0] : mode === 'suit' ? [0, SUIT_ORDER[c.suit] ?? 4, c.rank] : [0, c.rank, SUIT_ORDER[c.suit] ?? 4];
  return [...cards].sort((a, b) => {
    const ka = key(a);
    const kb = key(b);
    return ka[0] - kb[0] || ka[1] - kb[1] || ka[2] - kb[2];
  });
}

export function toSceneModel(state: GameState, selfId: string, skin: SkinId = 'classic', sort: HandSort = 'rank'): SceneModel {
  const me = state.players.find((p) => p.id === selfId);
  const role = roleOf(state, selfId);
  const hand = sortHand(me?.hand ?? [], sort);
  return {
    hand,
    playable: playableCards(state, role, hand),
    table: tablePairs(state),
    deckCount: state.deck_count,
    finished: state.status === 'finished',
    trumpCard: state.trump_card,
    trumpRevealed: state.trump_revealed,
    trumpDrawnBy: state.trump_drawn_by,
    trumpSuit: state.trump_suit,
    roomId: state.room_id,
    gamesPlayed: state.games_played ?? 0,
    wiped: state.wiped_stains ?? [],
    fliesDead: state.flies_dead ?? 0,
    discardCount: state.discard_count,
    role,
    defenderTaking: state.defender_taking,
    interactive: state.status === 'playing' && role !== 'spectator' && state.phase !== 'resolving' && state.stump_pending.length === 0,
    lastEvent: state.log[state.log.length - 1]?.type ?? '',
    lastEventBy: state.log[state.log.length - 1]?.player_id ?? '',
    resolving: state.phase === 'resolving',
    resolveOutcome: state.resolve_outcome,
    lastOutcome: lastOutcomeOf(state),
    stumpCount: me?.stump_count ?? 0,
    stumpStep: state.stump_pending.length > 0,
    mustTakeStump: state.stump_pending.includes(selfId),
    version: state.version,
    skin,
  };
}
