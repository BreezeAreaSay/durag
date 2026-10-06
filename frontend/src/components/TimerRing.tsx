import { useEffect, useRef } from 'react';

interface Props {
  /** unix ms when the timer fires */
  deadline: number;
  /** full length of the timer in ms */
  timeoutMs: number;
  size: number;
}

/**
 * Grainy, hatched circular progress around an avatar. Driven by the
 * server deadline so every client shows the same remaining time.
 */
export function TimerRing({ deadline, timeoutMs, size }: Props) {
  const ref = useRef<SVGCircleElement>(null);
  const r = size / 2 - 3;
  const c = 2 * Math.PI * r;

  useEffect(() => {
    let raf = 0;
    const tick = () => {
      const el = ref.current;
      if (el) {
        const remaining = Math.max(0, deadline - Date.now());
        const frac = timeoutMs > 0 ? Math.min(1, remaining / timeoutMs) : 0;
        // stop-motion feel: quantise to 24 steps
        const q = Math.ceil(frac * 24) / 24;
        el.setAttribute('stroke-dasharray', `${c * q} ${c}`);
        el.classList.toggle('timer-ring__arc--hot', frac < 0.25);
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [deadline, timeoutMs, c]);

  const id = `hatch-${size}`;
  return (
    <svg className="timer-ring" width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
      <defs>
        <pattern id={id} patternUnits="userSpaceOnUse" width="4" height="4" patternTransform="rotate(45)">
          <rect width="4" height="4" fill="transparent" />
          <rect width="2" height="4" fill="currentColor" />
        </pattern>
      </defs>
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="currentColor" strokeOpacity="0.18" strokeWidth="5" strokeDasharray="2 3" />
      <circle
        ref={ref}
        className="timer-ring__arc"
        cx={size / 2}
        cy={size / 2}
        r={r}
        fill="none"
        stroke={`url(#${id})`}
        strokeWidth="6"
        strokeLinecap="butt"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
      />
    </svg>
  );
}
