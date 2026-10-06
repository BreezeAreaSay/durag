import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { ClientConfig, ClientMessage, GameState, ServerMessage } from '../types/protocol';
import { useTelegram } from '../telegram/useTelegram';
import { setLang } from '../i18n';

export type Connection = 'connecting' | 'open' | 'closed';

export interface GameError {
  message: string;
  code?: string;
  cardId?: string;
  nonce: number;
}

export interface Reaction {
  emoji: string;
  nonce: number;
}

export interface Identity {
  kind: 'telegram' | 'dev';
  initData: string;
  devId: string;
  name: string;
  avatarUrl?: string;
}

export interface GameContextValue {
  connection: Connection;
  config: ClientConfig | null;
  state: GameState | null;
  selfId: string | null;
  roomId: string | null;
  identity: Identity;
  lastError: GameError | null;
  replaced: boolean;
  /** Live emoji reactions by player id (cleared automatically). */
  reactions: Record<string, Reaction>;
  react(emoji: string): void;
  join(roomId: string, create: boolean): void;
  leave(): void;
  setReady(ready: boolean): void;
  playCard(cardId: string, targetCardId?: string): void;
  transfer(cardId: string): void;
  take(): void;
  pass(): void;
  takeStump(): void;
  setDevName(name: string): void;
  clearError(): void;
}

const GameContext = createContext<GameContextValue | null>(null);

const DEV_KEY = 'durag.dev';
const ROOM_KEY = 'durag.room';

function wsUrl(): string {
  const configured = import.meta.env.VITE_WS_URL as string | undefined;
  if (configured) return configured;
  const proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
  return proto + location.host + '/ws';
}

/** HTTP API lives next to the WebSocket endpoint (same origin by default). */
function apiUrl(path: string): string {
  const configured = import.meta.env.VITE_WS_URL as string | undefined;
  if (!configured) return path;
  try {
    const u = new URL(configured);
    u.protocol = u.protocol === 'wss:' ? 'https:' : 'http:';
    u.pathname = path;
    u.search = '';
    return u.toString();
  } catch {
    return path;
  }
}

function randomId(len = 8): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let out = '';
  const bytes = new Uint8Array(len);
  crypto.getRandomValues(bytes);
  for (const b of bytes) out += alphabet[b % alphabet.length];
  return out;
}

function loadDev(): { id: string; name: string } {
  try {
    const raw = localStorage.getItem(DEV_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as { id?: string; name?: string };
      if (parsed.id) return { id: parsed.id, name: parsed.name ?? '' };
    }
  } catch {
    // ignore
  }
  const fresh = { id: randomId(), name: '' };
  try {
    localStorage.setItem(DEV_KEY, JSON.stringify(fresh));
  } catch {
    // ignore
  }
  return fresh;
}

