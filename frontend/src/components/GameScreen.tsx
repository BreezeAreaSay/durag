import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Hud } from './Hud';
import { ResultOverlay } from './ResultOverlay';
import { EmojiPanel } from './EmojiPanel';
import { EffectLayer } from './EffectLayer';
import { ReactionLayer } from './ReactionLayer';
import { FlyLayer } from './FlyLayer';
import { VodkaToy } from './VodkaToy';
import { SLAM_EMOJI } from '../util/emoji';
import { sound } from '../util/audio';
import { PixiTable } from '../pixi/PixiTable';
import { toSceneModel, type HandSort } from '../pixi/model';
import { computeMetrics } from '../pixi/layout';
import { useGame } from '../state/GameContext';
import { closingConfirmation, haptic } from '../telegram/useTelegram';
import { t } from '../i18n';
import type { GameState } from '../types/protocol';

interface Props {
  state: GameState;
}

export function GameScreen({ state }: Props) {
  const { selfId, playCard, transfer, takeStump, lastError, reactions, react, say, connection, isAdmin, skin, theme } = useGame();
  const [panelOpen, setPanelOpen] = useState(false);
  const [hudTop, setHudTop] = useState(150);
  const [size, setSize] = useState({ w: window.innerWidth, h: window.innerHeight });
  const [slam, setSlam] = useState(0);
  const [shaking, setShaking] = useState(false);
  const gameRef = useRef<HTMLDivElement>(null);
  const gazeTimer = useRef(0);
  const [sort, setSortState] = useState<HandSort>(() => {
    try {
      return localStorage.getItem('durag.sort') === 'suit' ? 'suit' : 'rank';
    } catch {
      return 'rank';
    }
  });
  const setSort = useCallback((next: HandSort) => {
    setSortState(next);
    try {
      localStorage.setItem('durag.sort', next);
    } catch {
      /* private mode: the choice lives for the session */
    }
  }, []);

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

  // the table glanced at a card: the player's sticker turns its head that way for a moment
  const onGaze = useCallback((x: number, y: number) => {
    const el = gameRef.current;
    const me = el?.querySelector<HTMLElement>('.me .avatar-btn');
    if (!el || !me) return;
    const base = el.getBoundingClientRect();
    const r = me.getBoundingClientRect();
    const cx = r.left + r.width / 2 - base.left;
    const cy = r.top + r.height / 2 - base.top;
    const angle = Math.atan2(y - cy, x - cx); // 0 = to the right
    el.style.setProperty('--gaze', `${(Math.cos(angle) * 5).toFixed(1)}deg`); // lean towards the card, at most 5°
    el.style.setProperty('--gaze-y', `${(Math.sin(angle) * 3).toFixed(1)}px`);
    el.classList.add('game--gaze');
    window.clearTimeout(gazeTimer.current);
    gazeTimer.current = window.setTimeout(() => el.classList.remove('game--gaze'), 900);
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

  const handCount = state.players.find((p) => p.id === selfId)?.hand_count ?? 0;
  const metrics = useMemo(() => computeMetrics(size.w, size.h, hudTop, handCount), [size, hudTop, handCount]);
  const model = useMemo(() => (selfId ? toSceneModel(state, selfId, skin, sort) : null), [state, selfId, skin, sort]);
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
    if (last.type === 'took') {
      if (Number(last.text) >= 5) sound.flush(); // a big take: the toilet
      else sound.took();
    }
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
          onGaze={onGaze}
          slam={slam}
          theme={theme}
        />
      ) : null}
      <Hud state={state} handTop={metrics.handTop} handMiddle={metrics.height - metrics.cardH / 2 - 58} reactions={reactions} onAvatarClick={onAvatarClick} onTopHeight={onTopHeight} sort={sort} onSort={setSort} />
      <ReactionLayer reactions={reactions} host={gameRef} target={{ x: size.w / 2, y: (metrics.tableTop + metrics.tableBottom) / 2 }} />
      {resolving ? (
        <div className={`stamp ${state.resolve_outcome === 'took' ? 'stamp--took' : ''}`} key={`${state.bout_number}-${state.resolve_outcome}`}>
          {state.resolve_outcome === 'took' ? t('game.takesStamp', { name: defenderName }) : t('game.bitoStamp')}
        </div>
      ) : null}
      <EffectLayer />
      <FlyLayer host={gameRef} top={metrics.tableTop} bottom={metrics.handTop} />
      {panelOpen ? (
        <EmojiPanel
          xp={state.players.find((p) => p.id === selfId)?.xp ?? 0}
          wins={state.players.find((p) => p.id === selfId)?.wins ?? 0}
          unlockAll={isAdmin}
          onPick={(emoji) => {
            react(emoji);
            haptic('light');
            setPanelOpen(false);
          }}
          onSay={(text) => {
            say(text);
            haptic('light');
            setPanelOpen(false);
          }}
          onClose={() => setPanelOpen(false)}
        />
      ) : null}
      {state.status === 'playing' && state.players.find((p) => p.id === selfId)?.out ? (
        <div className="vodka-seat" style={{ top: hudTop + 36 }}>
          {/* the floor is down in the empty hand zone (the player is out), clear of the bout stamp and the
              last-move label; the top stays under the header and the opponents' stickers */}
          <VodkaToy compact width={size.w} height={size.h - 56 - hudTop - 36} floor={size.h - 80 - hudTop - 36} />
        </div>
      ) : null}
      {state.status === 'finished' ? <ResultOverlay state={state} /> : null}
    </div>
  );
}
