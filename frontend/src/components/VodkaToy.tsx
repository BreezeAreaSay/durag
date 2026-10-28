import { useEffect, useRef, useState } from 'react';
import { sound } from '../util/audio';
import { haptic } from '../telegram/useTelegram';
import { t } from '../i18n';

const W = 300;
const H = 190;
const FLOOR = 160;
const GRAVITY = 1700; // px/s²
const BREAK_SPEED = 820; // px/s on impact
const BOTTLE_W = 34;
const BOTTLE_H = 112;
const GLASS_W = 46;
const GLASS_H = 54;
const GLASS_MAX = 100;

interface Body {
  x: number; // the base's centre
  y: number; // the base's y
  vx: number;
  vy: number;
  angle: number; // radians, 0 upright
  va: number;
  broken: boolean;
}

interface Drop {
  x: number;
  y: number;
  vx: number;
  vy: number;
}

interface Shard {
  x: number;
  y: number;
  vx: number;
  vy: number;
  a: number;
  va: number;
  s: number;
  color: string;
  life: number;
}

interface Puddle {
  x: number;
  r: number;
}

interface Grab {
  body: Body;
  dx: number;
  dy: number;
  lastX: number;
  lastY: number;
  lastT: number;
  moved: boolean;
}

/**
 * The winner's bottle and glass on a little canvas with real physics:
 * both can be dragged and thrown, they fall, bounce and break when they
 * land hard; bring the bottle up over the glass and it tilts and pours a
 * stream of drops that either land in the glass or on the table as a
 * puddle. Tap the glass to drink it. A double tap on the bare table
 * brings a fresh bottle and glass. Everything stays on this phone.
 */
