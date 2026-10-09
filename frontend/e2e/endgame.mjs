// End-to-end test of the endgame: two WebSocket bots play a whole game (the
// defender always takes, so the deck drains fast) until only the face-down
// trump is left. Then a headless browser takes over the attacker's seat and
// plays the rest through the real UI (tap = smart move): it draws the trump
// (reveal animation, the card must end up visible in the hand), plays out its
// hand and reaches the v10 "stump step" (TAKE_STUMP). A second browser takes
// the other seat for the opponent's view. `?debug=1` exposes the PixiJS scene
// snapshot the checks read.
//
// Prerequisites (not part of the project's dependencies on purpose):
//   npm i -D playwright-core
//   PORT=8082 ALLOW_DEV_AUTH=true REDIS_ADDR= BOUT_RESOLVE_DELAY=120ms STUMP_AUTO_DELAY=240s TURN_TIMEOUT=0 go run ./cmd/server
//   npm run build && VITE_BACKEND_URL=http://localhost:8082 npx vite preview --port 4175
//   BASE_URL=http://127.0.0.1:4175/ WS_URL=ws://127.0.0.1:8082/ws CHROME_PATH=/path/to/chrome node e2e/endgame.mjs
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4175/';
const WS_URL = process.env.WS_URL ?? 'ws://127.0.0.1:8082/ws';
const CHROME = process.env.CHROME_PATH;
const ROOM = 'EG' + Math.random().toString(36).slice(2, 6).toUpperCase();
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const problems = [];
const serverErrors = [];

function cardFromId(id) {
  if (id === 'RJ' || id === 'BJ') return { id, rank: 15 };
  if (id === 'SC') return { id, rank: 16 };
  return { id, rank: Number.parseInt(id.slice(2), 10) };
}

