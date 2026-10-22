import { describe, expect, it } from 'vitest';
import { Lamp, lampTint } from './lamp';

describe('lamp', () => {
  it('drifts inside the viewport and never stands still', () => {
    const lamp = new Lamp(0.3);
    const seen = new Set<string>();
    for (let i = 0; i < 600; i++) {
      lamp.update(100);
      const p = lamp.position();
      expect(p.x).toBeGreaterThan(0);
      expect(p.x).toBeLessThan(1);
      expect(p.y).toBeGreaterThan(0);
      expect(p.y).toBeLessThan(1);
      seen.add(`${p.x.toFixed(2)},${p.y.toFixed(2)}`);
    }
    expect(seen.size).toBeGreaterThan(150);
  });

  it('swings after a kick and settles within a few seconds', () => {
    const lamp = new Lamp(0);
    const rest = lamp.position();
    lamp.kick();
    const swung = lamp.position();
    expect(Math.abs(swung.x - rest.x)).toBeGreaterThan(0.2);
    for (let i = 0; i < 60; i++) lamp.update(100);
    const quiet = new Lamp(0);
    for (let i = 0; i < 60; i++) quiet.update(100);
    expect(Math.abs(lamp.position().x - quiet.position().x)).toBeLessThan(0.01);
  });

  it('glances at a point and lets go', () => {
    const lamp = new Lamp(0);
    lamp.look({ x: 0.9, y: 0.9 });
    expect(lamp.lookWeight()).toBe(0);
    lamp.update(150);
    expect(lamp.lookWeight()).toBeCloseTo(0.6, 5);
    lamp.update(2000);
    expect(lamp.lookWeight()).toBeLessThan(0.05);
  });

  it('casts shadows away from the light, longer when lifted', () => {
    const lamp = new Lamp(0);
    const l = lamp.position();
    const p = { x: l.x * 390 + 100, y: l.y * 844 + 100 };
    const flat = lamp.shadow(p, 390, 844, 0);
    const up = lamp.shadow(p, 390, 844, 1);
    expect(flat.x).toBeGreaterThan(0);
    expect(flat.y).toBeGreaterThan(0);
    expect(Math.hypot(up.x, up.y)).toBeGreaterThan(Math.hypot(flat.x, flat.y));
  });

  it('lets a second light take over next to it', () => {
    const lamp = new Lamp(0);
    const ember = { x: 200, y: 400 };
    const near = lamp.shadow({ x: 230, y: 400 }, 390, 844, 0, 7, ember);
    const far = lamp.shadow({ x: 230, y: 400 }, 390, 844, 0, 7);
    expect(near.x).toBeGreaterThan(far.x - 1); // pushed to the right, away from the ember
    expect(near.x).toBeGreaterThan(5);
    const away = lamp.shadow({ x: 30, y: 60 }, 390, 844, 0, 7, ember);
    const plain = lamp.shadow({ x: 30, y: 60 }, 390, 844, 0, 7);
    expect(Math.abs(away.x - plain.x)).toBeLessThan(0.5); // too far: the lamp alone
  });

  it('flickers in short bursts and is steady in between', () => {
    const lamp = new Lamp(0);
    expect(lamp.intensity()).toBe(1);
    lamp.flicker();
    lamp.update(1);
    let dimmed = 0;
    for (let i = 0; i < 80; i++) {
      lamp.update(10);
      const v = lamp.intensity();
      expect(v).toBeGreaterThanOrEqual(0.3);
      expect(v).toBeLessThanOrEqual(1);
      if (v < 0.95) dimmed++;
    }
    expect(dimmed).toBeGreaterThan(5);
    lamp.update(2000);
    expect(lamp.intensity()).toBe(1);
  });

  it('is yellow at night and white by day', () => {
    expect(lampTint(2).sat).toBeGreaterThan(lampTint(14).sat);
    expect(lampTint(2).alpha).toBeGreaterThan(lampTint(14).alpha);
  });
});
