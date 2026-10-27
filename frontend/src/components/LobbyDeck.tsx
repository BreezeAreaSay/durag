import { useRef, useState, type PointerEvent } from 'react';
import { useGame } from '../state/GameContext';
import { sound } from '../util/audio';

const CARDS = 7;

/**
 * Something for the hands while the table fills: a small deck under the
 * player list. Drag across it and the cards fan out after the finger; let
 * go and they snap back with a riffle.
 */
export function LobbyDeck() {
  const { skin } = useGame();
  const [spread, setSpread] = useState<{ dx: number; dy: number } | null>(null);
  const start = useRef<{ x: number; y: number } | null>(null);

  const down = (e: PointerEvent) => {
    start.current = { x: e.clientX, y: e.clientY };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const move = (e: PointerEvent) => {
    const s = start.current;
    if (!s) return;
    const dx = Math.max(-140, Math.min(140, e.clientX - s.x));
    const dy = Math.max(-60, Math.min(60, e.clientY - s.y));
    setSpread({ dx, dy });
  };
  const up = () => {
    if (!start.current) return;
    start.current = null;
    if (spread && Math.hypot(spread.dx, spread.dy) > 30) sound.riffle();
    setSpread(null);
  };

  return (
    <div className={`lobby-deck ${spread ? 'lobby-deck--held' : ''}`} onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={up} aria-hidden="true">
      {Array.from({ length: CARDS }, (_, i) => {
        const k = i / (CARDS - 1);
        const style = spread
          ? { transform: `translate(${(spread.dx * k).toFixed(0)}px, ${(spread.dy * k - i * 1.5).toFixed(0)}px) rotate(${(spread.dx * k * 0.12 + (k - 0.5) * 4).toFixed(1)}deg)` }
          : { transform: `translate(${i * 1.2}px, ${-i * 1.5}px) rotate(${(k - 0.5) * 2}deg)` };
        return <span key={i} className={`back back--${skin} lobby-deck__card`} style={style} />;
      })}
    </div>
  );
}
