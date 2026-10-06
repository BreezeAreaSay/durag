// Event-driven intensity for the shader passes.
//
// The effects are silent while people play (nothing flickers while you are
// defending) and light up only for a moment: the bout stamp, the trump reveal,
// the Super card blowing a card apart, picking up the stump. Levels move in
// discrete stop-motion steps, like everything else on the table.

const STEP = 0.2; // how far a level may move per stop-motion step
const FLICKER_MS = 170; // a rare two-frame glitch on trump cards
const FLICKER_LEVEL = 0.5;

function clamp(v: number): number {
  return Math.min(1, Math.max(0, v));
}

function round(v: number): number {
  return Math.round(v * 100) / 100;
}

export class FxDirector {
  /** 0..1 strength of the world passes (halftone + chromatic aberration). */
  level = 0;
  /** 0..1 strength of the glitch pass on trump cards and the trump plate. */
  glitch = 0;

  private ambient = 0;
  private burst = 0;
  private burstDecay = 0; // per ms
  private glitchBurst = 0;
  private glitchDecay = 0;
  private flickerIn = 0;
  private flickerLeft = 0;

  constructor(private readonly rng: () => number = Math.random) {
    this.scheduleFlicker();
  }

  /** A quiet background level (used for the bout pause and the stump step). */
  setAmbient(v: number): void {
    this.ambient = clamp(v);
  }

  /** A moment: the world passes jump to `strength` and fade out over `durationMs`. */
  hit(strength: number, durationMs: number): void {
    const s = clamp(strength);
    if (s < this.burst) return;
    this.burst = s;
    this.burstDecay = s / Math.max(1, durationMs);
  }

  /** A moment for the glitch pass (a trump card lands, the trump is revealed). */
  hitGlitch(strength: number, durationMs: number): void {
    const s = clamp(strength);
    if (s < this.glitchBurst) return;
    this.glitchBurst = s;
    this.glitchDecay = s / Math.max(1, durationMs);
  }

  /** Advances one stop-motion step of `dt` ms. Returns true when a level changed. */
  step(dt: number): boolean {
    const prevLevel = this.level;
    const prevGlitch = this.glitch;

    this.burst = Math.max(0, this.burst - this.burstDecay * dt);
    this.glitchBurst = Math.max(0, this.glitchBurst - this.glitchDecay * dt);

    this.flickerIn -= dt;
    if (this.flickerIn <= 0) {
      this.flickerLeft = FLICKER_MS;
      this.scheduleFlicker();
    } else {
      this.flickerLeft = Math.max(0, this.flickerLeft - dt);
    }

    const target = Math.max(this.ambient, this.burst);
    const diff = target - this.level;
    this.level = round(this.level + Math.sign(diff) * Math.min(Math.abs(diff), STEP));
    this.glitch = round(Math.max(this.glitchBurst, this.flickerLeft > 0 ? FLICKER_LEVEL : 0));

    return prevLevel !== this.level || prevGlitch !== this.glitch;
  }

  private scheduleFlicker(): void {
    this.flickerIn = 3500 + this.rng() * 4500;
  }
}
