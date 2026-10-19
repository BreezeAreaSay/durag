import { useEffect, useRef, useState, type RefObject } from 'react';
import type { Reaction } from '../state/GameContext';
import { SMOKE_EMOJI } from '../util/emoji';

/** An emoji that starts from a screen point inside the host (a card's gag). */
export interface Flight {
  key: string;
  emoji: string;
  x: number;
  y: number;
}

interface Props {
  reactions: Record<string, Reaction>;
  /** extra flights that do not come from an avatar */
  extra?: Flight[];
  /** the game container: flights are positioned inside it */
  host: RefObject<HTMLDivElement | null>;
  /** where the emoji fly to (CSS px inside host): the middle of the table */
  target: { x: number; y: number };
}

interface Fly extends Flight {
  dx: number;
  dy: number;
}

interface Smoke {
  key: string;
  x: number;
  y: number;
}

const FLIGHT_MS = 1100;
const SMOKE_MS = 6000;

/**
 * Emoji take off from the sender's avatar and sail to the middle of the
 * table, growing and dissolving on the way. The path is an arc without any
 * JS per frame: the outer element moves along X linearly while the inner
 * one moves along Y with an ease-out, and two different easings on two
 * axes trace a parabola. Opacity fades linearly in parallel.
 */
export function ReactionLayer({ reactions, extra = [], host, target }: Props) {
  const [flights, setFlights] = useState<Fly[]>([]);
  const [smokes, setSmokes] = useState<Smoke[]>([]);
  const seen = useRef(new Map<string, number>());
  const targetRef = useRef(target);
  targetRef.current = target;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const base = el.getBoundingClientRect();
    const fresh: Fly[] = [];
    const puffs: Smoke[] = [];
    for (const [playerId, r] of Object.entries(reactions)) {
      if (seen.current.get(playerId) === r.nonce) continue;
      seen.current.set(playerId, r.nonce);
      if (!r.emoji) continue;
      const avatar = el.querySelector<HTMLElement>(`[data-player="${CSS.escape(playerId)}"]`);
      const rect = avatar?.getBoundingClientRect();
      const x = rect ? rect.left + rect.width / 2 - base.left : base.width / 2;
      const y = rect ? rect.top + rect.height / 2 - base.top : base.height - 60;
      if (r.emoji === SMOKE_EMOJI) {
        // a cigarette is smoked by the avatar, it does not fly anywhere
        puffs.push({ key: `${playerId}:${r.nonce}`, x: x + (rect?.width ?? 40) * 0.35, y: y - (rect?.height ?? 40) * 0.2 });
        continue;
      }
      const t = targetRef.current;
      fresh.push({ key: `${playerId}:${r.nonce}`, emoji: r.emoji, x, y, dx: t.x - x, dy: t.y - y });
    }
    const timers: number[] = [];
    if (fresh.length) {
      setFlights((prev) => [...prev, ...fresh]);
      const keys = new Set(fresh.map((f) => f.key));
      timers.push(window.setTimeout(() => setFlights((prev) => prev.filter((f) => !keys.has(f.key))), FLIGHT_MS + 100));
    }
    if (puffs.length) {
      setSmokes((prev) => [...prev, ...puffs]);
      const keys = new Set(puffs.map((p) => p.key));
      timers.push(window.setTimeout(() => setSmokes((prev) => prev.filter((p) => !keys.has(p.key))), SMOKE_MS));
    }
    return () => timers.forEach((id) => window.clearTimeout(id));
  }, [reactions, host]);

  // a card's own emoji (a Joker laughing) starts where the card lies and sails up a little
  useEffect(() => {
    const fresh: Fly[] = [];
    for (const f of extra) {
      if (seen.current.has(f.key)) continue;
      seen.current.set(f.key, 1);
      fresh.push({ ...f, dx: (Math.random() - 0.5) * 60, dy: -90 });
    }
    if (fresh.length === 0) return;
    setFlights((prev) => [...prev, ...fresh]);
    const keys = new Set(fresh.map((f) => f.key));
    const id = window.setTimeout(() => setFlights((prev) => prev.filter((f) => !keys.has(f.key))), FLIGHT_MS + 100);
    return () => window.clearTimeout(id);
  }, [extra]);

  if (flights.length === 0 && smokes.length === 0) return null;
  return (
    <div className="fx-layer" aria-hidden="true">
      {flights.map((f) => (
        <span key={f.key} className="fly" style={{ left: f.x, top: f.y, ['--dx' as string]: `${f.dx}px`, ['--dy' as string]: `${f.dy}px` }}>
          <span className="fly__y">{f.emoji}</span>
        </span>
      ))}
      {smokes.map((s) => (
        <span key={s.key} className="smoke" style={{ left: s.x, top: s.y }}>
          <i className="smoke__cig" />
          <i className="smoke__puff" />
          <i className="smoke__puff" />
          <i className="smoke__puff" />
          <i className="smoke__puff" />
        </span>
      ))}
    </div>
  );
}
