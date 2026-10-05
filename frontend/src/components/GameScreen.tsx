import { useEffect, useMemo } from 'react';
import { Hud } from './Hud';
import { ResultOverlay } from './ResultOverlay';
import { PixiTable } from '../pixi/PixiTable';
import { toSceneModel } from '../pixi/model';
import { useGame } from '../state/GameContext';
import { closingConfirmation, haptic } from '../telegram/useTelegram';
import type { GameState } from '../types/protocol';

interface Props {
  state: GameState;
}

export function GameScreen({ state }: Props) {
  const { selfId, playCard, transfer, lastError } = useGame();
  const model = useMemo(() => (selfId ? toSceneModel(state, selfId) : null), [state, selfId]);
  const reject = useMemo(() => (lastError?.cardId ? { cardId: lastError.cardId, nonce: lastError.nonce } : null), [lastError]);

  useEffect(() => {
    closingConfirmation(state.status === 'playing');
    return () => closingConfirmation(false);
  }, [state.status]);

  useEffect(() => {
    const last = state.log[state.log.length - 1];
    if (!last) return;
    if (last.type === 'took' || last.type === 'bito') haptic('medium');
    else if (last.type === 'finish') haptic('success');
  }, [state.log]);

  return (
    <div className="game">
      {model ? (
        <PixiTable
          model={model}
          reject={reject}
          onPlay={(cardId, targetId) => {
            haptic('light');
            playCard(cardId, targetId);
          }}
          onTransfer={(cardId) => {
            haptic('light');
            transfer(cardId);
          }}
        />
      ) : null}
      <Hud state={state} />
      {state.status === 'finished' ? <ResultOverlay state={state} /> : null}
    </div>
  );
}
