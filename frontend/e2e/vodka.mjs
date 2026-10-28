// The winner's bottle: a browser Alice beats a WebSocket Bob who takes
// everything, then pours by bringing the bottle to the glass and smashes
// one with a throw. Screenshots of each.
//
//   PORT=8081 ALLOW_DEV_AUTH=true REDIS_ADDR= BOUT_RESOLVE_DELAY=200ms go run ./cmd/server
//   BASE_URL=http://127.0.0.1:4174/ WS_URL=ws://127.0.0.1:8081/ws CHROME_PATH=... node e2e/vodka.mjs
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4174/';
const WS_URL = process.env.WS_URL ?? 'ws://127.0.0.1:8081/ws';
const CHROME = process.env.CHROME_PATH;
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const problems = [];

// Bob: joins the room, is ready, takes whatever is attacked, leads his first card when he must.
function bob(room) {
  const ws = new WebSocket(WS_URL);
  const acted = new Set();
  const send = (m) => ws.readyState === 1 && ws.send(JSON.stringify(m));
  ws.onopen = () => send({ type: 'JOIN_ROOM', payload: { room_id: room, create: false, tg_init_data: '', nickname: 'Bob', dev_user: { id: 'vodkabob', name: 'Bob' } } });
  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.type !== 'STATE_UPDATE') return;
    const s = msg.payload;
    const me = s.players.find((p) => p.id === s.viewer_id);
    const once = (k, fn) => { const key = `${s.version}:${k}`; if (!acted.has(key)) { acted.add(key); fn(); } };
    if (s.status === 'waiting' && !me?.is_ready) once('ready', () => send({ type: 'READY', payload: { ready: true } }));
    if (s.status !== 'playing' || s.phase === 'resolving' || me?.out) return;
    if (s.stump_pending?.includes(me.id)) return once('stump', () => send({ type: 'TAKE_STUMP' }));
    const undefended = (s.table_order ?? []).filter((id) => !(s.table_cards[id]?.length));
    if (s.defender_id === me.id) {
      if (undefended.length && !s.defender_taking) once('take', () => send({ type: 'TAKE_CARDS' }));
      return;
    }
    if (s.current_turn_player_id === me.id && (s.table_order ?? []).length === 0 && me.hand?.length) {
      once('attack', () => send({ type: 'PLAY_CARD', payload: { card_id: me.hand[0].id } }));
    } else if (s.current_turn_player_id === me.id && s.defender_taking && !me.passed) {
      once('pass', () => send({ type: 'RESOLVE_BOUT' }));
    }
  };
  return ws;
}

const browser = await chromium.launch({
  args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
  ...(CHROME ? { executablePath: CHROME } : {}),
});
try {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
  const p = await ctx.newPage();
  p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(m.text()); });
  p.on('pageerror', (e) => problems.push(e.stack ?? e.message));
  await p.goto(BASE + '?debug=1');
  await p.fill('input.input', 'Alice');
  await p.click('button:has-text("Создать стол")');
  await p.locator('.zine-title--code').waitFor({ timeout: 5000 });
  const room = (await p.locator('.zine-title--code').textContent())?.trim();
  log('room', room);
  const ws = bob(room);
  await p.locator('.players__item >> nth=1').waitFor({ timeout: 5000 });
  await p.click('button:has-text("ГОТОВ")');
  await p.waitForSelector('canvas', { timeout: 15000 });
  const snap = () => p.evaluate(() => window.__durag?.debugSnapshot() ?? null);
  const deadline = Date.now() + 150000;
  let bouts = 0;
  while (Date.now() < deadline) {
    if (await p.locator('.overlay .vodka').count()) break;
    const st = (await p.locator('.status').first().textContent().catch(() => '')) ?? '';
    if (await p.locator('button:has-text("ВЗЯТЬ ПЕНЁК")').count()) { await p.click('button:has-text("ВЗЯТЬ ПЕНЁК")', { timeout: 3000 }).catch(() => {}); continue; }
    if (st.includes('АТАКУЙ')) {
      const s = await snap();
      const card = s?.hand?.[0];
      if (card?.visible) await p.mouse.click(card.x, card.y);
      bouts++;
      await p.waitForTimeout(250);
      continue;
    }
    if (await p.locator('button:has-text("БИТО")').count()) { await p.click('button:has-text("БИТО")', { timeout: 3000 }).catch(() => {}); await p.waitForTimeout(200); continue; }
    if (await p.locator('button:has-text("БЕРУ")').count()) {
      // defend with a card that can cover (the table marks them), else take
      const s = await snap();
      const cover = s?.hand?.find((c) => c.playable && c.visible);
      if (cover) await p.mouse.click(cover.x, cover.y);
      else await p.click('button:has-text("БЕРУ")', { timeout: 3000 }).catch(() => {});
      await p.waitForTimeout(250);
      continue;
    }
    await p.waitForTimeout(120);
  }
  log('bouts played by Alice:', bouts);
  await p.locator('.overlay .vodka__canvas').waitFor({ timeout: 5000 });
  const c = await p.locator('.overlay .vodka__canvas').boundingBox();
  log('bottle canvas at', c);
  await p.screenshot({ path: join(here, 'shot-vodka.png') });
  // pour: grab the bottle (right side, standing on the table line at y=160/190) and hold it over the glass
  const sx = (x) => c.x + (x / 300) * c.width;
  const sy = (y) => c.y + (y / 190) * c.height;
  await p.mouse.move(sx(215), sy(110));
  await p.mouse.down();
  for (let i = 1; i <= 12; i++) { await p.mouse.move(sx(215 - i * 8), sy(110 - i * 4)); await p.waitForTimeout(30); }
  await p.waitForTimeout(1500);
  await p.screenshot({ path: join(here, 'shot-vodka-pour.png') });
  await p.mouse.up();
  await p.waitForTimeout(800);
  // drink: a tap on the glass
  await p.mouse.click(sx(75), sy(135));
  await p.waitForTimeout(300);
  const hint = await p.locator('.vodka__hint').textContent();
  log('after the drink:', hint);
  if (!/Выпито/.test(hint ?? '')) problems.push(`the glass was not drunk: ${hint}`);
  // throw the bottle (it fell where it was let go, beside the glass) hard into the table
  await p.mouse.move(sx(119), sy(110));
  await p.mouse.down();
  await p.mouse.move(sx(150), sy(20), { steps: 2 });
  await p.waitForTimeout(16);
  await p.mouse.move(sx(120), sy(150), { steps: 2 });
  await p.mouse.up();
  await p.waitForTimeout(700);
  await p.screenshot({ path: join(here, 'shot-vodka-smash.png') });
  log('after the throw:', await p.locator('.vodka__hint').textContent());
  ws.close();
} catch (err) {
  problems.push(String(err?.stack ?? err));
} finally {
  await browser.close();
}
if (problems.length) {
  console.log('PROBLEMS:\n' + problems.join('\n'));
  process.exit(1);
}
log('vodka test OK');
