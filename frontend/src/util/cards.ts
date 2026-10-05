import type { Card, Suit } from '../types/protocol';

export const RANK_JOKER = 15;
export const RANK_SUPER = 16;

export const SUIT_SYMBOL: Record<Suit, string> = {
  Hearts: '♥',
  Diamonds: '♦',
  Clubs: '♣',
  Spades: '♠',
  None: '',
};

const SUIT_BY_LETTER: Record<string, Suit> = { H: 'Hearts', D: 'Diamonds', C: 'Clubs', S: 'Spades' };

/** Rebuilds a card from its id (same derivation as the server). */
export function cardFromId(id: string): Card {
  if (id === 'RJ' || id === 'BJ') return { id, suit: 'None', rank: RANK_JOKER };
  if (id === 'SC') return { id, suit: 'None', rank: RANK_SUPER };
  const suit = SUIT_BY_LETTER[id[0] ?? ''] ?? 'None';
  const rank = Number.parseInt(id.slice(2), 10);
  return { id, suit, rank: Number.isFinite(rank) ? rank : 0 };
}

export function isJoker(card: Card): boolean {
  return card.rank === RANK_JOKER;
}

export function isSuper(card: Card): boolean {
  return card.rank === RANK_SUPER;
}

export function isRed(card: Card): boolean {
  return card.suit === 'Hearts' || card.suit === 'Diamonds' || card.id === 'RJ';
}

export function isBlack(card: Card): boolean {
  return card.suit === 'Clubs' || card.suit === 'Spades' || card.id === 'BJ';
}

export type CardColor = 'red' | 'black' | 'none';

export function cardColor(card: Card): CardColor {
  if (isRed(card)) return 'red';
  if (isBlack(card)) return 'black';
  return 'none';
}

/** Short rank label for the card corner. */
export function rankLabel(card: Card): string {
  switch (card.rank) {
    case 11:
      return 'J';
    case 12:
      return 'Q';
    case 13:
      return 'K';
    case 14:
      return 'A';
    case RANK_JOKER:
      return 'JK';
    case RANK_SUPER:
      return 'SC';
    default:
      return String(card.rank);
  }
}

/** Human readable card name, e.g. "10♥", "Red Joker". */
export function cardLabel(card: Card): string {
  if (card.id === 'RJ') return 'RED JOKER';
  if (card.id === 'BJ') return 'BLACK JOKER';
  if (card.id === 'SC') return 'SUPER';
  return rankLabel(card) + SUIT_SYMBOL[card.suit];
}
