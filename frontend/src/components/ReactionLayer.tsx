import { useEffect, useRef, useState, type RefObject } from 'react';
import type { Reaction } from '../state/GameContext';

interface Props {
  reactions: Record<string, Reaction>;
  /** the game container: flights are positioned inside it */
  host: RefObject<HTMLDivElement | null>;
  /** where the emoji fly to (CSS px inside host): the middle of the table */
  target: { x: number; y: number };
}

interface Flight {
  key: string;
  emoji: string;
  x: number;
  y: number;
  dx: number;
  dy: number;
}

const FLIGHT_MS = 1100;

/**
 * Emoji take off from the sender's avatar and sail to the middle of the
 * table, growing and dissolving on the way. The path is an arc without any
 * JS per frame: the outer element moves along X linearly while the inner
 * one moves along Y with an ease-out, and two different easings on two
 * axes trace a parabola. Opacity fades linearly in parallel.
 */
export function ReactionLayer({ reactions, host, target }: Props) {
  const [flights, setFlights] = useState<Flight[]>([]);
  const seen = useRef(new Map<string, number>());
  const targetRef = useRef(target);
  targetRef.current = target;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const base = el.getBoundingClientRect();
    const fresh: Flight[] = [];
    for (const [playerId, r] of Object.entries(reactions)) {
      if (seen.current.get(playerId) === r.nonce) continue;
      seen.current.set(playerId, r.nonce);
      if (!r.emoji) continue;
      const avatar = el.querySelector<HTMLElement>(`[data-player="${CSS.escape(playerId)}"]`);
      const rect = avatar?.getBoundingClientRect();
      const x = rect ? rect.left + rect.width / 2 - base.left : base.width / 2;
      const y = rect ? rect.top + rect.height / 2 - base.top : base.height - 60;
      const t = targetRef.current;
      fresh.push({ key: `${playerId}:${r.nonce}`, emoji: r.emoji, x, y, dx: t.x - x, dy: t.y - y });
    }
    if (fresh.length === 0) return;
    setFlights((prev) => [...prev, ...fresh]);
    const keys = new Set(fresh.map((f) => f.key));
    const id = window.setTimeout(() => setFlights((prev) => prev.filter((f) => !keys.has(f.key))), FLIGHT_MS + 100);
    return () => window.clearTimeout(id);
  }, [reactions, host]);

  if (flights.length === 0) return null;
  return (
    <div className="fx-layer" aria-hidden="true">
      {flights.map((f) => (
        <span key={f.key} className="fly" style={{ left: f.x, top: f.y, ['--dx' as string]: `${f.dx}px`, ['--dy' as string]: `${f.dy}px` }}>
          <span className="fly__y">{f.emoji}</span>
        </span>
      ))}
    </div>
  );
}