export function GameProvider({ children }: { children: ReactNode }) {
  const tg = useTelegram();
  const [devUser, setDevUser] = useState(loadDev);
  const [connection, setConnection] = useState<Connection>('connecting');
  const [config, setConfig] = useState<ClientConfig | null>(null);
  const [state, setState] = useState<GameState | null>(null);
  const [roomId, setRoomId] = useState<string | null>(() => sessionStorage.getItem(ROOM_KEY));
  const [lastError, setLastError] = useState<GameError | null>(null);
  const [replaced, setReplaced] = useState(false);
  const [reactions, setReactions] = useState<Record<string, Reaction>>({});
  const reactionTimers = useRef(new Map<string, number>());

  const socketRef = useRef<WebSocket | null>(null);
  const roomRef = useRef<{ roomId: string; create: boolean } | null>(roomId ? { roomId, create: false } : null);
  const attemptRef = useRef(0);
  const closedByUs = useRef(false);
  const nonceRef = useRef(0);

  useEffect(() => {
    setLang(tg.languageCode ?? navigator.language);
  }, [tg.languageCode]);

  // Public server configuration (is dev login allowed, table sizes).
  useEffect(() => {
    let cancelled = false;
    fetch(apiUrl('/api/config'))
      .then((r) => (r.ok ? (r.json() as Promise<ClientConfig>) : null))
      .then((cfg) => {
        if (!cancelled && cfg) setConfig(cfg);
      })
      .catch(() => {
        // the backend may still be starting; the socket reconnect loop will tell
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const identity = useMemo<Identity>(
    () =>
      tg.isTelegram
        ? { kind: 'telegram', initData: tg.initData, devId: '', name: tg.user?.first_name ?? '', avatarUrl: tg.user?.photo_url }
        : { kind: 'dev', initData: '', devId: devUser.id, name: devUser.name },
    [tg, devUser],
  );
  const identityRef = useRef(identity);
  identityRef.current = identity;

  const send = useCallback((msg: ClientMessage) => {
    const ws = socketRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
  }, []);

  const sendJoin = useCallback(() => {
    const target = roomRef.current;
    if (!target) return;
    const id = identityRef.current;
    send({
      type: 'JOIN_ROOM',
      payload: {
        room_id: target.roomId,
        tg_init_data: id.initData,
        create: target.create,
        dev_user: id.kind === 'dev' ? { id: id.devId, name: id.name || `Guest ${id.devId.slice(0, 4)}` } : undefined,
      },
    });
  }, [send]);

  // Connection management with exponential back-off.
  useEffect(() => {
    let timer: number | undefined;
    let disposed = false;

    const connect = () => {
      if (disposed) return;
      setConnection('connecting');
      const ws = new WebSocket(wsUrl());
      socketRef.current = ws;
      ws.onopen = () => {
        attemptRef.current = 0;
        setConnection('open');
        sendJoin();
      };
      ws.onmessage = (ev) => {
        let msg: ServerMessage;
        try {
          msg = JSON.parse(ev.data as string) as ServerMessage;
        } catch {
          return;
        }
        if (msg.type === 'STATE_UPDATE') {
          setState(msg.payload);
          setRoomId(msg.payload.room_id);
          roomRef.current = { roomId: msg.payload.room_id, create: false };
          sessionStorage.setItem(ROOM_KEY, msg.payload.room_id);
        } else if (msg.type === 'REACTION') {
          const { player_id: playerId, emoji } = msg.payload;
          nonceRef.current += 1;
          const nonce = nonceRef.current;
          setReactions((prev) => ({ ...prev, [playerId]: { emoji, nonce } }));
          const previous = reactionTimers.current.get(playerId);
          if (previous) window.clearTimeout(previous);
          reactionTimers.current.set(
            playerId,
            window.setTimeout(() => {
              reactionTimers.current.delete(playerId);
              setReactions((prev) => {
                if (prev[playerId]?.nonce !== nonce) return prev;
                const next = { ...prev };
                delete next[playerId];
                return next;
              });
            }, 3200),
          );
        } else if (msg.type === 'ERROR') {
          nonceRef.current += 1;
          const err: GameError = { message: msg.payload.message, code: msg.payload.code, cardId: msg.payload.card_id, nonce: nonceRef.current };
          if (err.code === 'REPLACED') {
            closedByUs.current = true;
            setReplaced(true);
          }
          if (err.code === 'ROOM_NOT_FOUND' || err.code === 'UNAUTHORIZED' || err.code === 'ROOM_FULL' || err.code === 'GAME_IN_PROGRESS') {
            // Joining failed: forget the room so the lobby shows again.
            roomRef.current = null;
            sessionStorage.removeItem(ROOM_KEY);
            setRoomId(null);
            setState(null);
          }
          setLastError(err);
        }
      };
      ws.onclose = () => {
        socketRef.current = null;
        setConnection('closed');
        if (disposed || closedByUs.current) return;
        const delay = Math.min(10000, 500 * 2 ** attemptRef.current);
        attemptRef.current += 1;
        timer = window.setTimeout(connect, delay);
      };
      ws.onerror = () => {
        ws.close();
      };
    };
    connect();
    return () => {
      disposed = true;
      if (timer) window.clearTimeout(timer);
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [sendJoin]);

  useEffect(() => {
    const timers = reactionTimers.current;
    return () => {
      for (const id of timers.values()) window.clearTimeout(id);
      timers.clear();
    };
  }, []);

  const join = useCallback(
    (id: string, create: boolean) => {
      const normalized = id.trim().toUpperCase();
      if (!normalized) return;
      roomRef.current = { roomId: normalized, create };
      setRoomId(normalized);
      sendJoin();
    },
    [sendJoin],
  );

  const leave = useCallback(() => {
    send({ type: 'LEAVE_ROOM' });
    roomRef.current = null;
    sessionStorage.removeItem(ROOM_KEY);
    setRoomId(null);
    setState(null);
  }, [send]);

  const setDevName = useCallback((name: string) => {
    setDevUser((prev) => {
      const next = { ...prev, name: name.trim().slice(0, 32) };
      try {
        localStorage.setItem(DEV_KEY, JSON.stringify(next));
      } catch {
        // ignore
      }
      return next;
    });
  }, []);

  const value = useMemo<GameContextValue>(
    () => ({
      connection,
      config,
      state,
      selfId: state?.viewer_id ?? null,
      roomId,
      identity,
      lastError,
      replaced,
      reactions,
      react: (emoji) => send({ type: 'SEND_EMOJI', payload: { emoji } }),
      join,
      leave,
      setReady: (ready) => send({ type: 'READY', payload: { ready } }),
      playCard: (cardId, targetCardId) => send({ type: 'PLAY_CARD', payload: { card_id: cardId, target_card_id: targetCardId } }),
      transfer: (cardId) => send({ type: 'TRANSFER_TURN', payload: { card_id: cardId } }),
      take: () => send({ type: 'TAKE_CARDS' }),
      pass: () => send({ type: 'RESOLVE_BOUT' }),
      takeStump: () => send({ type: 'TAKE_STUMP' }),
      setDevName,
      clearError: () => setLastError(null),
    }),
    [connection, config, state, roomId, identity, lastError, replaced, reactions, join, leave, send, setDevName],
  );

  return <GameContext.Provider value={value}>{children}</GameContext.Provider>;
}

export function useGame(): GameContextValue {
  const ctx = useContext(GameContext);
  if (!ctx) throw new Error('useGame must be used inside <GameProvider>');
  return ctx;
}
