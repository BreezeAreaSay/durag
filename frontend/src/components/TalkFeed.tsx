import { useEffect, useRef, useState } from 'react';
import type { Reaction } from '../state/GameContext';
import type { Player } from '../types/protocol';
import { TALK_EVENT, talkOn } from '../util/talk';

interface Props {
  reactions: Record<string, Reaction>;
  players: Player[];
  /** y (CSS px) where the feed starts: under the opponents; without it the feed sits in the flow */
  top?: number;
}

interface Line {
  key: number;
  playerId: string;
  text: string;
}

const LINE_MS = 6000;
const MAX_LINES = 2;

/**
 * What is said at the table, in one feed under the opponents: full width,
 * no clipping, the newest line on top and the older one sliding down. Two
 * bots talking at once no longer draw over each other.
 */
export function TalkFeed({ reactions, players, top }: Props) {
  const [lines, setLines] = useState<Line[]>([]);
  const [on, setOn] = useState(talkOn);
  useEffect(() => {
    const follow = (e: Event) => setOn(Boolean((e as CustomEvent<boolean>).detail));
    window.addEventListener(TALK_EVENT, follow);
    return () => window.removeEventListener(TALK_EVENT, follow);
  }, []);
  const seen = useRef(new Map<string, number>());
  const timers = useRef(new Set<number>());

  useEffect(() => {
    const fresh: Line[] = [];
    for (const [playerId, r] of Object.entries(reactions)) {
      if (!r.text || seen.current.get(playerId) === r.nonce) continue;
      seen.current.set(playerId, r.nonce);
      fresh.push({ key: r.nonce, playerId, text: r.text });
    }
    if (fresh.length === 0) return;
    setLines((prev) => [...fresh.reverse(), ...prev].slice(0, MAX_LINES));
    const keys = new Set(fresh.map((l) => l.key));
    const id = window.setTimeout(() => {
      timers.current.delete(id);
      setLines((prev) => prev.filter((l) => !keys.has(l.key)));
    }, LINE_MS);
    timers.current.add(id);
  }, [reactions]);

  useEffect(() => {
    const pending = timers.current;
    return () => {
      pending.forEach((id) => window.clearTimeout(id));
      pending.clear();
    };
  }, []);

  if (!on || lines.length === 0) return null;
  return (
    <ol className={`talk ${top === undefined ? 'talk--static' : ''}`} aria-live="polite" style={top === undefined ? undefined : { top }}>
      {lines.map((l) => {
        const p = players.find((x) => x.id === l.playerId);
        return (
          <li key={l.key} className="talk__line">
            <span className="talk__who" aria-hidden="true">
              {p?.avatar ?? (p?.name?.[0] ?? '?').toUpperCase()}
            </span>
            <span className="talk__name">{p?.name ?? '?'}</span>
            <span className="talk__text">{l.text}</span>
          </li>
        );
      })}
    </ol>
  );
}
