import { useEffect, useRef } from 'react';
import { Button } from './Button';
import { Sticker } from './Sticker';
import { TimerRing } from './TimerRing';
import { useGame, type Reaction } from '../state/GameContext';
import { t, type Key } from '../i18n';
import { SUIT_SYMBOL } from '../util/cards';
import { roleOf, tablePairs } from '../util/rules';
import { lastMove } from '../util/moves';
import type { GameState, Player } from '../types/protocol';

interface Props {
  state: GameState;
  /** y (CSS px) where the hand starts; the action band sits right above it. */
  handTop: number;
  reactions: Record<string, Reaction>;
  onAvatarClick(playerId: string): void;
  /** Reports the rendered height of the top panel so the table can avoid it. */
  onTopHeight(height: number): void;
}

type RoleKey = 'attack' | 'throw' | 'defend' | 'take' | 'pass' | 'wait' | 'out' | 'offline' | 'stump';
const HOT_ROLES: RoleKey[] = ['attack', 'defend', 'take', 'throw', 'stump'];

/** What a player is doing right now, for the tag next to their avatar. */
function roleKey(state: GameState, p: Player): RoleKey {
  if (p.out) return 'out';
  if (!p.connected) return 'offline';
  if (state.status !== 'playing') return 'wait';
  if (state.stump_pending.includes(p.id)) return 'stump';
  const tableEmpty = state.table_order.length === 0;
  if (p.id === state.defender_id) return state.defender_taking ? 'take' : 'defend';
  if (p.id === state.current_turn_player_id) return p.passed && !tableEmpty ? 'pass' : 'attack';
  if (tableEmpty) return 'wait';
  return p.passed ? 'pass' : 'throw';
}

function roleLabel(key: RoleKey, self: boolean): string {
  return t(`${self ? 'role.me.' : 'role.'}${key}` as Key);
}

function statusLine(state: GameState, selfId: string): string {
  const role = roleOf(state, selfId);
  const me = state.players.find((p) => p.id === selfId);
  const defender = state.players.find((p) => p.id === state.defender_id);
  const attacker = state.players.find((p) => p.id === state.current_turn_player_id);
  const tableEmpty = state.table_order.length === 0;
  if (state.phase === 'resolving') {
    return state.resolve_outcome === 'took' ? t('game.takesStamp', { name: defender?.name ?? '?' }) : t('game.resolvingBito');
  }
  if (state.stump_pending.length > 0) {
    if (state.stump_pending.includes(selfId)) return t('game.takeStumpHint');
    const names = state.stump_pending.map((id) => state.players.find((p) => p.id === id)?.name ?? '?').join(', ');
    return t('game.stumpWait', { names });
  }
  if (me?.out) return t('game.youOut');
  switch (role) {
    case 'attacker':
      if (tableEmpty) return t('game.yourAttack');
      return t('game.throwOrPass');
    case 'defender':
      if (state.defender_taking) return t('game.youTake');
      if (tableEmpty) return t('game.waitFor', { name: attacker?.name ?? '?' });
      return tablePairs(state).some((p) => p.defense) ? t('game.defendNoTransfer') : t('game.defend');
    case 'thrower':
      if (tableEmpty) return t('game.waitFor', { name: attacker?.name ?? '?' });
      if (state.defender_taking) return t('game.takes', { name: defender?.name ?? '?' });
      return t('game.throwIn');
    default:
      return tableEmpty ? t('game.waitFor', { name: attacker?.name ?? '?' }) : t('game.defends', { name: defender?.name ?? '?' });
  }
}