export function VodkaToy({ compact = false }: { compact?: boolean }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [drinks, setDrinks] = useState(0);
  const [note, setNote] = useState<'' | 'broken'>('');
  const world = useRef({
    bottle: { x: 215, y: FLOOR, vx: 0, vy: 0, angle: 0, va: 0, broken: false } as Body,
    glass: { x: 75, y: FLOOR, vx: 0, vy: 0, angle: 0, va: 0, broken: false } as Body,
    fill: 100,
    level: 0,
    drops: [] as Drop[],
    shards: [] as Shard[],
    puddles: [] as Puddle[],
    grab: null as Grab | null,
    pouring: false,
    lastTap: 0,
  });

  useEffect(() => {
    const el = canvas.current;
    if (!el) return;
    const ctx = el.getContext('2d');
    if (!ctx) return;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    el.width = W * dpr;
    el.height = H * dpr;
    ctx.scale(dpr, dpr);
    let raf = 0;
    let last = performance.now();
    const w = world.current;

    // canvas rotation is clockwise: the mouth (0, -H) lands at (H·sin a, -H·cos a)
    const mouth = (b: Body) => ({ x: b.x + Math.sin(b.angle) * BOTTLE_H, y: b.y - Math.cos(b.angle) * BOTTLE_H });

    const shatter = (b: Body, color: string, amount: number) => {
      b.broken = true;
      for (let i = 0; i < 12; i++) {
        w.shards.push({ x: b.x + (Math.random() - 0.5) * 20, y: b.y - Math.random() * 40, vx: (Math.random() - 0.5) * 500, vy: -Math.random() * 500, a: Math.random() * 6, va: (Math.random() - 0.5) * 20, s: 3 + Math.random() * 6, color, life: 1 });
      }
      if (amount > 0) w.puddles.push({ x: b.x, r: 8 + amount * 0.4 });
      sound.crash();
      haptic('heavy');
      setNote('broken');
    };

    const settle = (b: Body, dt: number) => {
      if (w.grab?.body === b) return;
      b.vy += GRAVITY * dt;
      b.x += b.vx * dt;
      b.y += b.vy * dt;
      b.angle += b.va * dt;
      if (b.x < 20) {
        b.x = 20;
        b.vx = Math.abs(b.vx) * 0.5;
      }
      if (b.x > W - 20) {
        b.x = W - 20;
        b.vx = -Math.abs(b.vx) * 0.5;
      }
      if (b.y >= FLOOR) {
        const speed = Math.hypot(b.vx, b.vy);
        b.y = FLOOR;
        if (!b.broken && speed > BREAK_SPEED) {
          shatter(b, b === w.bottle ? '#3f6b43' : '#cfe3f5', b === w.bottle ? w.fill : w.level);
          if (b === w.bottle) w.fill = 0;
          else w.level = 0;
        }
        b.vy = -b.vy * 0.25;
        b.vx *= 0.7;
        if (Math.abs(b.vy) < 60) b.vy = 0;
        // on the table it stands up again, slowly, unless it is in pieces
        b.va = 0;
        b.angle += (0 - b.angle) * Math.min(1, dt * 8);
      }
    };

    const step = (now: number) => {
      const dt = Math.min(0.033, (now - last) / 1000);
      last = now;
      const { bottle, glass } = w;
      settle(bottle, dt);
      settle(glass, dt);
      // a thrown bottle smashes into the glass
      if (!glass.broken && !bottle.broken && Math.abs(bottle.x - glass.x) < 30 && Math.abs(bottle.y - glass.y) < 60 && Math.hypot(bottle.vx, bottle.vy) > BREAK_SPEED * 0.8 && w.grab?.body !== bottle) {
        shatter(glass, '#cfe3f5', w.level);
        w.level = 0;
      }
      // the bottle in the hand: upright, unless it is held up beside the glass;
      // then it leans so that the mouth comes over the glass (the further to
      // the side, the steeper), and past the pouring angle the drops go
      if (w.grab?.body === bottle && !bottle.broken) {
        const dx = glass.x - bottle.x;
        const beside = !glass.broken && Math.abs(dx) < BOTTLE_H + 40 && bottle.y < glass.y - 10;
        let target = 0;
        if (beside) target = Math.abs(dx) < BOTTLE_H ? Math.asin(dx / BOTTLE_H) : Math.sign(dx) * 1.9;
        bottle.angle += (target - bottle.angle) * Math.min(1, dt * 7);
      }
      // a full bottle pours at a slight tilt, a nearly empty one has to go horizontal
      const pourFrom = 0.35 + (1 - w.fill / 100) * 1.3;
      const lean = Math.abs(bottle.angle) - pourFrom;
      const pouring = !bottle.broken && lean > 0 && w.fill > 0;
      if (pouring && !w.pouring) sound.pour();
      w.pouring = pouring;
      if (pouring) {
        const m = mouth(bottle);
        const n = lean > 0.5 ? 2 : 1;
        for (let i = 0; i < n; i++) w.drops.push({ x: m.x, y: m.y, vx: Math.sin(bottle.angle) * 20 + (Math.random() - 0.5) * 20, vy: 10 + Math.random() * 40 });
        w.fill = Math.max(0, w.fill - 0.18 * n);
      }
      // drops fall; into the glass, or onto the table
      const rimY = glass.y - GLASS_H;
      w.drops = w.drops.filter((d) => {
        d.vy += GRAVITY * dt;
        d.x += d.vx * dt;
        d.y += d.vy * dt;
        if (!glass.broken && d.y >= rimY && d.y < rimY + 12 && Math.abs(d.x - glass.x) < GLASS_W / 2 - 3 && Math.abs(glass.angle) < 0.5) {
          if (w.level < GLASS_MAX) {
            w.level += 1.3;
            return false;
          }
          d.vx += (d.x < glass.x ? -1 : 1) * 80; // it runs over the rim
        }
        if (d.y >= FLOOR) {
          const p = w.puddles.find((q) => Math.abs(q.x - d.x) < q.r + 6);
          if (p) p.r = Math.min(60, p.r + 0.4);
          else w.puddles.push({ x: d.x, r: 3 });
          return false;
        }
        return d.x > -20 && d.x < W + 20;
      });
      w.shards = w.shards.filter((s) => {
        s.vy += GRAVITY * dt;
        s.x += s.vx * dt;
        s.y += s.vy * dt;
        s.a += s.va * dt;
        if (s.y > FLOOR + 12) {
          s.y = FLOOR + 12;
          s.vy = -s.vy * 0.3;
          s.vx *= 0.8;
          s.va *= 0.5;
        }
        s.life -= dt * 0.12;
        return s.life > 0;
      });
      for (const p of w.puddles) p.r = Math.max(0, p.r - dt * 0.6);
      w.puddles = w.puddles.filter((p) => p.r > 0.5);
      draw(ctx);
      raf = requestAnimationFrame(step);
    };

    const draw = (c: CanvasRenderingContext2D) => {
      const { bottle, glass } = w;
      c.clearRect(0, 0, W, H);
      const ink = getComputedStyle(el).getPropertyValue('--ink').trim() || '#0d0d0d';
      // the table
      c.strokeStyle = ink;
      c.lineWidth = 3;
      c.beginPath();
      c.moveTo(0, FLOOR + 1.5);
      c.lineTo(W, FLOOR + 1.5);
      c.stroke();
      // puddles
      for (const p of w.puddles) {
        c.fillStyle = 'rgba(150, 200, 245, 0.75)';
        c.beginPath();
        c.ellipse(p.x, FLOOR + 4, p.r, Math.max(2, p.r * 0.22), 0, 0, Math.PI * 2);
        c.fill();
      }
      // the glass
      if (!glass.broken) {
        c.save();
        c.translate(glass.x, glass.y);
        c.rotate(glass.angle);
        c.fillStyle = 'rgba(255,255,255,0.35)';
        c.fillRect(-GLASS_W / 2, -GLASS_H, GLASS_W, GLASS_H);
        const lh = (w.level / GLASS_MAX) * (GLASS_H - 6);
        c.fillStyle = 'rgba(165, 205, 245, 0.95)';
        c.fillRect(-GLASS_W / 2 + 3, -lh - 3, GLASS_W - 6, lh);
        c.strokeStyle = ink;
        c.lineWidth = 3;
        c.strokeRect(-GLASS_W / 2, -GLASS_H, GLASS_W, GLASS_H);
        c.restore();
      }
      // the bottle
      if (!bottle.broken) {
        c.save();
        c.translate(bottle.x, bottle.y);
        c.rotate(bottle.angle);
        c.fillStyle = '#3f6b43';
        c.fillRect(-BOTTLE_W / 2, -BOTTLE_H + 30, BOTTLE_W, BOTTLE_H - 30);
        c.fillStyle = '#2b4a2e';
        c.fillRect(-7, -BOTTLE_H, 14, 32);
        // what is left inside
        const fh = (w.fill / 100) * (BOTTLE_H - 36);
        c.fillStyle = 'rgba(220, 240, 255, 0.35)';
        c.fillRect(-BOTTLE_W / 2 + 3, -fh - 3, BOTTLE_W - 6, fh);
        c.fillStyle = '#ece9e1';
        c.fillRect(-BOTTLE_W / 2 + 3, -BOTTLE_H + 46, BOTTLE_W - 6, 26);
        c.strokeStyle = ink;
        c.lineWidth = 2;
        c.strokeRect(-BOTTLE_W / 2 + 3, -BOTTLE_H + 46, BOTTLE_W - 6, 26);
        c.fillStyle = ink;
        c.font = '900 8px Unbounded, Arial Black, sans-serif';
        c.textAlign = 'center';
        c.fillText('DURAG', 0, -BOTTLE_H + 58);
        c.font = '7px monospace';
        c.fillText('40%', 0, -BOTTLE_H + 68);
        c.lineWidth = 3;
        c.strokeRect(-BOTTLE_W / 2, -BOTTLE_H + 30, BOTTLE_W, BOTTLE_H - 30);
        c.strokeRect(-7, -BOTTLE_H, 14, 32);
        c.restore();
      }
      // drops and shards
      c.fillStyle = 'rgba(190, 225, 255, 0.95)';
      for (const d of w.drops) {
        c.beginPath();
        c.arc(d.x, d.y, 2.2, 0, Math.PI * 2);
        c.fill();
      }
      for (const s of w.shards) {
        c.save();
        c.translate(s.x, s.y);
        c.rotate(s.a);
        c.globalAlpha = Math.max(0, s.life);
        c.fillStyle = s.color;
        c.strokeStyle = ink;
        c.lineWidth = 1;
        c.beginPath();
        c.moveTo(-s.s, s.s);
        c.lineTo(s.s, s.s * 0.6);
        c.lineTo(0, -s.s);
        c.closePath();
        c.fill();
        c.stroke();
        c.restore();
      }
    };

    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, []);

  const point = (e: React.PointerEvent) => {
    const r = canvas.current!.getBoundingClientRect();
    return { x: ((e.clientX - r.left) / r.width) * W, y: ((e.clientY - r.top) / r.height) * H };
  };
  const hit = (b: Body, p: { x: number; y: number }, bw: number, bh: number) => !b.broken && Math.abs(p.x - b.x) < bw / 2 + 10 && p.y < b.y + 10 && p.y > b.y - bh - 10;

  const onDown = (e: React.PointerEvent) => {
    const w = world.current;
    const p = point(e);
    e.currentTarget.setPointerCapture(e.pointerId);
    const body = hit(w.bottle, p, BOTTLE_W, BOTTLE_H) ? w.bottle : hit(w.glass, p, GLASS_W, GLASS_H) ? w.glass : null;
    if (body) {
      w.grab = { body, dx: body.x - p.x, dy: body.y - p.y, lastX: p.x, lastY: p.y, lastT: performance.now(), moved: false };
      body.vx = 0;
      body.vy = 0;
      return;
    }
    const now = performance.now();
    if (now - w.lastTap < 400) {
      // a fresh bottle and glass from under the table
      w.bottle = { x: 215, y: FLOOR, vx: 0, vy: 0, angle: 0, va: 0, broken: false };
      w.glass = { x: 75, y: FLOOR, vx: 0, vy: 0, angle: 0, va: 0, broken: false };
      w.fill = 100;
      w.level = 0;
      w.shards = [];
      setNote('');
      sound.snap();
      haptic('medium');
    }
    w.lastTap = now;
  };
  const onMove = (e: React.PointerEvent) => {
    const w = world.current;
    const g = w.grab;
    if (!g) return;
    const p = point(e);
    const now = performance.now();
    const dt = Math.max(1, now - g.lastT) / 1000;
    const nx = Math.max(20, Math.min(W - 20, p.x + g.dx));
    const ny = Math.min(FLOOR, p.y + g.dy);
    g.body.vx = (nx - g.body.x) / dt;
    g.body.vy = (ny - g.body.y) / dt;
    g.body.x = nx;
    g.body.y = ny;
    if (Math.hypot(p.x - g.lastX, p.y - g.lastY) > 4) g.moved = true;
    g.lastX = p.x;
    g.lastY = p.y;
    g.lastT = now;
  };
  const onUp = () => {
    const w = world.current;
    const g = w.grab;
    if (!g) return;
    w.grab = null;
    // a tap on the glass drinks it
    if (!g.moved && g.body === w.glass && w.level >= 10) {
      w.level = 0;
      setDrinks((n) => n + 1);
      sound.gulp();
      haptic('heavy');
    }
    // the throw keeps the hand's speed; a resting release just drops
    if (performance.now() - g.lastT > 80) {
      g.body.vx = 0;
      g.body.vy = 0;
    }
  };

  return (
    <div className={`vodka ${compact ? 'vodka--table' : ''}`} aria-label="vodka">
      <canvas ref={canvas} className="vodka__canvas" style={{ width: W, height: H }} onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp} />
      {compact ? null : <p className="vodka__hint">{note === 'broken' ? t('vodka.broken') : drinks > 0 ? t('vodka.drinks', { n: drinks }) : t('vodka.hint')}</p>}
    </div>
  );
}
