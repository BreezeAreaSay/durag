// End-to-end test of the bots (spec v10 §2): one headless browser creates a
// table, seats a bot from the lobby, gets ready and plays a few bouts against
// it through the real UI. The bot must move on its own and the game must
// progress; screenshots show the lobby with the bot and the table.
//
//   PORT=8081 ALLOW_DEV_AUTH=true REDIS_ADDR= BOT_DELAY_MIN=150ms BOT_DELAY_MAX=400ms go run ./cmd/server
//   npm run build && VITE_BACKEND_URL=http://localhost:8081 npx vite preview --port 4174
//   BASE_URL=http://127.0.0.1:4174/ CHROME_PATH=/path/to/chrome node e2e/bots.mjs
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4174/';
const CHROME = process.env.CHROME_PATH;
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const problems = [];
const botLines = [];

const browser = await chromium.launch({
  args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
  ...(CHROME ? { executablePath: CHROME } : {}),
});
try {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
  const p = await ctx.newPage();
  p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(m.text()); });
  p.on('pageerror', (e) => problems.push(e.message));
  p.on('websocket', (ws) => ws.on('framereceived', (f) => {
    try {
      const m = JSON.parse(String(f.payload));
      if (m.type === 'REACTION' && String(m.payload.player_id).startsWith('bot:')) botLines.push(`${m.payload.emoji}${m.payload.text ?? ''}`);
    } catch { /* not json */ }
  }));
  await p.goto(BASE + '?debug=1');
  await p.fill('input.input', 'Alice');
  await p.click('button:has-text("Создать стол")');
  await p.locator('.zine-title--code').waitFor({ timeout: 5000 });

  await p.click('button:has-text("ДОБАВИТЬ БОТА")');
  await p.locator('.players__item >> nth=1').waitFor({ timeout: 5000 });
  const seat = (await p.locator('.players__item >> nth=1').textContent())?.replace(/\s+/g, ' ').trim();
  log('bot seated:', seat);
  if (!seat?.includes('🤖')) problems.push('the bot seat has no robot marker');
  await p.screenshot({ path: join(here, 'shot-bot-lobby.png') });

  // a second bot can be added and removed
  await p.click('button:has-text("ДОБАВИТЬ БОТА")');
  await p.locator('.players__item >> nth=2').waitFor({ timeout: 5000 });
  await p.locator('.players__item >> nth=2 >> .players__remove').click();
  await p.waitForFunction(() => document.querySelectorAll('.players__item').length === 2, null, { timeout: 5000 });

  await p.click('button:has-text("ГОТОВ")');
  await p.waitForSelector('canvas', { timeout: 10000 });
  await p.waitForFunction(() => window.__durag !== undefined, null, { timeout: 15000 });
  const snap = () => p.evaluate(() => window.__durag?.debugSnapshot() ?? null);
  const status = () => p.locator('.status').first().textContent();
  const until = async (label, pred, timeoutMs) => {
    const t0 = Date.now();
    for (;;) {
      const v = await pred();
      if (v) return v;
      if (Date.now() - t0 > timeoutMs) throw new Error(`${label}: timed out after ${timeoutMs}ms`);
      await p.waitForTimeout(80);
    }
  };

  // Play until bout 4: when it is our attack, tap the rightmost card; when we
  // defend, press "take" (the bot does the rest); pass whenever offered.
  let bouts = 0;
  let lastBout = -1;
  const deadline = Date.now() + 60000;
  let botMoves = 0;
  while (Date.now() < deadline) {
    const st = (await status()) ?? '';
    const text = (await p.locator('.lastmove').textContent().catch(() => '')) ?? '';
    const boutText = (await p.locator('.chip', { hasText: 'ЗАХОД' }).textContent().catch(() => '')) ?? '';
    const bout = Number((boutText.match(/\d+/) ?? ['0'])[0]);
    if (bout !== lastBout) { lastBout = bout; bouts = bout; log(`bout ${bout}: ${st} | ${text}`); }
    if (/Нейронка/.test(text)) botMoves++;
    if (bout >= 4) break;
    if (await p.locator('button:has-text("ВЗЯТЬ ПЕНЁК")').count()) { await p.click('button:has-text("ВЗЯТЬ ПЕНЁК")'); continue; }
    if (st.includes('АТАКУЙ')) {
      await p.waitForTimeout(400);
      const s = await snap();
      const card = s?.hand?.[s.hand.length - 1];
      if (card && card.visible) await p.mouse.click(card.x, card.y);
      await p.waitForTimeout(500);
      continue;
    }
    if (await p.locator('button:has-text("БЕРУ")').count()) { await p.click('button:has-text("БЕРУ")'); await p.waitForTimeout(300); continue; }
    if (await p.locator('button:has-text("БИТО")').count()) { await p.click('button:has-text("БИТО")'); await p.waitForTimeout(300); continue; }
    await p.waitForTimeout(150);
  }
  await p.screenshot({ path: join(here, 'shot-bot-table.png') });
  log(`reached bout ${bouts}; bot moves seen in the log line: ${botMoves}; bot reactions: ${botLines.length} ${botLines.slice(0, 3).join(' | ')}`);
  if (bouts < 4) problems.push(`the game did not reach bout 4 (got ${bouts})`);
  if (botMoves === 0) problems.push('no bot move was ever shown in the last-move line');
} catch (err) {
  problems.push(String(err?.stack ?? err));
} finally {
  await browser.close();
}
if (problems.length) { console.error('PROBLEMS:\n' + problems.join('\n')); process.exit(1); }
log('bots test OK');
