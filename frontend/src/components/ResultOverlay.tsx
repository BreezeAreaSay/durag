import { Button } from './Button';
import { useGame } from '../state/GameContext';
import { t } from '../i18n';
import type { GameState } from '../types/protocol';
import { VodkaToy } from './VodkaToy';

interface Props {
  state: GameState;
}

export function ResultOverlay({ state }: Props) {
  const { selfId, setReady, leave, wins } = useGame();
  const loser = state.players.find((p) => p.id === state.loser_id);
  const me = state.players.find((p) => p.id === selfId);
  const title = !state.loser_id ? t('result.draw') : state.loser_id === selfId ? t('result.you') : t('result.durak', { name: loser?.name ?? '?' });
  const winners = state.finished_order.map((id) => state.players.find((p) => p.id === id)?.name ?? id);
  return (
    <div className="overlay">
      <div className="overlay__card">
        <h1 className="zine-title zine-title--result">{title}</h1>
        {state.finished_order[0] === selfId ? <p className="sticker-text result__win">{t('result.win', { n: wins })}</p> : null}
        {state.finished_order[0] === selfId ? <VodkaToy /> : null}
        {state.loser_id === selfId ? <p className="sticker-text result__scar">{t('result.scar')}</p> : null}
        {winners.length ? (
          <p className="muted">
            {t('result.winners')}: {winners.join(' → ')}
          </p>
        ) : null}
        <div className="row row--center">
          <Button big variant={me?.is_ready ? 'ink' : 'acid'} onClick={() => setReady(!me?.is_ready)}>
            {me?.is_ready ? t('waiting.notReady') : t('result.again')}
          </Button>
          <Button variant="paper" onClick={leave}>
            {t('game.leave')}
          </Button>
        </div>
      </div>
    </div>
  );
}
