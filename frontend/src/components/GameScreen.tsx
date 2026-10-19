import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Hud } from './Hud';
import { ResultOverlay } from './ResultOverlay';
import { EmojiPanel } from './EmojiPanel';
import { ReactionLayer, type Flight } from './ReactionLayer';
import { SLAM_EMOJI } from '../util/emoji';
import { sound } from '../util/audio';
import { PixiTable } from '../pixi/PixiTable';
import { toSceneModel } from '../pixi/model';
import { computeMetrics } from '../pixi/layout';
import { useGame } from '../state/GameContext';
import { closingConfirmation, haptic } from '../telegram/useTelegram';
import { t } from '../i18n';
import type { GameState } from '../types/protocol';

interface Props {
  state: GameState;
}

export function GameScreen({ state }: Props) {
  const { selfId, playCard, transfer, takeStump, lastError, reactions, react, connection, skin, theme } = useGame();
  const [panelOpen, setPanelOpen] = useState(false);
  const [hudTop, setHudTop] = useState(150);
  const [size, setSize] = useState({ w: window.innerWidth, h: window.innerHeight });
  const [slam, setSlam] = useState(0);
  const [shaking, setShaking] = useState(false);
  const [extra, setExtra] = useState<Flight[]>([]);
  const gameRef = useRef<HTMLDivElement>(null);
  const flightNonce = useRef(0);

  // a fist on the table from anybody in the room: cards jump, the screen shakes
  useEffect(() => {
    const hit = Object.values(reactions).find((r) => r.emoji === SLAM_EMOJI);
    if (!hit) return;
    setSlam(hit.nonce);
    setShaking(true);
    haptic('heavy');
    const id = window.setTimeout(() => setShaking(false), 650);
    return () => window.clearTimeout(id);
  }, [reactions]);

  const onCardEmoji = useCallback((emoji: string, x: number, y: number) => {
    flightNonce.current += 1;
    const key = `card:${flightNonce.current}`;
    setExtra((prev) => [...prev, { key, emoji, x, y }]);
    window.setTimeout(() => setExtra((prev) => prev.filter((f) => f.key !== key)), 1300);
  }, []);

  const onTopHeight = useCallback((h: number) => setHudTop((prev) => (Math.abs(prev - h) < 1 ? prev : Math.round(h))), []);
  const onAvatarClick = useCallback(() => setPanelOpen(true), []);

  useEffect(() => {
    const el = gameRef.current;
    if (!el) return;
    const measure = () => setSize((prev) => (prev.w === el.clientWidth && prev.h === el.clientHeight ? prev : { w: el.clientWidth, h: el.clientHeight }));
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const metrics = useMemo(() => computeMetrics(size.w, size.h, hudTop), [size, hudTop]);
  const model = useMemo(() => (selfId ? toSceneModel(state, selfId, skin) : null), [state, selfId, skin]);
  const reject = useMemo(() => (lastError?.cardId ? { cardId: lastError.cardId, nonce: lastError.nonce } : null), [lastError]);
  const labels = useMemo(() => ({ trump: t('game.trump'), stump: t('game.stump').toUpperCase() }), []);

  useEffect(() => {
    closingConfirmation(state.status === 'playing');
    return () => closingConfirmation(false);
  }, [state.status]);

  useEffect(() => {
    const last = state.log[state.log.length - 1];
    if (!last) return;
    if (last.type === 'bout_end' || last.type === 'took' || last.type === 'bito') haptic('medium');
    else if (last.type === 'finish') haptic('success');
    if (last.type === 'took') sound.took();
    else if (last.type === 'bito') sound.bito();
  }, [state.log]);

  const resolving = state.phase === 'resolving';
  const defenderName = state.players.find((p) => p.id === state.defender_id)?.name ?? '?';

  return (
    <div className={`game ${shaking ? 'game--shake' : ''}`} ref={gameRef} onPointerDown={() => sound.unlock()}>
      {model ? (
        <PixiTable
          model={model}
          reject={reject}
          topInset={hudTop}
          labels={labels}
          connection={connection}
          onTakeStump={() => {
            haptic('medium');
            takeStump();
          }}
          onPlay={(cardId, targetId) => {
            haptic('light');
            playCard(cardId, targetId);
          }}
          onTransfer={(cardId) => {
            haptic('light');
            transfer(cardId);
          }}
          onSlam={() => react(SLAM_EMOJI)}
          onCardEmoji={onCardEmoji}
          slam={slam}
          theme={theme}
        />
      ) : null}
      <Hud state={state} handTop={metrics.handTop} reactions={reactions} onAvatarClick={onAvatarClick} onTopHeight={onTopHeight} />
      <ReactionLayer reactions={reactions} extra={extra} host={gameRef} target={{ x: size.w / 2, y: (metrics.tableTop + metrics.tableBottom) / 2 }} />
      {resolving ? (
        <div className={`stamp ${state.resolve_outcome === 'took' ? 'stamp--took' : ''}`} key={`${state.bout_number}-${state.resolve_outcome}`}>
          {state.resolve_outcome === 'took' ? t('game.takesStamp', { name: defenderName }) : t('game.bitoStamp')}
        </div>
      ) : null}
      {panelOpen ? (
        <EmojiPanel
          onPick={(emoji) => {
            react(emoji);
            haptic('light');
            setPanelOpen(false);
          }}
          onClose={() => setPanelOpen(false)}
        />
      ) : null}
      {state.status === 'finished' ? <ResultOverlay state={state} /> : null}
    </div>
  );
}
