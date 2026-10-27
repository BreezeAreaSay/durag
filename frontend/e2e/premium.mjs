// Seeds experience by letting two WebSocket bots finish games (Alice always
// attacks first, the defender always takes) until Alice has 9 XP, then checks in a real browser: the premium emoji
// row with XP locks, a 🏆 reaction with its full-screen effect, the LOCKED_EMOJI
// error for a player without XP, and the admin panel for an ADMIN_IDS identity.
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4174/';
const WS_URL = process.env.WS_URL ?? 'ws://127.0.0.1:8081/ws';
const CHROME = process.env.CHROME_PATH;
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const problems = [];

function cardFromId(id) {
  if (id === 'RJ' || id === 'BJ') return { id, rank: 15 };
  if (id === 'SC') return { id, rank: 16 };
  return { id, rank: Number.parseInt(id.slice(2), 10) };
}

class Bot {
  constructor(id, name, create, room) {
    Object.assign(this, { id, name, create, room, state: null, skip: new Set(), acted: new Set(), errors: [], reactions: [], bout: -1 });
  }
  connect() {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(WS_URL);
      this.ws.onopen = () => this.send({ type: 'JOIN_ROOM', payload: { room_id: this.room, create: this.create, tg_init_data: '', dev_user: { id: this.id, name: this.name } } });
      this.ws.onerror = (e) => reject(new Error(`ws error for ${this.name}: ${e.message ?? e}`));
      this.ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'STATE_UPDATE') {
          this.state = msg.payload;
          resolve();
          this.onState?.(this);
        } else if (msg.type === 'REACTION') {
          this.reactions.push(msg.payload);
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
  if (!s || s.status !== 'playing' || s.phase === 'resolving') return;
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

async function playGame(n) {
  const room = 'XP' + n + Math.random().toString(36).slice(2, 5).toUpperCase();
  const alice = new Bot('botalice1', 'Alice', true, room);
  const bob = new Bot('botbob001', 'Bob', false, room);
  await alice.connect();
  await bob.connect();
  const finished = new Promise((resolve) => {
    for (const b of [alice, bob]) b.onState = (bot) => { if (bot.state.status === 'finished') resolve(bot.state); else act(bot); };
  });
  alice.send({ type: 'READY', payload: { ready: true } });
  bob.send({ type: 'READY', payload: { ready: true } });
  const watchdog = new Promise((_, reject) => setTimeout(() => {
    const s = alice.state;
    reject(new Error(`game ${n} did not finish in 90s: bout=${s?.bout_number} deck=${s?.deck_count} phase=${s?.phase} stump=${JSON.stringify(s?.stump_pending)} errors=${JSON.stringify(alice.errors.slice(-3))}/${JSON.stringify(bob.errors.slice(-3))}`));
  }, 90000));
  const s = await Promise.race([finished, watchdog]);
  await new Promise((r) => setTimeout(r, 400)); // let the XP update and victory reactions arrive
  const final = alice.state;
  const xp = Object.fromEntries(final.players.map((p) => [p.name, p.xp]));
  const fx = [...alice.reactions, ...bob.reactions].filter((r) => r.effect).map((r) => `${r.emoji}${r.effect}`);
  const loser = final.players.find((p) => p.id === final.loser_id)?.name ?? '?';
  log(`game ${n} ${room}: ${s.bout_number} bouts, loser=${loser} xp=${JSON.stringify(xp)} effects=${fx.join(',') || '-'} botErrors=${alice.errors.length}/${bob.errors.length}`);
  // a winner with at least 9 XP gets the victory fanfare published by the server
  const fanfareDue = final.players.some((p) => p.id !== final.loser_id && (p.xp ?? 0) >= 9);
  if (fanfareDue && !fx.some((f) => f.includes('fanfare'))) problems.push(`game ${n}: no victory fanfare although a winner has 9+ XP`);
  if (!fanfareDue && fx.length) problems.push(`game ${n}: unexpected effect ${fx.join(',')}`);
  alice.ws.close();
  bob.ws.close();
  return { xp, fx };
}

let browser;
try {
  let last = { xp: {} };
  for (let n = 1; n <= 10 && (last.xp.Alice ?? 0) < 9; n++) last = await playGame(n);
  if ((last.xp.Alice ?? 0) < 9) problems.push(`Alice should have reached 9 XP, has ${last.xp.Alice}`);

  // Game 6: the browser takes Alice's seat in a running game.
  const room = 'XP6' + Math.random().toString(36).slice(2, 5).toUpperCase();
  const alice = new Bot('botalice1', 'Alice', true, room);
  const bob = new Bot('botbob001', 'Bob', false, room);
  await alice.connect();
  await bob.connect();
  const playing = new Promise((resolve) => { bob.onState = (b) => { if (b.state.status === 'playing') resolve(); }; });
  alice.send({ type: 'READY', payload: { ready: true } });
  bob.send({ type: 'READY', payload: { ready: true } });
  await playing;
  alice.ws.close();

  browser = await chromium.launch({
    args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
    ...(CHROME ? { executablePath: CHROME } : {}),
  });
  const openAs = async (dev, roomId) => {
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
    await ctx.addInitScript(({ dev, room }) => {
      localStorage.setItem('durag.dev', JSON.stringify(dev));
      if (room) sessionStorage.setItem('durag.room', room);
    }, { dev, room: roomId });
    const p = await ctx.newPage();
    p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(`[${dev.name}] ${m.text()}`); });
    p.on('pageerror', (e) => problems.push(`[${dev.name}] ${e.message}`));
    await p.goto(BASE);
    return p;
  };

