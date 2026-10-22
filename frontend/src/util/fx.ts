// Effects mode: full, lite, or auto. Auto starts full and lets the table
// judge the phone: when frames stay slow for a while, the verdict "lite" is
// stored and the heavy layers (lamp, shadows, camera, paper grain) go away,
// this launch and the next ones. The mode lives on <html> as data-fx so CSS
// can follow, and a window event tells the live scene.

export type FxMode = 'auto' | 'full' | 'lite';
export type FxLevel = 'full' | 'lite';

const KEY = 'durag.fx';
const VERDICT_KEY = 'durag.fx.verdict';
export const FX_EVENT = 'durag:fx';

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // private mode: the choice lives for the session
  }
}

export function loadFx(): FxMode {
  const v = read(KEY);
  return v === 'full' || v === 'lite' ? v : 'auto';
}

/** what the table judged the phone to be, if it has judged yet */
export function verdict(): FxLevel | null {
  return read(VERDICT_KEY) === 'lite' ? 'lite' : null;
}

/** the level that actually runs */
export function effectiveFx(mode = loadFx()): FxLevel {
  if (mode === 'auto') return verdict() ?? 'full';
  return mode;
}

function broadcast(): void {
  document.documentElement.dataset.fx = effectiveFx();
  window.dispatchEvent(new CustomEvent(FX_EVENT, { detail: effectiveFx() }));
}

export function applyFx(mode: FxMode): void {
  write(KEY, mode === 'auto' ? null : mode);
  broadcast();
}

/** the table's judgement: the phone cannot keep up */
export function judgeLite(): void {
  write(VERDICT_KEY, 'lite');
  if (loadFx() === 'auto') broadcast();
}

export function isLite(): boolean {
  return document.documentElement.dataset.fx === 'lite';
}

/**
 * Decides whether a phone keeps up, from frame times. An exponential
 * moving average of the frame time (α = 0.05, so about the last 20
 * frames) is compared with a threshold; the verdict comes only after a
 * warm-up (the deal animation is heavy on purpose) and only when the
 * average stays above the threshold for `holdMs` in a row. A single slow
 * frame — a GC pause, a tab switch — never flips it.
 */
export class FrameJudge {
  private avg = 0;
  private t = 0;
  private slowSince = -1;
  private done = false;

  constructor(
    private readonly thresholdMs = 30, // below ~33 fps
    private readonly warmupMs = 4000,
    private readonly holdMs = 3000,
  ) {}

  /** feed one frame; returns true once, the moment the phone is judged slow */
  feed(dtMs: number): boolean {
    if (this.done) return false;
    this.t += dtMs;
    const dt = Math.min(dtMs, 100); // a hidden tab is not a slow phone
    this.avg = this.avg === 0 ? dt : this.avg + 0.05 * (dt - this.avg);
    if (this.t < this.warmupMs) return false;
    if (this.avg > this.thresholdMs) {
      if (this.slowSince < 0) this.slowSince = this.t;
      if (this.t - this.slowSince >= this.holdMs) {
        this.done = true;
        return true;
      }
    } else {
      this.slowSince = -1;
    }
    return false;
  }
}
