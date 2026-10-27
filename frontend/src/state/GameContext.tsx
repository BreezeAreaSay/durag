import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { ClientConfig, ClientMessage, GameState, ServerMessage, BotLevel } from '../types/protocol';
import { isEffect, type Effect } from '../util/emoji';
import { unlockAudio } from '../util/sound';
import { useTelegram } from '../telegram/useTelegram';
import { setLang, t } from '../i18n';
import { loadSkin, loadWins, saveSkin, saveWins, type SkinId } from '../util/progress';
import { applyTheme, loadTheme, type Theme } from '../util/theme';
import { FX_EVENT, applyFx, effectiveFx, loadFx, type FxLevel, type FxMode } from '../util/fx';

export type Connection = 'connecting' | 'open' | 'closed';

export interface GameError {
  message: string;
  code?: string;
  cardId?: string;
  nonce: number;
}

export interface Reaction {
  /** a bot's short line (spec v10 §2), shown as a speech bubble */
  text?: string;
  emoji: string;
  /** premium reaction: the effect the EffectLayer plays */
  effect?: Effect;
  nonce: number;
}

/** The fly in the air (caught: who slapped it, with their new XP). */
export interface LiveFly {
  id: string;
  seed: number;
  caughtBy?: string;
  xp?: number;
  nonce: number;
}

export interface LiveEffect {
  effect: Effect;
  playerId: string;
  nonce: number;
}

export interface Identity {
  kind: 'telegram' | 'dev';
  initData: string;
  devId: string;
  /** the id the server knows us by (dev:<id> or the Telegram user id): picks the food avatar */
  userId: string;
  /** the nickname for the table, chosen by the person; "" until they pick one */
  name: string;
  /** what Telegram calls them: only a suggestion for the name field, never shown at the table */
  suggested?: string;
}

export interface GameContextValue {
  connection: Connection;
  config: ClientConfig | null;
  state: GameState | null;
  selfId: string | null;
  roomId: string | null;
  identity: Identity;
  /** the server lists this identity in ADMIN_IDS */
  isAdmin: boolean;
  /** query string that proves who we are to /api/admin/* */
  adminQuery(): string;
  adminOpen: boolean;
  setAdminOpen(open: boolean): void;
  lastError: GameError | null;
  replaced: boolean;
  /** Live emoji reactions by player id (cleared automatically). */
  reactions: Record<string, Reaction>;
  /** The latest premium reaction; the EffectLayer plays each nonce once. */
  lastEffect: LiveEffect | null;
  /** The fly over the table, if any. */
  fly: LiveFly | null;
  /** Lifetime wins of the viewer: counted by the server, remembered locally. */
  wins: number;
  /** The chosen card back. */
  skin: SkinId;
  setSkin(skin: SkinId): void;
  theme: Theme;
  setTheme(theme: Theme): void;
  /** effects: auto / full / lite, and what actually runs */
  fx: FxMode;
  fxLevel: FxLevel;
  setFx(mode: FxMode): void;
  /** a one-line notice for the toast (not an error) */
  notice: string | null;
  clearNotice(): void;
  react(emoji: string): void;
  /** a quick phrase from the list (unlocked by wins) */
  say(text: string): void;
  catchFly(id: string): void;
  join(roomId: string, create: boolean, options?: { noTimer?: boolean }): void;
  leave(): void;
  addBot(level?: BotLevel): void;
  removeBot(playerId: string): void;
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
const NICK_KEY = 'durag.nick';

function loadNick(): string {
  try {
    return localStorage.getItem(NICK_KEY) ?? '';
  } catch {
    return '';
  }
}
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
  const [nick, setNick] = useState(loadNick);
  const [connection, setConnection] = useState<Connection>('connecting');
  const [config, setConfig] = useState<ClientConfig | null>(null);
  const [state, setState] = useState<GameState | null>(null);
  const [roomId, setRoomId] = useState<string | null>(() => sessionStorage.getItem(ROOM_KEY));
  const [lastError, setLastError] = useState<GameError | null>(null);
  const [replaced, setReplaced] = useState(false);
  const [reactions, setReactions] = useState<Record<string, Reaction>>({});
  const [lastEffect, setLastEffect] = useState<LiveEffect | null>(null);
  const [fly, setFly] = useState<LiveFly | null>(null);
  const flyTimer = useRef(0);
  const [isAdmin, setIsAdmin] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [wins, setWins] = useState<number>(loadWins);
  const [skin, setSkinState] = useState<SkinId>(() => loadSkin(loadWins()));
  const [theme, setThemeState] = useState<Theme>(() => {
    const t = loadTheme();
    applyTheme(t);
    return t;
  });
  const setTheme = useCallback((t: Theme) => {
    applyTheme(t);
    setThemeState(t);
  }, []);
  const [fx, setFxState] = useState<FxMode>(() => {
    const m = loadFx();
    document.documentElement.dataset.fx = effectiveFx(m);
    return m;
  });
  const [fxLevel, setFxLevel] = useState<FxLevel>(() => effectiveFx());
  const [notice, setNotice] = useState<string | null>(null);
  const setFx = useCallback((m: FxMode) => {
    applyFx(m);
    setFxState(m);
  }, []);
  useEffect(() => {
    // the table judged the phone (or the mode changed): follow, and say so once when it was automatic
    const onFx = (e: Event) => {
      const level = (e as CustomEvent<FxLevel>).detail;
      setFxLevel((prev) => {
        if (prev !== level && loadFx() === 'auto' && level === 'lite') setNotice(t('ui.fxAuto'));
        return level;
      });
    };
    window.addEventListener(FX_EVENT, onFx);
    return () => window.removeEventListener(FX_EVENT, onFx);
  }, []);
  const setSkin = useCallback((id: SkinId) => {
    saveSkin(id);
    setSkinState(id);
  }, []);
  useEffect(() => {
    if (isAdmin) setSkinState(loadSkin(Number.POSITIVE_INFINITY)); // admins own every skin
  }, [isAdmin]);
  const reactionTimers = useRef(new Map<string, number>());

