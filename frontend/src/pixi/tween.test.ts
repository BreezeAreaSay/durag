import { describe, expect, it } from 'vitest';
import { StopMotionTweener, easings, fallDuration, type Animatable } from './tween';

function obj(): Animatable {
  const o = {
    position: { x: 0, y: 0, set(x: number, y?: number) { o.position.x = x; o.position.y = y ?? x; } },
    scale: { x: 1, y: 1, set(x: number, y?: number) { o.scale.x = x; o.scale.y = y ?? x; } },
    rotation: 0,
    alpha: 1,
  };
  return o;
}

describe('StopMotionTweener', () => {
  it('only moves on whole frames (12 fps = every 83.3ms)', () => {
    const tw = new StopMotionTweener(12);
    const o = obj();
    void tw.to(o, { x: 100 }, { duration: 1000, ease: easings.linear });
    tw.update(40);
    expect(o.position.x).toBe(0); // less than one frame: nothing drawn yet
    tw.update(50); // 90ms total -> one frame of 83.3ms
    expect(o.position.x).toBeCloseTo(8.33, 1);
    tw.update(10); // 16.6ms left over, still no new frame
    expect(o.position.x).toBeCloseTo(8.33, 1);
  });
  it('completes and resolves, cancelling an older tween on the same target', async () => {
    const tw = new StopMotionTweener(12);
    const o = obj();
    const first = tw.to(o, { x: 50 }, { duration: 200 });
    const second = tw.to(o, { x: 100, rotation: 1, scale: 2, alpha: 0.5 }, { duration: 200, ease: easings.linear });
    expect(await first).toBe(false);
    tw.update(1000);
    expect(await second).toBe(true);
    expect(o.position.x).toBe(100);
    expect(o.rotation).toBe(1);
    expect(o.scale.x).toBe(2);
    expect(o.alpha).toBe(0.5);
    expect(tw.active).toBe(0);
  });
  it('springy easings start at 0 and end at 1', () => {
    for (const e of [easings.outBack, easings.outElastic, easings.outQuad]) {
      expect(e(0)).toBeCloseTo(0, 5);
      expect(e(1)).toBeCloseTo(1, 5);
    }
    expect(easings.outBack(0.7)).toBeGreaterThan(1); // overshoot
  });
  it('honours delay', () => {
    const tw = new StopMotionTweener(10);
    const o = obj();
    void tw.to(o, { x: 10 }, { duration: 100, delay: 300, ease: easings.linear });
    tw.update(250);
    expect(o.position.x).toBe(0);
    tw.update(200);
    expect(o.position.x).toBeGreaterThan(0);
  });
});

describe('landing physics', () => {
  it('land() starts at 0, ends at 1 and overshoots once', () => {
    expect(easings.land(0)).toBe(0);
    expect(easings.land(1)).toBe(1);
    expect(easings.land(2 / 3)).toBeCloseTo(1.111, 2);
    expect(easings.land(1 / 3)).toBeCloseTo(1, 3); // crosses the slot
    expect(easings.land(0.9)).toBeGreaterThan(1); // still settling
    expect(easings.land(0.9)).toBeLessThan(1.02);
  });
  it('fallDuration grows with the square root of the distance, clamped', () => {
    expect(fallDuration(0)).toBe(180);
    expect(fallDuration(100)).toBe(250);
    expect(fallDuration(400)).toBe(340);
    expect(fallDuration(1e6)).toBe(460);
  });
});
