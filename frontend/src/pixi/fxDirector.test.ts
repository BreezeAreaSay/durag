import { describe, expect, it } from 'vitest';
import { FxDirector } from './fxDirector';

const DT = 1000 / 12;
const run = (d: FxDirector, ms: number) => {
  for (let t = 0; t < ms; t += DT) d.step(DT);
};

describe('FxDirector', () => {
  it('stays silent during ordinary play', () => {
    const d = new FxDirector(() => 1); // first flicker only after 8 s
    run(d, 5000);
    expect(d.level).toBe(0);
    expect(d.glitch).toBe(0);
  });

  it('ramps up on a hit and fades back to zero', () => {
    const d = new FxDirector(() => 1);
    d.hit(1, 1000);
    d.step(DT);
    expect(d.level).toBeGreaterThan(0);
    run(d, 500);
    expect(d.level).toBeGreaterThan(0.3);
    run(d, 2000);
    expect(d.level).toBe(0);
  });

  it('holds an ambient level and lets a stronger hit win', () => {
    const d = new FxDirector(() => 1);
    d.setAmbient(0.3);
    run(d, 1000);
    expect(d.level).toBe(0.3);
    d.hit(0.1, 500); // weaker than the ambient: nothing visible happens
    run(d, 200);
    expect(d.level).toBe(0.3);
    d.setAmbient(0);
    run(d, 1000);
    expect(d.level).toBe(0);
  });

  it('flickers the glitch pass rarely and briefly', () => {
    const d = new FxDirector(() => 0); // flicker every 3.5 s
    run(d, 3400);
    expect(d.glitch).toBe(0);
    run(d, 200);
    expect(d.glitch).toBe(0.5);
    run(d, 400);
    expect(d.glitch).toBe(0);
  });

  it('reports changes so the scene only touches filters when needed', () => {
    const d = new FxDirector(() => 1);
    expect(d.step(DT)).toBe(false);
    d.hitGlitch(1, 500);
    expect(d.step(DT)).toBe(true);
  });
});
