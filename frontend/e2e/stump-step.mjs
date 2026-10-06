// End-to-end test of the v10 "stump step": two WebSocket bots play a whole game
// (the defender always takes, so the deck drains fast) until a player's hand is
// empty with the deck gone -> the server waits for TAKE_STUMP. Then two headless
// browsers take over the bots' identities, screenshot the TAKE_STUMP UI, press
// the button and screenshot the result.
//
// Prerequisites (not part of the project's dependencies on purpose):
//   npm i -D playwright-core
//   # backend with fast bouts and a long stump step, no turn timer:
//   PORT=8082 ALLOW_DEV_AUTH=true REDIS_ADDR= BOUT_RESOLVE_DELAY=120ms STUMP_AUTO_DELAY=240s TURN_TIMEOUT=0 go run ./cmd/server
//   # frontend: npm run build && VITE_BACKEND_URL=http://localhost:8082 npx vite preview --port 4175
//   BASE_URL=http://127.0.0.1:4175/ WS_URL=ws://127.0.0.1:8082/ws CHROME_PATH=/path/to/chrome node e2e/stump-step.mjs
//
// Screenshots (shot-stump-*.png) are written next to this file.
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4175/';
const WS_URL = process.env.WS_URL ?? 'ws://127.0.0.1:8082/ws';
const CHROME = process.env.CHROME_PATH;
const ROOM = 'ST' + Math.random().toString(36).slice(2, 6).toUpperCase();
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const problems = [];

function cardFromId(id) {
  if (id === 'RJ' || id === 'BJ') return { id, rank: 15 };
  if (id === 'SC') return { id, rank: 16 };
  return { id, rank: Number.parseInt(id.slice(2), 10) };
}

class Bot {
  constructor(id, name, create) {
    Object.assign(this, { id, name, create, state: null, skip: new Set(), acted: new Set(), errors: [], bout: -1 });
  }
  connect() {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(WS_URL);
      this.ws.onopen = () => this.send({ type: 'JOIN_ROOM', payload: { room_id: ROOM, create: this.create, tg_init_data: '', dev_user: { id: this.id, name: this.name } } });
      this.ws.onerror = (e) => reject(new Error(`ws error for ${this.name}: ${e.message ?? e}`));
      this.ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'STATE_UPDATE') {
          this.state = msg.payload;
          resolve();
          this.onState?.(this);
        } else if (msg.type === 'ERROR') {
          this.errors.push(msg.payload);
          if (msg.payload.card_id) {
            this.skip.add(msg.payload.card_id);
            for (const k of [...this.acted]) if (k.endsWith(':' + msg.payload.card_id)) this.acted.delete(k);
          }
        }
      };
    });
  }
  send(msg) { if (this.ws.readyState === 1) this.ws.send(JSON.stringify(msg)); }
  once(what, fn) {
    const k = `${this.state.version}:${what}`;
    if (this.acted.has(k)) return;
    this.acted.add(k);
    fn();
  }
}

function act(bot) {
  const s = bot.state;
  if (!s || s.status !== 'playing' || s.phase === 'resolving' || (s.stump_pending?.length ?? 0) > 0) return;
  if (s.bout_number !== bot.bout) { bot.bout = s.bout_number; bot.skip.clear(); }
  const me = s.players.find((p) => p.id === s.viewer_id);
  if (!me || me.out) return;
  const order = s.table_order ?? [];
  const undefended = order.filter((id) => !(s.table_cards[id]?.length));
  const hand = me.hand ?? [];
  if (s.defender_id === me.id) {
    if (undefended.length && !s.defender_taking) bot.once('take', () => bot.send({ type: 'TAKE_CARDS' }));
    return;
  }
  if (s.current_turn_player_id !== me.id) return;
  if (order.length === 0) {
    const card = hand.find((c) => !bot.skip.has(c.id));
    if (card) bot.once('attack:' + card.id, () => bot.send({ type: 'PLAY_CARD', payload: { card_id: card.id } }));
    return;
  }
  if (s.defender_taking) {
    const ranks = new Set();
    for (const id of order) { ranks.add(cardFromId(id).rank); for (const d of s.table_cards[id] ?? []) ranks.add(d.rank); }
    const card = hand.find((c) => ranks.has(c.rank) && !bot.skip.has(c.id));
    if (card) bot.once('throw:' + card.id, () => bot.send({ type: 'PLAY_CARD', payload: { card_id: card.id } }));
    else if (!me.passed) bot.once('pass', () => bot.send({ type: 'RESOLVE_BOUT' }));
  }
}

