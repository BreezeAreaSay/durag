// Mirrors backend/internal/game and backend/internal/ws JSON contracts.

export type Suit = 'Hearts' | 'Diamonds' | 'Clubs' | 'Spades' | 'None';

export interface Card {
  id: string; // "H_10", "S_14", "RJ", "BJ", "SC"
  suit: Suit;
  rank: number; // 2..14, 15 = Joker, 16 = Super card
}

export interface Player {
  id: string;
  name: string;
  hand: Card[]; // only filled for the viewer
  stump: Card[]; // always empty on the client
  is_ready: boolean;
  avatar_url?: string;
  connected: boolean;
  passed: boolean;
  out: boolean;
  hand_count: number;
  stump_count: number;
}

export interface LogEntry {
  type: string;
  player_id?: string;
  card_id?: string;
  target_id?: string;
  text?: string;
}

export type Status = 'waiting' | 'playing' | 'finished';

export interface GameState {
  room_id: string;
  players: Player[];
  deck: Card[]; // always empty on the client
  trump_card: Card | null; // visible once revealed and not yet drawn
  table_cards: Record<string, Card[]>; // attack card id -> [defending card]
  current_turn_player_id: string; // lead attacker
  status: Status;
  table_order: string[];
  defender_id: string;
  defender_taking: boolean;
  trump_revealed: boolean;
  trump_suit: Suit | '';
  deck_count: number;
  discard_count: number;
  host_id: string;
  max_players: number;
  loser_id?: string;
  finished_order: string[];
  version: number;
  updated_at: number;
  log: LogEntry[];
  viewer_id: string;
  transfer_count: number;
  bout_number: number;
}

export interface ErrorPayload {
  message: string;
  code?: string;
  card_id?: string;
}

export type ServerMessage =
  | { type: 'STATE_UPDATE'; payload: GameState }
  | { type: 'ERROR'; payload: ErrorPayload }
  | { type: 'PONG' };

export interface DevUser {
  id: string;
  name: string;
  avatar_url?: string;
}

export interface JoinPayload {
  room_id: string;
  tg_init_data: string;
  create?: boolean;
  dev_user?: DevUser;
}

export type ClientMessage =
  | { type: 'JOIN_ROOM'; payload: JoinPayload }
  | { type: 'PLAY_CARD'; payload: { card_id: string; target_card_id?: string } }
  | { type: 'TRANSFER_TURN'; payload: { card_id: string } }
  | { type: 'TAKE_CARDS' }
  | { type: 'PASS' }
  | { type: 'READY'; payload: { ready: boolean } }
  | { type: 'LEAVE_ROOM' }
  | { type: 'PING' };

export interface ClientConfig {
  dev_auth: boolean;
  max_players: number;
  min_players: number;
}