  const p = await openAs({ id: 'botalice1', name: 'Alice' }, room);
  await p.waitForSelector('canvas', { timeout: 15000 });
  await p.locator('.me__meta').waitFor({ timeout: 10000 });
  const meta = await p.locator('.me__meta').textContent();
  log('me strip:', meta);
  if (!/XP \d+/.test(meta)) problems.push(`me strip has no XP: ${meta}`);

  await p.click('.me .avatar-btn');
  await p.locator('.emoji-panel').waitFor({ timeout: 5000 });
  await p.waitForTimeout(300);
  await p.screenshot({ path: join(here, 'shot-premium-panel.png') });
  const fxButtons = await p.locator('.emoji-panel__btn--fx').count();
  const locked = await p.locator('.emoji-panel__btn--locked').count();
  const hint = await p.locator('.emoji-panel__hint').textContent();
  log(`panel: premium buttons=${fxButtons} locked=${locked} hint="${hint}"`);
  const myXP = Number((meta.match(/XP (\d+)/) ?? [])[1]);
  const expectLocked = [3, 6, 9, 12, 15].filter((n) => n > myXP).length;
  if (fxButtons !== 5) problems.push(`expected 5 premium buttons, got ${fxButtons}`);
  if (locked !== expectLocked) problems.push(`expected ${expectLocked} locked buttons at XP ${myXP}, got ${locked}`);

  // the phrases row is locked below ten wins, the second page lists the wins reactions
  const lockedPhrases = await p.locator('.emoji-panel__say--locked').count();
  log('phrase chips locked:', lockedPhrases);
  if (lockedPhrases !== 8) problems.push(`expected 8 locked phrase chips for a player without wins, got ${lockedPhrases}`);
  await p.click('.emoji-panel__dot >> nth=1');
  const winsButtons = await p.locator('.emoji-panel__row--wins .emoji-panel__btn--fx').count();
  const winsLocked = await p.locator('.emoji-panel__row--wins .emoji-panel__btn--locked').count();
  log(`page 2: wins buttons=${winsButtons} locked=${winsLocked} hint="${await p.locator('.emoji-panel__hint').textContent()}"`);
  const myWins = Number((((await p.locator('.emoji-panel__hint').textContent()) ?? '').match(/Побед (\d+)/) ?? [])[1] ?? 0);
  const expectWinsLocked = [5, 10, 15, 20, 30].filter((n) => n > myWins).length;
  if (winsButtons !== 5 || winsLocked !== expectWinsLocked) problems.push(`page 2 must show ${expectWinsLocked} locked wins reactions at ${myWins} wins, got ${winsButtons}/${winsLocked}`);
  await p.screenshot({ path: join(here, 'shot-premium-page2.png') });
  await p.click('.emoji-panel__dot >> nth=0');
  await p.click('.emoji-panel__btn--fx:has-text("🏆")');
  await p.locator('.effects-layer').waitFor({ timeout: 5000 });
  await p.locator('.fx--fanfare').waitFor({ timeout: 5000 });
  const confetti = await p.locator('.fx-confetti').count();
  log('fanfare effect rendered; confetti pieces:', confetti);
  if (confetti < 40) problems.push(`expected confetti, got ${confetti}`);
  await p.waitForTimeout(500);
  await p.screenshot({ path: join(here, 'shot-premium-fanfare.png') });
  await p.waitForTimeout(400);
  const seenByBob = bob.reactions.filter((r) => r.emoji === '🏆' && r.effect === 'fanfare').length;
  log('Bob received the 🏆 reaction with effect:', seenByBob);
  if (!seenByBob) problems.push('Bob did not receive the premium reaction with its effect');