const alice = new Bot('botalice1', 'Alice', true);
const bob = new Bot('botbob001', 'Bob', false);
const bots = [alice, bob];
let browser;
try {
  await alice.connect();
  await bob.connect();
  log('room', ROOM, 'players', alice.state.players.length);
  const reached = new Promise((resolve) => {
    for (const b of bots) b.onState = (bot) => {
      if ((bot.state.stump_pending?.length ?? 0) > 0) resolve(bot.state);
      else act(bot);
    };
  });
  alice.send({ type: 'READY', payload: { ready: true } });
  bob.send({ type: 'READY', payload: { ready: true } });
  const watchdog = new Promise((_, reject) => setTimeout(() => reject(new Error('stump step not reached in 150s')), 150000));
  const state = await Promise.race([reached, watchdog]);
  const names = Object.fromEntries(state.players.map((p) => [p.id, p.name]));
  log(`stump step reached after bout ${state.bout_number}: pending=${state.stump_pending.map((id) => names[id])}`,
    `deck=${state.deck_count} trump=${state.trump_suit}/${state.trump_revealed} hands=${state.players.map((p) => `${p.name}:${p.hand_count}+${p.stump_count}`).join(' ')}`);
  log('bot errors:', bots.map((b) => `${b.name}:${b.errors.length}`).join(' '), [...new Set(bots.flatMap((b) => b.errors.map((e) => e.code)))].join(','));

  const pending = bots.find((b) => b.name === names[state.stump_pending[0]]); // server ids are derived from the dev id
  const other = bots.find((b) => b !== pending);
  for (const b of bots) b.ws.close();

  browser = await chromium.launch({
    args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
    ...(CHROME ? { executablePath: CHROME } : {}),
  });
  const openAs = async (bot) => {
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
    await ctx.addInitScript(({ dev, room }) => {
      localStorage.setItem('durag.dev', JSON.stringify(dev));
      sessionStorage.setItem('durag.room', room);
    }, { dev: { id: bot.id, name: bot.name }, room: ROOM });
    const p = await ctx.newPage();
    p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(`[${bot.name}] ${m.text()}`); });
    p.on('pageerror', (e) => problems.push(`[${bot.name}] ${e.message}`));
    await p.goto(BASE);
    await p.waitForSelector('canvas', { timeout: 15000 });
    return p;
  };
  const status = (p) => p.locator('.status').first().textContent();
  const P = await openAs(pending);
  const O = await openAs(other);
  const btn = P.locator('button:has-text("ВЗЯТЬ ПЕНЁК")');
  await btn.waitFor({ timeout: 10000 });
  await P.waitForTimeout(1500);
  log('pending view:', await status(P), '| me tag:', (await P.locator('.me .roletag').textContent().catch(() => '-'))?.trim());
  log('other view:', await status(O), '| opp tag:', (await O.locator('.opponent .roletag').first().textContent().catch(() => '-'))?.trim());
  await P.screenshot({ path: join(here, 'shot-stump-pending.png') });
  await O.screenshot({ path: join(here, 'shot-stump-other.png') });

  await btn.click();
  await O.waitForFunction(() => document.body.innerText.includes('берёт пенёк'), null, { timeout: 8000 });
  await P.waitForTimeout(1800);
  log('after TAKE_STUMP:', await status(P), '| other:', await status(O));
  await P.screenshot({ path: join(here, 'shot-stump-taken.png') });
  await O.screenshot({ path: join(here, 'shot-stump-taken-other.png') });
  if (await btn.count()) problems.push('TAKE_STUMP button still visible after taking');
} catch (err) {
  problems.push(String(err?.stack ?? err));
} finally {
  for (const b of bots) try { b.ws?.close(); } catch {}
  await browser?.close();
}
if (problems.length) { console.error('PROBLEMS:\n' + problems.join('\n')); process.exit(1); }
log('stump-step test OK');
