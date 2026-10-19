// A lamp over the table. One light source, three motions added together,
// every one of them a closed-form function of time: nothing to simulate.
//
//   drift   a Lissajous figure with incommensurable frequencies
//           (ω and 1.3ω): the light never visibly repeats its path;
//   kick    a damped pendulum θ(t) = A·e^(−λt)·cos(ω_p·t) after a fist
//           on the table: the lamp swings and the shadows swing with it;
//   look    a pull towards a point (a card that was just beaten) that rises
//           in 150 ms and lets go over half a second: the light glances.
//
// Positions are fractions of the viewport (0..1) so the same lamp drives
// the Pixi shadows and the CSS light spot.

export interface Point {
  x: number;
  y: number;
}

const DRIFT_PERIOD_S = 60;
const KICK_LAMBDA = 1.2; // 1/s
const KICK_PERIOD_S = 1.1;
const LOOK_RISE_MS = 150;
const LOOK_HOLD_MS = 300;
const LOOK_TAU_MS = 500;

export class Lamp {
  private t = 0; // ms
  private readonly phase: number;
  private kickAt = -Infinity;
  private kickAmp = 0;
  private lookAt = -Infinity;
  private lookTarget: Point = { x: 0.5, y: 0.4 };

  constructor(seed = Math.random()) {
    this.phase = seed * Math.PI * 2;
  }

  /** advance the clock */
  update(dtMs: number): void {
    this.t += dtMs;
  }

  /** a fist on the table: the lamp swings, strength 1 = a quarter of the width */
  kick(strength = 1): void {
    this.kickAt = this.t;
    this.kickAmp = 0.25 * strength;
  }

  /** glance at a point (viewport fractions) */
  look(p: Point): void {
    this.lookAt = this.t;
    this.lookTarget = p;
  }

  /** where the lamp hangs now, in viewport fractions */
  position(): Point {
    const sec = this.t / 1000;
    const w = (Math.PI * 2) / DRIFT_PERIOD_S;
    let x = 0.5 + 0.35 * Math.cos(w * sec + this.phase);
    let y = 0.35 + 0.22 * Math.sin(1.3 * w * sec + this.phase);
    const k = (this.t - this.kickAt) / 1000;
    if (k >= 0 && k < 6) {
      const env = this.kickAmp * Math.exp(-KICK_LAMBDA * k);
      x += env * Math.cos(((Math.PI * 2) / KICK_PERIOD_S) * k);
      y += env * 0.4 * Math.sin(((Math.PI * 2) / KICK_PERIOD_S) * k);
    }
    const g = this.lookWeight();
    if (g > 0) {
      x += (this.lookTarget.x - x) * g;
      y += (this.lookTarget.y - y) * g;
    }
    return { x, y };
  }

  /** how far the lamp is pulled towards the glance target, 0..0.6 */
  lookWeight(): number {
    const l = this.t - this.lookAt;
    if (l < 0 || l > 3000) return 0;
    const rise = Math.min(1, l / LOOK_RISE_MS);
    const decay = Math.exp(-Math.max(0, l - LOOK_RISE_MS - LOOK_HOLD_MS) / LOOK_TAU_MS);
    return 0.6 * rise * decay;
  }

  /**
   * The shadow of an object at p (pixels) in a viewport of w×h: it falls
   * away from the lamp, longer the further from the light and when lifted.
   * A second light on the table (the Super card) takes over nearby: its
   * weight is e^(−d/R), so a card right next to it is lit by it alone and
   * one across the table by the lamp alone.
   */
  shadow(p: Point, w: number, h: number, lift = 0, base = 7, ember?: Point, reach = 160): Point {
    const l = this.position();
    const dx = p.x - l.x * w;
    const dy = p.y - l.y * h;
    const d = Math.hypot(dx, dy) || 1;
    const len = base * (1 + lift * 1.1) * (0.8 + 0.6 * Math.min(1, d / w));
    const out = { x: (dx / d) * len, y: (dy / d) * len + base * 0.25 };
    if (!ember) return out;
    const ex = p.x - ember.x;
    const ey = p.y - ember.y;
    const ed = Math.hypot(ex, ey);
    if (ed < 1) return out;
    const k = Math.exp(-ed / reach);
    const elen = base * (1 + lift * 1.1) * 1.3;
    return { x: out.x * (1 - k) + (ex / ed) * elen * k, y: out.y * (1 - k) + (ey / ed) * elen * k };
  }
}

/**
 * The lamp's colour by the hour on the phone: white in the afternoon,
 * yellow at night. night = (1 − cos(π·(hour − 14)/12)) / 2 peaks at 2 am.
 */
export function lampTint(hour: number): { hue: number; sat: number; alpha: number } {
  const night = (1 - Math.cos((Math.PI * (hour - 14)) / 12)) / 2;
  return { hue: 48 - 8 * night, sat: 30 + 50 * night, alpha: 0.22 + 0.18 * night };
}