export function Hud({ state, handTop, reactions, onAvatarClick, onTopHeight }: Props) {
  const { selfId, take, pass, takeStump, leave, connection } = useGame();
  const topRef = useRef<HTMLElement>(null);

  useEffect(() => {
    const el = topRef.current;
    if (!el) return;
    const report = () => onTopHeight(el.getBoundingClientRect().height);
    report();
    const observer = new ResizeObserver(report);
    observer.observe(el);
    return () => observer.disconnect();
  }, [onTopHeight]);

  const me = state.players.find((p) => p.id === selfId);
  const role = selfId ? roleOf(state, selfId) : 'spectator';
  const tableEmpty = state.table_order.length === 0;
  const resolving = state.phase === 'resolving';
  const stumpStep = state.stump_pending.length > 0;
  const mustTakeStump = selfId !== null && state.stump_pending.includes(selfId);
  const canTake = role === 'defender' && !tableEmpty && !state.defender_taking && !resolving && !stumpStep;
  const canPass = (role === 'attacker' || role === 'thrower') && !tableEmpty && !me?.passed && (me?.hand_count ?? 0) > 0 && !resolving && !stumpStep;
  const timerFor = (id: string) => (state.turn_deadline > 0 && state.turn_actors.includes(id) ? state.turn_deadline : 0);
  const last = lastMove(state);
  const trumpKnown = state.trump_revealed && state.trump_suit !== '';
  const trumpRed = state.trump_suit === 'Hearts' || state.trump_suit === 'Diamonds';
  const opponents = state.players.filter((p) => p.id !== selfId);
  const myRole = me ? roleKey(state, me) : 'wait';

  return (
    <>
      <header className="hud-top" ref={topRef}>
        <div className="hud-top__row">
          <span className="chip chip--ink">{state.room_id}</span>
          <span className={`chip chip--trump ${trumpKnown ? 'chip--acid' : 'chip--ghost'}`} title={state.trump_suit || undefined}>
            {t('game.trump')} <b className={`chip__suit ${trumpRed ? 'chip__suit--red' : ''}`}>{trumpKnown ? SUIT_SYMBOL[state.trump_suit as Exclude<typeof state.trump_suit, ''>] : '?'}</b>
          </span>
          <span className="chip">
            {t('game.deck')} {state.deck_count}
          </span>
          <span className="chip chip--ghost">
            {t('game.bout')} {state.bout_number}
          </span>
          <button className="chip chip--link" onClick={leave}>
            {t('game.leave')}
          </button>
        </div>
        <ul className="opponents">
          {opponents.map((p) => {
            const key = roleKey(state, p);
            const hot = HOT_ROLES.includes(key) && !resolving;
            const reaction = reactions[p.id];
            return (
              <li key={p.id} className={`opponent ${p.out ? 'opponent--out' : ''}`}>
                <button type="button" className="avatar-btn" onClick={() => onAvatarClick(p.id)} aria-label={p.name}>
                  <Sticker name={p.name} avatarUrl={p.avatar_url} seed={p.id} size={40} muted={!p.connected || p.out} />
                  {timerFor(p.id) ? <TimerRing deadline={timerFor(p.id)} timeoutMs={state.turn_timeout_ms} size={64} /> : null}
                </button>
                {reaction ? (
                  <span className="reaction" key={reaction.nonce}>
                    {reaction.emoji}
                  </span>
                ) : null}
                <div className="opponent__info">
                  <span className="opponent__name">{p.name}</span>
                  <span className="opponent__cards">
                    {p.hand_count}
                    {p.stump_count ? <small> +{p.stump_count}</small> : null}
                  </span>
                  <span className={`roletag ${hot ? 'roletag--hot' : ''}`}>{roleLabel(key, false)}</span>
                </div>
              </li>
            );
          })}
        </ul>
      </header>

      {/* Status, last move and the action buttons live right above the hand. */}
      <div className="actionband" style={{ top: Math.max(0, handTop - 96) }}>
        <div className="actionband__text">
          <p className={`status ${(role === 'attacker' || role === 'defender') && !resolving ? 'status--hot' : ''}`}>{selfId ? statusLine(state, selfId) : ''}</p>
          {last ? <p className="lastmove">{last}</p> : null}
          {connection !== 'open' ? <p className="status status--warn">{t('conn.reconnecting')}</p> : null}
        </div>
        <div className="actionband__buttons">
          {mustTakeStump ? (
            <Button big onClick={takeStump} className="btn--pulse">
              {t('game.takeStump')}
            </Button>
          ) : null}
          {canTake ? (
            <Button big onClick={take}>
              {t('game.take')}
            </Button>
          ) : null}
          {canPass ? (
            <Button big variant="paper" onClick={pass}>
              {t('game.pass')}
            </Button>
          ) : null}
        </div>
      </div>

      <footer className="hud-bottom">
        {me ? (
          <div className="me">
            <button type="button" className="avatar-btn" onClick={() => onAvatarClick(me.id)} aria-label={me.name}>
              <Sticker name={me.name} avatarUrl={me.avatar_url} seed={me.id} size={34} />
              {timerFor(me.id) ? <TimerRing deadline={timerFor(me.id)} timeoutMs={state.turn_timeout_ms} size={58} /> : null}
            </button>
            {reactions[me.id] ? (
              <span className="reaction reaction--me" key={reactions[me.id]!.nonce}>
                {reactions[me.id]!.emoji}
              </span>
            ) : null}
            <span className={`roletag roletag--me ${HOT_ROLES.includes(myRole) && !resolving ? 'roletag--hot' : ''}`}>{roleLabel(myRole, true)}</span>
            <span className="me__meta">
              {me.name} · {me.hand_count}
              {me.stump_count ? ` · ${t('game.stump')} ${me.stump_count}` : ''}
            </span>
          </div>
        ) : null}
      </footer>
    </>
  );
}
