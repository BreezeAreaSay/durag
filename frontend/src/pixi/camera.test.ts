import { describe, expect, it } from 'vitest';
import { Camera } from './camera';

const none = () => null;

describe('camera', () => {
  it('sways by a few pixels and never holds still', () => {
    const cam = new Camera(0.4);
    const seen = new Set<string>();
    for (let i = 0; i < 300; i++) {
      cam.update(50, none, true);
      const f = cam.frame();
      expect(Math.abs(f.x)).toBeLessThan(4);
      expect(Math.abs(f.y)).toBeLessThan(4);
      expect(Math.abs(f.rotation)).toBeLessThan(0.004);
      seen.add(`${f.x.toFixed(1)},${f.y.toFixed(1)}`);
    }
    expect(seen.size).toBeGreaterThan(40);
  });

  it('peers at a point and comes back', () => {
    const cam = new Camera(0);
    cam.peer({ x: 100, y: 200 });
    cam.update(900, none, true);
    const f = cam.frame();
    expect(f.scale).toBeGreaterThan(1.02);
    expect(f.pivot).toEqual({ x: 100, y: 200 });
    cam.update(650 + 900, none, true);
    expect(cam.frame().scale).toBeCloseTo(1, 5);
  });

  it('releases a peer smoothly when a drag starts', () => {
    const cam = new Camera(0);
    cam.peer({ x: 0, y: 0 });
    cam.update(450, none, true);
    const before = cam.frame().scale;
    cam.release();
    expect(cam.frame().scale).toBeCloseTo(before, 5);
    cam.update(1000, none, true);
    expect(cam.frame().scale).toBeCloseTo(1, 5);
  });

  it('asks where to look only when not busy', () => {
    const cam = new Camera(0);
    let asked = 0;
    for (let i = 0; i < 2000; i++) cam.update(100, () => (asked++, { x: 1, y: 1 }), true);
    expect(asked).toBe(0);
    for (let i = 0; i < 2000; i++) cam.update(100, () => (asked++, { x: 1, y: 1 }), false);
    expect(asked).toBeGreaterThan(0);
  });
});
