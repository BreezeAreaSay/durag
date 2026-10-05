// Stop-motion tween engine: values only change `fps` times per second, so
// movement reads as hand-animated frames rather than smooth interpolation.

export type Ease = (t: number) => number;

export const easings = {
  linear: ((t) => t) as Ease,
  outQuad: ((t) => 1 - (1 - t) * (1 - t)) as Ease,
  /** springy overshoot */
  outBack: ((t) => {
    const c1 = 1.70158;
    const c3 = c1 + 1;
    return 1 + c3 * Math.pow(t - 1, 3) + c1 * Math.pow(t - 1, 2);
  }) as Ease,
  /** rubber-band snap (used when the server rejects a card) */
  outElastic: ((t) => {
    if (t <= 0) return 0;
    if (t >= 1) return 1;
    const c4 = (2 * Math.PI) / 3;
    return Math.pow(2, -10 * t) * Math.sin((t * 10 - 0.75) * c4) + 1;
  }) as Ease,
};

/** The subset of a PixiJS Container the tweener touches. */
export interface Animatable {
  position: { x: number; y: number; set(x: number, y?: number): unknown };
  scale: { x: number; y: number; set(x: number, y?: number): unknown };
  rotation: number;
  alpha: number;
}

export interface TweenProps {
  x?: number;
  y?: number;
  rotation?: number;
  scale?: number;
  alpha?: number;
}

export interface TweenOptions {
  duration?: number;
  ease?: Ease;
  delay?: number;
}

interface Tween {
  target: Animatable;
  from: Required<TweenProps>;
  to: TweenProps;
  duration: number;
  delay: number;
  ease: Ease;
  elapsed: number;
  resolve: (completed: boolean) => void;
}

export class StopMotionTweener {
  private tweens: Tween[] = [];
  private acc = 0;

  constructor(public fps = 12) {}

  get frameMs(): number {
    return 1000 / this.fps;
  }

  /** Starts a tween; a running tween on the same target is cancelled. */
  to(target: Animatable, to: TweenProps, opts: TweenOptions = {}): Promise<boolean> {
    this.cancel(target);
    return new Promise((resolve) => {
      this.tweens.push({
        target,
        from: {
          x: target.position.x,
          y: target.position.y,
          rotation: target.rotation,
          scale: target.scale.x,
          alpha: target.alpha,
        },
        to,
        duration: Math.max(1, opts.duration ?? 320),
        delay: opts.delay ?? 0,
        ease: opts.ease ?? easings.outBack,
        elapsed: 0,
        resolve,
      });
    });
  }

  cancel(target: Animatable): void {
    const rest: Tween[] = [];
    for (const tw of this.tweens) {
      if (tw.target === target) tw.resolve(false);
      else rest.push(tw);
    }
    this.tweens = rest;
  }

  clear(): void {
    for (const tw of this.tweens) tw.resolve(false);
    this.tweens = [];
  }

  get active(): number {
    return this.tweens.length;
  }

  /** Advances time; properties are only written on whole animation frames. */
  update(deltaMs: number): void {
    this.acc += deltaMs;
    let frames = 0;
    while (this.acc >= this.frameMs) {
      this.acc -= this.frameMs;
      frames++;
    }
    if (frames === 0 || this.tweens.length === 0) return;
    const advance = frames * this.frameMs;
    const done: Tween[] = [];
    for (const tw of this.tweens) {
      tw.elapsed += advance;
      const local = tw.elapsed - tw.delay;
      if (local < 0) continue;
      const t = Math.min(1, local / tw.duration);
      const e = tw.ease(t);
      const x = tw.to.x !== undefined ? lerp(tw.from.x, tw.to.x, e) : tw.target.position.x;
      const y = tw.to.y !== undefined ? lerp(tw.from.y, tw.to.y, e) : tw.target.position.y;
      tw.target.position.set(x, y);
      if (tw.to.rotation !== undefined) tw.target.rotation = lerp(tw.from.rotation, tw.to.rotation, e);
      if (tw.to.scale !== undefined) {
        const s = lerp(tw.from.scale, tw.to.scale, e);
        tw.target.scale.set(s, s);
      }
      if (tw.to.alpha !== undefined) tw.target.alpha = lerp(tw.from.alpha, tw.to.alpha, e);
      if (t >= 1) done.push(tw);
    }
    if (done.length) {
      this.tweens = this.tweens.filter((tw) => !done.includes(tw));
      for (const tw of done) tw.resolve(true);
    }
  }
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}