  // Bob has too little experience for 💣 (15 XP) on a fresh server: refused with LOCKED_EMOJI.
  const bobXP = bob.state.players.find((p) => p.id === bob.state.viewer_id)?.xp ?? 0;
  const before = bob.reactions.length;
  bob.send({ type: 'SEND_EMOJI', payload: { emoji: '💣' } });
  await new Promise((r) => setTimeout(r, 500));
  const refused = bob.errors.find((e) => e.code === 'LOCKED_EMOJI');
  const accepted = bob.reactions.slice(before).some((r) => r.emoji === '💣' && r.effect === 'boom');
  log(`Bob (XP ${bobXP}) 💣:`, refused ? 'refused LOCKED_EMOJI' : accepted ? 'accepted with boom' : 'no answer');
  if (bobXP < 15 && !refused) problems.push(`LOCKED_EMOJI was not returned for a player with ${bobXP} XP`);
  if (bobXP >= 15 && !accepted) problems.push(`💣 was not accepted for a player with ${bobXP} XP`);
  // a quick phrase needs ten wins: refused with LOCKED_PHRASE; the wins page with LOCKED_EMOJI
  bob.send({ type: 'SAY', payload: { text: 'ха' } });
  await new Promise((r) => setTimeout(r, 1300)); // past the reaction rate limit
  bob.send({ type: 'SEND_EMOJI', payload: { emoji: '🤝' } });
  await new Promise((r) => setTimeout(r, 500));
  if (!bob.errors.find((e) => e.code === 'LOCKED_PHRASE')) problems.push('LOCKED_PHRASE was not returned for a player without wins');
  if (bob.errors.filter((e) => e.code === 'LOCKED_EMOJI').length < 2) problems.push('LOCKED_EMOJI was not returned for the handshake without wins');

  // Admin panel for an ADMIN_IDS identity.
  const a = await openAs({ id: 'admin-e2e', name: 'Admin' }, null);
  await a.locator('button:has-text("АДМИН-ПАНЕЛЬ")').waitFor({ timeout: 10000 });
  await a.click('button:has-text("АДМИН-ПАНЕЛЬ")');
  await a.locator('.admin__table').first().waitFor({ timeout: 10000 });
  await a.waitForTimeout(500);
  await a.screenshot({ path: join(here, 'shot-admin-overview.png') });
  const overview = await a.locator('.admin__section').first().innerText();
  log('admin overview:', overview.replace(/\s+/g, ' ').slice(0, 300));
  const tabs = a.locator('.admin__tabs button');
  const tabCount = await tabs.count();
  log('admin tabs:', tabCount);
  for (let i = 1; i < tabCount; i++) {
    await tabs.nth(i).click();
    await a.waitForTimeout(400);
    const text = (await a.locator('.admin__section').first().innerText()).replace(/\s+/g, ' ');
    log(`tab ${i}: ${text.slice(0, 160)}`);
    await a.screenshot({ path: join(here, `shot-admin-tab${i}.png`) });
  }
  // A non-admin never sees the button.
  const n = await openAs({ id: 'nobody-e2e', name: 'Nobody' }, null);
  await n.locator('input.input').first().waitFor({ timeout: 10000 });
  await n.waitForTimeout(800);
  const btn = await n.locator('button:has-text("АДМИН-ПАНЕЛЬ")').count();
  log('admin button for a non-admin:', btn);
  if (btn) problems.push('a non-admin sees the admin button');
  bob.ws.close();
} catch (e) {
  problems.push(e.stack ?? String(e));
} finally {
  await browser?.close();
}
if (problems.length) {
  console.log('PROBLEMS:\n' + problems.join('\n'));
  process.exit(1);
}
log('premium/admin test OK');