class Bot {
  constructor(id, name, create) {
    Object.assign(this, { id, name, create, state: null, skip: new Set(), acted: new Set(), errors: [], bout: -1, frozen: false });
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
  if (bot.frozen || !s || s.status !== 'playing' || s.phase === 'resolving') return;
  if (s.bout_number !== bot.bout) { bot.bout = s.bout_number; bot.skip.clear(); }
  const me = s.players.find((p) => p.id === s.viewer_id);
  if (!me || me.out) return;
  if ((s.stump_pending?.length ?? 0) > 0) {
    if (s.stump_pending.includes(me.id)) bot.once('stump', () => bot.send({ type: 'TAKE_STUMP' }));
    return;
  }
  const order = s.table_order ?? [];
  const undefended = order.filter((id) => !(s.table_cards[id]?.length));
  const hand = me.hand ?? [];
  if (s.defender_id === me.id) {
    if (undefended.length && !s.defender_taking) bot.once('take', () => bot.send({ type: 'TAKE_CARDS' }));
    return;
  }
  if (s.current_turn_player_id !== me.id) return;
  if (order.length === 0) {
    // The deck is nearly gone: hand this seat to the browser, which draws the
    // last cards one per bout and therefore the face-down trump as well.
    if (s.deck_count <= 4 && !s.trump_revealed) { bot.frozen = true; bot.onFrozen?.(s); return; }
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
const status = (p) => p.locator('.status').first().textContent();
try {
  await alice.connect();
  await bob.connect();
  log('room', ROOM);
  const frozen = new Promise((resolve) => { for (const b of bots) b.onFrozen = (s) => resolve({ bot: b, state: s }); });
  for (const b of bots) b.onState = (bot) => act(bot);
  alice.send({ type: 'READY', payload: { ready: true } });
  bob.send({ type: 'READY', payload: { ready: true } });
  const watchdog = new Promise((_, reject) => setTimeout(() => {
    const dump = bots.map((b) => {
      const s = b.state;
      const me = s?.players.find((p) => p.id === s.viewer_id);
      return `${b.name}: bout=${s?.bout_number} deck=${s?.deck_count} phase=${s?.phase} turn=${s?.current_turn_player_id === me?.id ? 'me' : 'other'} defender=${s?.defender_id === me?.id ? 'me' : 'other'} taking=${s?.defender_taking} table=${JSON.stringify(s?.table_cards)} hand=${me?.hand?.map((c) => c.id).join(',')} passed=${me?.passed} errors=${JSON.stringify(b.errors.slice(-3))} acted=${[...b.acted].slice(-4)}`;
    });
    reject(new Error('the deck did not drain in 60s\n' + dump.join('\n')));
  }, 60000));
  const { bot: seat, state: s0 } = await Promise.race([frozen, watchdog]);
  const names = Object.fromEntries(s0.players.map((p) => [p.id, p.name]));
  log(`bots stopped after bout ${s0.bout_number}: deck=${s0.deck_count}+trump; attacker=${seat.name}; hands=${s0.players.map((p) => `${p.name}:${p.hand_count}+${p.stump_count}`).join(' ')}`);
  seat.ws.close(); // the browser takes this seat; the taker bot keeps playing

  browser = await chromium.launch({
    args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
    ...(CHROME ? { executablePath: CHROME } : {}),
  });
  const openAs = async (bot, debug = false) => {
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
    await ctx.addInitScript(({ dev, room }) => {
      localStorage.setItem('durag.dev', JSON.stringify(dev));
      sessionStorage.setItem('durag.room', room);
    }, { dev: { id: bot.id, name: bot.name }, room: ROOM });
    const p = await ctx.newPage();
    p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(`[${bot.name}] ${m.text()}`); });
    p.on('pageerror', (e) => problems.push(`[${bot.name}] ${e.message}`));
    // every ERROR frame the page receives, for diagnostics
    p.on('websocket', (ws) => ws.on('framereceived', (f) => {
      try {
        const m = JSON.parse(String(f.payload));
        if (m.type === 'ERROR') serverErrors.push(`${new Date().toISOString().slice(11, 23)} ${bot.name}: ${JSON.stringify(m.payload)}`);
      } catch { /* not json */ }
    }));
    await p.goto(BASE + (debug ? '?debug=1' : ''));
    await p.waitForSelector('canvas', { timeout: 15000 });
    return p;
  };

  const P = await openAs(seat, true);
  await P.waitForFunction(() => window.__durag !== undefined, null, { timeout: 15000 });
  const snap = () => P.evaluate(() => window.__durag?.debugSnapshot() ?? null);

  // One bout through the real UI: tap the rightmost card (never covered by a
  // neighbour), let the taker bot take, pass when the UI offers it, wait for
  // the next attack turn or the stump step.
  const until = async (label, pred, timeoutMs) => {
    const t0 = Date.now();
    for (;;) {
      const v = await pred();
      if (v) return v;
      if (Date.now() - t0 > timeoutMs) throw new Error(`${label}: timed out after ${timeoutMs}ms`);
      await P.waitForTimeout(60);
    }
  };
  async function playBout(label) {
    await until(`${label}: my attack turn`, async () => ((await status(P)) ?? '').includes('АТАКУЙ'), 20000);
    await P.waitForTimeout(450); // let the stop-motion tweens settle
    const before = await snap();
    const card = before.hand[before.hand.length - 1];
    if (!card || card.missing || !card.visible) throw new Error(`${label}: no tappable hand card: ${JSON.stringify(before)}`);
    log(`${label}: tap ${card.id} at ${card.x},${card.y} (hand: ${before.hand.map((c) => `${c.id}@${c.x},${c.y}${c.visible ? '' : '!'}`).join(' ')})`);
    const errorsBefore = serverErrors.length;
    // The server confirms the attack with a new state version (polled through
    // CDP, so a throttled requestAnimationFrame cannot hide a fast bout).
    const tapped = async () => {
      await P.mouse.click(card.x, card.y);
      return until(`${label}: confirmation`, async () => {
        const s = await snap();
        if (s.version > before.version) return 'moved';
        const toast = await P.locator('.toast').textContent({ timeout: 10 }).catch(() => null);
        return toast ? `toast: ${toast}` : null;
      }, 4000).catch(() => 'nothing');
    };
    let result = await tapped();
    if (result !== 'moved') {
      // a tap that did not land is a harness timing issue only if a second one works
      log(`${label}: first tap -> ${result}; snapshot=${JSON.stringify(await snap())}; retrying`);
      await P.waitForTimeout(400);
      result = await tapped();
      if (result !== 'moved') {
        await P.screenshot({ path: join(here, 'shot-bout-fail.png') });
        throw new Error(`${label}: the tap did not become a move twice (${result}); server errors: ${JSON.stringify(serverErrors.slice(errorsBefore))}; snapshot=${JSON.stringify(await snap())}`);
      }
      problems.push(`${label}: the first tap did not land (${result}); the retry did`);
    }
    return until(`${label}: end of the bout`, async () => {
      if (await P.locator('button:has-text("ВЗЯТЬ ПЕНЁК")').count()) return 'stump';
      const st = (await status(P)) ?? '';
      const s = await snap();
      if (st.includes('АТАКУЙ') && s.table === 0 && s.version > before.version) return 'next';
      const bito = P.locator('button:has-text("БИТО")');
      if (await bito.count()) await bito.click().catch(() => {});
      return null;
    }, 20000);
  }

  // Draw the rest of the deck one card per bout; the last draw is the trump
  // and its reveal animation must end with the card visible in the hand.
  let n = 0;
  let outcome = 'next';
  while (outcome === 'next' && n < 8) {
    n++;
    outcome = await playBout(`bout-${n}`);
    if ((await snap())?.trumpRevealed) break;
  }
  if (outcome !== 'next') throw new Error(`expected to keep attacking until the trump is drawn, got ${outcome} after ${n} bouts`);
  await P.waitForTimeout(700);
  const mid = await snap();
  log(`trump reveal running: hidden=${mid.revealHidden} temp=${mid.revealTemp} suit=${mid.trumpSuit}`);
  await P.screenshot({ path: join(here, 'shot-trump-reveal.png') });
  await P.waitForTimeout(2600);
  const after = await snap();
  const broken = after.hand.filter((c) => c.missing || !c.visible || !c.renderable || c.layer !== 'hand');
  if (after.revealHidden !== null || after.revealTemp) problems.push('the trump reveal did not finish: ' + JSON.stringify(after));
  if (broken.length) problems.push('hand cards hidden after the trump reveal: ' + JSON.stringify(broken));
  if (after.drag || after.pending.length) problems.push(`stale drag/pending after the reveal: ${JSON.stringify({ drag: after.drag, pending: after.pending })}`);
  log(`after the reveal: ${after.hand.length} cards in hand, hidden=${broken.length}, trump=${after.trumpSuit}`);
  await P.screenshot({ path: join(here, 'shot-after-reveal.png') });

  while (outcome === 'next' && n < 18) {
    n++;
    outcome = await playBout(`bout-${n}`);
  }
  if (outcome !== 'stump') throw new Error(`expected the stump step, got ${outcome} after ${n} bouts`);
  log(`stump step reached after ${n} browser bouts; bot errors: ${bots.map((b) => `${b.name}:${b.errors.length}`).join(' ')}`);

  const other = bots.find((b) => b !== seat);
  other.ws.close();
  const O = await openAs(other);
  const btn = P.locator('button:has-text("ВЗЯТЬ ПЕНЁК")');
  await btn.waitFor({ timeout: 10000 });
  await O.waitForFunction(() => document.querySelector('.status')?.textContent?.includes('ЖДЁМ'), null, { timeout: 10000 });
  await P.waitForTimeout(600);
  log('pending view:', await status(P), '| me tag:', (await P.locator('.me .roletag').textContent().catch(() => '-'))?.trim());
  log('other view:', await status(O), '| opp tag:', (await O.locator('.opponent .roletag').first().textContent().catch(() => '-'))?.trim());
  await P.screenshot({ path: join(here, 'shot-stump-pending.png') });
  await O.screenshot({ path: join(here, 'shot-stump-other.png') });

  await btn.click();
  await O.waitForFunction(() => document.body.innerText.includes('берёт пенёк'), null, { timeout: 8000 });
  await P.waitForTimeout(1800);
  const taken = await snap();
  const hiddenTaken = taken.hand.filter((c) => c.missing || !c.visible || !c.renderable);
  if (taken.hand.length !== 2 || hiddenTaken.length) problems.push('stump cards not shown in the hand: ' + JSON.stringify(taken.hand));
  log('after TAKE_STUMP:', await status(P), '| other:', await status(O), `| hand=${taken.hand.length} hidden=${hiddenTaken.length}`);
  await P.screenshot({ path: join(here, 'shot-stump-taken.png') });
  await O.screenshot({ path: join(here, 'shot-stump-taken-other.png') });
  if (await btn.count()) problems.push('TAKE_STUMP button still visible after taking');
} catch (err) {
  problems.push(String(err?.stack ?? err));
} finally {
  for (const b of bots) try { b.ws?.close(); } catch {}
  await browser?.close();
}
if (serverErrors.length) log('server ERROR frames seen by the browsers:\n' + serverErrors.join('\n'));
if (problems.length) { console.error('PROBLEMS:\n' + problems.join('\n')); process.exit(1); }
log('endgame test OK');
