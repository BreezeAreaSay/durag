import { Button } from './Button';
import { Sticker } from './Sticker';
import { useGame } from '../state/GameContext';
import { t } from '../i18n';
import { SUIT_SYMBOL } from '../util/cards';
import { roleOf, tablePairs } from '../util/rules';
import { lastMove } from '../util/moves';
import type { GameState } from '../types/protocol';

interface Props {
  state: GameState;
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

export function Hud({ state }: Props) {
  const { selfId, take, pass, leave, connection } = useGame();
  const me = state.players.find((p) => p.id === selfId);
  const role = selfId ? roleOf(state, selfId) : 'spectator';
  const tableEmpty = state.table_order.length === 0;
  const resolving = state.phase === 'resolving';
  const canTake = role === 'defender' && !tableEmpty && !state.defender_taking && !resolving;
  const canPass = (role === 'attacker' || role === 'thrower') && !tableEmpty && !me?.passed && (me?.hand_count ?? 0) > 0 && !resolving;
  const last = lastMove(state);
  const trump = state.trump_revealed && state.trump_suit ? `${t('game.trump')} ${SUIT_SYMBOL[state.trump_suit]}` : t('game.trumpHidden');
  const opponents = state.players.filter((p) => p.id !== selfId);

  return (
    <>
      <header className="hud-top">
        <div className="hud-top__row">
          <span className="chip chip--ink">{state.room_id}</span>
          <span className={`chip ${state.trump_revealed ? 'chip--acid' : ''}`}>{trump}</span>
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
            const isDef = p.id === state.defender_id;
            const isAtt = p.id === state.current_turn_player_id;
            const badge = p.out ? '✓' : isDef ? '🛡' : isAtt ? '⚔' : p.passed ? '·' : undefined;
            const tag = p.out ? t('game.out') : !p.connected ? t('game.offline') : isDef && state.defender_taking ? t('game.taking') : isDef ? t('game.defender') : isAtt ? t('game.attacker') : p.passed ? t('game.passed') : '';
            return (
              <li key={p.id} className={`opponent ${isDef ? 'opponent--defender' : ''} ${isAtt ? 'opponent--attacker' : ''} ${p.out ? 'opponent--out' : ''}`}>
                <Sticker name={p.name} avatarUrl={p.avatar_url} seed={p.id} size={40} muted={!p.connected || p.out} badge={badge} />
                <span className="opponent__name">{p.name}</span>
                <span className="opponent__cards">
                  {p.hand_count}
                  {p.stump_count ? <small> +{p.stump_count}</small> : null}
                </span>
                {tag ? <span className="opponent__tag">{tag}</span> : null}
              </li>
            );
          })}
        </ul>
        <p className={`status ${(role === 'attacker' || role === 'defender') && !resolving ? 'status--hot' : ''}`}>{selfId ? statusLine(state, selfId) : ''}</p>
        {last ? <p className="lastmove">{last}</p> : null}
        {connection !== 'open' ? <p className="status status--warn">{t('conn.reconnecting')}</p> : null}
      </header>

      <footer className="hud-bottom">
        <div className="hud-bottom__row">
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
        {me ? (
          <div className="me">
            <Sticker name={me.name} avatarUrl={me.avatar_url} seed={me.id} size={34} />
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