  const socketRef = useRef<WebSocket | null>(null);
  const roomRef = useRef<{ roomId: string; create: boolean; noTimer?: boolean } | null>(roomId ? { roomId, create: false } : null);
  const attemptRef = useRef(0);
  const closedByUs = useRef(false);
  const nonceRef = useRef(0);

  useEffect(() => {
    setLang(tg.languageCode ?? navigator.language);
  }, [tg.languageCode]);

  // Mobile browsers only start audio after a gesture: unlock on the first tap.
  useEffect(() => {
    const unlock = () => unlockAudio();
    window.addEventListener('pointerdown', unlock, { once: true, passive: true });
    window.addEventListener('keydown', unlock, { once: true });
    return () => {
      window.removeEventListener('pointerdown', unlock);
      window.removeEventListener('keydown', unlock);
    };
  }, []);

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
        ? { kind: 'telegram', initData: tg.initData, devId: '', userId: tg.user ? String(tg.user.id) : '', name: nick, suggested: tg.user?.first_name ?? '' }
        : { kind: 'dev', initData: '', devId: devUser.id, userId: `dev:${devUser.id}`, name: nick || devUser.name },
    [tg, devUser, nick],
  );
  const identityRef = useRef(identity);
  identityRef.current = identity;

  const adminQuery = useCallback(() => {
    const id = identityRef.current;
    if (id.kind === 'telegram') return `init_data=${encodeURIComponent(id.initData)}`;
    return `dev_id=${encodeURIComponent(id.devId)}`;
  }, []);

  // Is this person an admin? One cheap question once the identity is known.
  useEffect(() => {
    if (identity.kind === 'dev' && !identity.devId) return;
    let cancelled = false;
    fetch(apiUrl(`/api/admin/me?${adminQuery()}`))
      .then((r) => (r.ok ? (r.json() as Promise<{ admin: boolean }>) : null))
      .then((me) => {
        if (!cancelled) setIsAdmin(Boolean(me?.admin));
      })
      .catch(() => {
        // not reachable: no admin button
      });
    return () => {
      cancelled = true;
    };
  }, [identity.kind, identity.initData, identity.devId, adminQuery]);

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
        no_timer: target.create && target.noTimer ? true : undefined,
        nickname: id.name || undefined,
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
          const mine = msg.payload.players.find((p) => p.id === msg.payload.viewer_id)?.wins ?? 0;
          setWins((prev) => {
            if (mine <= prev) return prev;
            saveWins(mine);
            return mine;
          });
          setRoomId(msg.payload.room_id);
          roomRef.current = { roomId: msg.payload.room_id, create: false };
          sessionStorage.setItem(ROOM_KEY, msg.payload.room_id);
        } else if (msg.type === 'REACTION') {
          const { player_id: playerId, emoji, text, effect } = msg.payload;
          nonceRef.current += 1;
          const nonce = nonceRef.current;
          const fx = isEffect(effect) ? effect : undefined;
          setReactions((prev) => ({ ...prev, [playerId]: { emoji, text, effect: fx, nonce } }));
          if (fx) setLastEffect({ effect: fx, playerId, nonce });
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
            }, text ? 5200 : 3200),
          );
        } else if (msg.type === 'FLY') {
          nonceRef.current += 1;
          const { id, seed, caught_by: caughtBy, xp } = msg.payload;
          setFly({ id, seed, caughtBy, xp, nonce: nonceRef.current });
          window.clearTimeout(flyTimer.current);
          // the fly leaves on its own after a while; a slapped one lies there a moment
          flyTimer.current = window.setTimeout(() => setFly((prev) => (prev?.id === id ? null : prev)), caughtBy ? 2500 : 9000);
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
    (id: string, create: boolean, options?: { noTimer?: boolean }) => {
      const normalized = id.trim().toUpperCase();
      if (!normalized) return;
      roomRef.current = { roomId: normalized, create, noTimer: options?.noTimer };
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
    const clean = name.trim().slice(0, 20);
    setNick(clean);
    try {
      localStorage.setItem(NICK_KEY, clean);
    } catch {
      // ignore
    }
    setDevUser((prev) => {
      const next = { ...prev, name: clean };
      try {
        localStorage.setItem(DEV_KEY, JSON.stringify(next));
      } catch {
        // ignore
      }
      return next;
    });
  }, []);

  const clearError = useCallback(() => setLastError(null), []);
  const clearNotice = useCallback(() => setNotice(null), []);

  const value = useMemo<GameContextValue>(
    () => ({
      connection,
      config,
      state,
      selfId: state?.viewer_id ?? null,
      roomId,
      identity,
      isAdmin,
      adminQuery,
      adminOpen,
      setAdminOpen,
      lastError,
      replaced,
      reactions,
      lastEffect,
      fly,
      wins,
      skin,
      setSkin,
      theme,
      setTheme,
      fx,
      fxLevel,
      setFx,
      notice,
      clearNotice,
      react: (emoji) => send({ type: 'SEND_EMOJI', payload: { emoji } }),
      say: (text) => send({ type: 'SAY', payload: { text } }),
      catchFly: (id) => send({ type: 'CATCH_FLY', payload: { id } }),
      join,
      leave,
      setReady: (ready) => send({ type: 'READY', payload: { ready } }),
      addBot: (level = 'normal') => send({ type: 'ADD_BOT', payload: { level } }),
      removeBot: (playerId) => send({ type: 'REMOVE_BOT', payload: { player_id: playerId } }),
      playCard: (cardId, targetCardId) => send({ type: 'PLAY_CARD', payload: { card_id: cardId, target_card_id: targetCardId } }),
      transfer: (cardId) => send({ type: 'TRANSFER_TURN', payload: { card_id: cardId } }),
      take: () => send({ type: 'TAKE_CARDS' }),
      pass: () => send({ type: 'RESOLVE_BOUT' }),
      takeStump: () => send({ type: 'TAKE_STUMP' }),
      setDevName,
      clearError,
    }),
    [connection, config, state, roomId, identity, isAdmin, adminQuery, adminOpen, lastError, replaced, reactions, lastEffect, fly, wins, skin, setSkin, theme, setTheme, fx, fxLevel, setFx, notice, join, leave, send, setDevName],
  );

  return <GameContext.Provider value={value}>{children}</GameContext.Provider>;
}

export function useGame(): GameContextValue {
  const ctx = useContext(GameContext);
  if (!ctx) throw new Error('useGame must be used inside <GameProvider>');
  return ctx;
}
