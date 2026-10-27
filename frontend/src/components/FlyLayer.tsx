import { useEffect, useMemo, useRef, useState, type CSSProperties, type RefObject } from 'react';
import { useGame } from '../state/GameContext';
import { sound } from '../util/audio';
import { haptic } from '../telegram/useTelegram';
import { t } from '../i18n';

interface Props {
  host: RefObject<HTMLDivElement | null>;
  /** the band of the screen the fly crosses (CSS px inside host) */
  top: number;
  bottom: number;
}

const FLIGHT_MS = 7000;
const POINTS = 9;

/** A noisy path through the table band, the same on every phone for one seed. */
function path(seed: number, w: number, top: number, bottom: number): { x: number; y: number }[] {
  let s = seed || 1;
  const rnd = () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
  const pts: { x: number; y: number }[] = [];
  const fromLeft = rnd() < 0.5;
  for (let i = 0; i < POINTS; i++) {
    const u = i / (POINTS - 1);
    const x = (fromLeft ? u : 1 - u) * (w + 80) - 40 + (rnd() - 0.5) * 60;
    const y = top + (bottom - top) * (0.15 + rnd() * 0.7);
    pts.push({ x, y });
  }
  return pts;
}

/**
 * The fly the server launched: 🪰 zigzags across the table for seven
 * seconds, buzzing. A tap is a slap: the first one in the room earns a
 * point of experience, which the server confirms with the news of the catch.
 */
export function FlyLayer({ host, top, bottom }: Props) {
  const { fly, catchFly, selfId, state } = useGame();
  const [slapped, setSlapped] = useState<string | null>(null);
  const buzzed = useRef<string | null>(null);
  const width = host.current?.clientWidth ?? window.innerWidth;
  const pts = useMemo(() => (fly ? path(fly.seed, width, top, bottom) : []), [fly, width, top, bottom]);

  useEffect(() => {
    if (!fly || fly.caughtBy || buzzed.current === fly.id) return;
    buzzed.current = fly.id;
    sound.buzz(FLIGHT_MS / 1000);
  }, [fly]);

  if (!fly) return null;
  const caught = fly.caughtBy;
  const winner = caught ? state?.players.find((p) => p.id === caught) : null;
  const style = {
    '--fly-ms': `${FLIGHT_MS}ms`,
    offsetPath: `path("M ${pts.map((p) => `${p.x.toFixed(0)} ${p.y.toFixed(0)}`).join(' L ')}")`,
  } as CSSProperties;
  return (
    <div className="bug-layer" aria-hidden={caught ? undefined : true}>
      {caught ? (
        <div className="bug-news" style={{ top: (top + bottom) / 2 }} key={fly.nonce}>
          {caught === selfId ? t('fly.you') : t('fly.caught', { name: winner?.name ?? '?' })}
        </div>
      ) : (
        <button
          type="button"
          className={`bug ${slapped === fly.id ? 'bug--slapped' : ''}`}
          style={style}
          aria-label="fly"
          onPointerDown={(e) => {
            e.preventDefault();
            if (slapped === fly.id) return;
            setSlapped(fly.id);
            sound.slap();
            haptic('medium');
            catchFly(fly.id);
          }}
        >
          🪰
        </button>
      )}
    </div>
  );
}
