import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { useGame } from '../state/GameContext';
import { playEffect } from '../util/sound';
import type { Effect } from '../util/emoji';

interface Burst {
  id: number;
  effect: Effect;
  seed: number;
}

interface Piece {
  key: number;
  cls: string;
  char: string;
  style: CSSProperties;
}

const TEXT: Partial<Record<Effect, string>> = { laugh: 'ХА-ХА-ХА', boom: 'БУМ!', fanfare: 'ПОБЕДА', rooster: 'КУ-КА-РЕ-КУ', clink: 'ДЗЫНЬ' };
const SHAKES: Effect[] = ['finger', 'laugh', 'boom', 'firecracker'];
const BURST_MS = 2600;

/** Deterministic pseudo-random numbers so a burst looks the same across re-renders. */
function rng(seed: number) {
  return (i: number) => {
    const x = Math.sin(seed * 9301 + i * 49297) * 233280;
    return x - Math.floor(x);
  };
}

function pieces(effect: Effect, seed: number): Piece[] {
  const rnd = rng(seed);
  switch (effect) {
    case 'cry':
      return Array.from({ length: 14 }, (_, i) => ({
        key: i,
        cls: 'fx-tear',
        char: '💧',
        style: { left: `${rnd(i) * 100}%`, animationDelay: `${rnd(i + 50) * 0.6}s`, fontSize: `${14 + rnd(i + 90) * 16}px` },
      }));
    case 'fanfare':
      return Array.from({ length: 42 }, (_, i) => ({
        key: i,
        cls: 'fx-confetti',
        char: '',
        style: {
          left: `${rnd(i) * 100}%`,
          background: ['#e6ff00', '#cf1fff', '#ece9e1', '#19e6ff'][i % 4],
          animationDelay: `${rnd(i + 30) * 0.8}s`,
          animationDuration: `${1.6 + rnd(i + 60)}s`,
        },
      }));
    case 'boom':
      return Array.from({ length: 24 }, (_, i) => {
        const angle = (i / 24) * Math.PI * 2;
        const dist = 90 + rnd(i) * 160;
        const vars = { '--dx': `${Math.cos(angle) * dist}px`, '--dy': `${Math.sin(angle) * dist}px` } as CSSProperties;
        return {
          key: i,
          cls: 'fx-spark',
          char: '',
          style: { left: `${48 + (rnd(i + 20) - 0.5) * 8}%`, top: `${45 + (rnd(i + 40) - 0.5) * 8}%`, animationDelay: `${rnd(i + 60) * 0.15}s`, ...vars },
        };
      });
    case 'shake': // a rain of hands
      return Array.from({ length: 16 }, (_, i) => ({
        key: i,
        cls: 'fx-tear',
        char: '🤝',
        style: { left: `${rnd(i) * 100}%`, animationDelay: `${rnd(i + 50) * 0.7}s`, fontSize: `${22 + rnd(i + 90) * 18}px` },
      }));
    case 'firecracker': // a flash and a burst of red sparks
      return Array.from({ length: 30 }, (_, i) => {
        const angle = rnd(i + 7) * Math.PI * 2;
        const dist = 60 + rnd(i) * 200;
        const vars = { '--dx': `${Math.cos(angle) * dist}px`, '--dy': `${Math.sin(angle) * dist}px` } as CSSProperties;
        return {
          key: i,
          cls: 'fx-spark fx-spark--red',
          char: '',
          style: { left: `${48 + (rnd(i + 20) - 0.5) * 6}%`, top: `${50 + (rnd(i + 40) - 0.5) * 6}%`, animationDelay: `${rnd(i + 60) * 0.3}s`, ...vars },
        };
      });
    case 'crown': // golden confetti
      return Array.from({ length: 42 }, (_, i) => ({
        key: i,
        cls: 'fx-confetti',
        char: '',
        style: {
          left: `${rnd(i) * 100}%`,
          background: ['#ffd700', '#ffb300', '#fff1a8', '#e6ff00'][i % 4],
          animationDelay: `${rnd(i + 30) * 0.8}s`,
          animationDuration: `${1.6 + rnd(i + 60)}s`,
        },
      }));
    case 'rooster':
      return Array.from({ length: 8 }, (_, i) => ({
        key: i,
        cls: 'fx-feather',
        char: '🪶',
        style: { left: `${10 + rnd(i) * 80}%`, animationDelay: `${rnd(i + 50) * 0.9}s`, fontSize: `${18 + rnd(i + 90) * 14}px` },
      }));
    case 'clink':
      return Array.from({ length: 10 }, (_, i) => ({
        key: i,
        cls: 'fx-bubble',
        char: '',
        style: { left: `${30 + rnd(i) * 40}%`, animationDelay: `${rnd(i + 50) * 1.2}s`, width: `${6 + rnd(i + 9) * 10}px`, height: `${6 + rnd(i + 9) * 10}px` },
      }));
    default:
      return [];
  }
}

/** Full-screen visuals (and the sound) of premium reactions: tears, a buzzer shake, confetti, a laugh, a boom. */
export function EffectLayer() {
  const { lastEffect } = useGame();
  const [bursts, setBursts] = useState<Burst[]>([]);
  const seen = useRef(0);
  const timers = useRef(new Set<number>());

  useEffect(() => {
    if (!lastEffect || lastEffect.nonce === seen.current) return;
    seen.current = lastEffect.nonce;
    const effect = lastEffect.effect;
    const burst: Burst = { id: lastEffect.nonce, effect, seed: lastEffect.nonce % 1000 };
    setBursts((prev) => [...prev.slice(-3), burst]);
    playEffect(effect);
    if (SHAKES.includes(effect)) {
      const cls = `fx-shake--${effect}`;
      document.body.classList.remove(cls); // restart the animation when it repeats
      void document.body.offsetWidth;
      document.body.classList.add(cls);
      const t = window.setTimeout(() => {
        document.body.classList.remove(cls);
        timers.current.delete(t);
      }, 800);
      timers.current.add(t);
    }
    const t = window.setTimeout(() => {
      setBursts((prev) => prev.filter((b) => b.id !== burst.id));
      timers.current.delete(t);
    }, BURST_MS);
    timers.current.add(t);
  }, [lastEffect]);

  useEffect(() => {
    const pending = timers.current;
    return () => {
      pending.forEach((t) => window.clearTimeout(t));
      pending.clear();
      SHAKES.forEach((e) => document.body.classList.remove(`fx-shake--${e}`));
    };
  }, []);

  if (bursts.length === 0) return null;
  return (
    <div className="effects-layer" aria-hidden="true">
      {bursts.map((b) => (
        <div key={b.id} className={`fx fx--${b.effect}`}>
          {b.effect === 'boom' || b.effect === 'firecracker' ? <div className="fx-flash" /> : null}
          {pieces(b.effect, b.seed).map((p) => (
            <span key={p.key} className={p.cls} style={p.style}>
              {p.char}
            </span>
          ))}
          {TEXT[b.effect] ? <div className="fx-text">{TEXT[b.effect]}</div> : null}
        </div>
      ))}
    </div>
  );
}
