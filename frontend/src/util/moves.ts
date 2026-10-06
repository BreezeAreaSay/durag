// Human readable descriptions of log entries ("who beat what with which card").
import type { GameState, LogEntry, Suit } from '../types/protocol';
import { countWord, t } from '../i18n';
import { SUIT_SYMBOL, cardFromId, cardLabel } from './cards';

function tableCardCount(state: GameState): number {
  return state.table_order.reduce((n, id) => n + 1 + (state.table_cards[id]?.length ?? 0), 0);
}

export function describeMove(state: GameState, entry: LogEntry | undefined): string | null {
  if (!entry) return null;
  const name = (id?: string) => state.players.find((p) => p.id === id)?.name ?? id ?? '?';
  const card = (id?: string) => (id ? cardLabel(cardFromId(id)) : '?');
  const who = name(entry.player_id);
  switch (entry.type) {
    case 'attack':
      return t('move.attack', { name: who, card: card(entry.card_id) });
    case 'throw_in':
      return t('move.throw_in', { name: who, card: card(entry.card_id) });
    case 'defend':
      return t('move.defend', { name: who, card: card(entry.card_id), target: card(entry.target_id) });
    case 'transfer':
      return t('move.transfer', { name: who, card: card(entry.card_id) });
    case 'take':
      return t('move.take', { name: who });
    case 'pass':
      return t('move.pass', { name: who });
    case 'bout_end':
      if (entry.text === 'took') return t('move.bout_end_took', { name: who });
      return t('move.bout_end_bito', { n: tableCardCount(state), cards: countWord(tableCardCount(state), 'word.cards') });
    case 'took':
      return t('move.took', { name: who, n: entry.text ?? '', cards: countWord(entry.text ?? 0, 'word.cards_acc') });
    case 'bito':
      return t('move.bito', { n: entry.text ?? '', cards: countWord(entry.text ?? 0, 'word.cards') });
    case 'stump':
      return t('move.stump', { name: who });
    case 'stump_grow':
      return t('move.stump_grow', { name: who, n: entry.text ?? '' });
    case 'stump_wait':
      return t('move.stump_wait', { n: entry.text ?? '', players: countWord(entry.text ?? 0, 'word.players_gen') });
    case 'timeout':
      if (entry.text === 'auto_attack') return t('move.timeout_auto_attack', { name: who, card: card(entry.card_id) });
      if (entry.text === 'auto_take') return t('move.timeout_auto_take', { name: who });
      return t('move.timeout_auto_pass', { name: who });
    case 'out':
      return t('move.out', { name: who });
    case 'trump_revealed': {
      const suit = (entry.text ?? 'None') as Suit;
      return t('move.trump_revealed', { suit: SUIT_SYMBOL[suit] || '?' });
    }
    case 'start':
      return t('move.start', { name: who });
    case 'finish':
      return t('move.finish');
    default:
      return null; // join / leave / ready / disconnect are not moves
  }
}

/** The most recent log entry that reads as a move, as text. */
export function lastMove(state: GameState): string | null {
  for (let i = state.log.length - 1; i >= 0 && i >= state.log.length - 10; i--) {
    const text = describeMove(state, state.log[i]);
    if (text) return text;
  }
  return null;
}
