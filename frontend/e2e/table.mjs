// The table in one browser against a bot: the fly takes off (FLY_CHANCE=1,
// FLY_DELAY_MIN/MAX short), a slap earns a point, and a hand that grows past
// nine cards is dealt in two rows.
//
//   PORT=8081 ALLOW_DEV_AUTH=true REDIS_ADDR= BOT_DELAY_MIN=150ms BOT_DELAY_MAX=400ms BOUT_RESOLVE_DELAY=300ms FLY_CHANCE=1 FLY_DELAY_MIN=2s FLY_DELAY_MAX=3s go run ./cmd/server
//   BASE_URL=http://127.0.0.1:4174/ CHROME_PATH=/path/to/chrome node e2e/table.mjs
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4174/';
const CHROME = process.env.CHROME_PATH;
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);
const problems = [];

const browser = await chromium.launch({
  args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
  ...(CHROME ? { executablePath: CHROME } : {}),
});
try {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
  const p = await ctx.newPage();
  p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(m.text()); });
  p.on('pageerror', (e) => problems.push(e.message));
  await p.goto(BASE + '?debug=1');
  await p.fill('input.input', 'Alice');
  await p.click('button:has-text("Создать стол")');
  await p.locator('.zine-title--code').waitFor({ timeout: 5000 });
  await p.click('button:has-text("ДОБАВИТЬ БОТА")');
  await p.locator('.players__item >> nth=1').waitFor({ timeout: 5000 });
  await p.click('button:has-text("ГОТОВ")');
  await p.waitForSelector('canvas', { timeout: 15000 });
  await p.locator('.me__meta').waitFor({ timeout: 10000 });
  const snap = () => p.evaluate(() => window.__durag?.debugSnapshot() ?? null);
  const meta = async () => (await p.locator('.me__meta').textContent()) ?? '';
  const xpOf = (s) => Number((s.match(/XP (\d+)/) ?? [])[1] ?? 0);
  const handOf = (s) => Number((s.match(/Alice · (\d+)/) ?? [])[1] ?? 0);

  // the fly
  const xp0 = xpOf(await meta());
  await p.locator('.bug').waitFor({ timeout: 15000 });
  await p.waitForTimeout(600);
  await p.screenshot({ path: join(here, 'shot-fly.png') });
  await p.locator('.bug').dispatchEvent('pointerdown');
  await p.locator('.bug-news').waitFor({ timeout: 5000 });
  const news = await p.locator('.bug-news').textContent();
  log('fly slapped:', news);
  if (!/XP/.test(news ?? '')) problems.push(`no XP news after the slap: ${news}`);
  await p.screenshot({ path: join(here, 'shot-fly-caught.png') });
  await p.waitForTimeout(500);
  const xp1 = xpOf(await meta());
  log(`xp ${xp0} -> ${xp1}`);
  if (xp1 !== xp0 + 1) problems.push(`the slap must add one XP: ${xp0} -> ${xp1}`);

  // take everything until the hand holds nine cards or more
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline && handOf(await meta()) < 9) {
    const st = (await p.locator('.status').first().textContent()) ?? '';
    if (await p.locator('button:has-text("ВЗЯТЬ ПЕНЁК")').count()) { await p.click('button:has-text("ВЗЯТЬ ПЕНЁК")'); continue; }
    if (await p.locator('button:has-text("БЕРУ")').count()) { await p.click('button:has-text("БЕРУ")'); await p.waitForTimeout(300); continue; }
    if (st.includes('АТАКУЙ')) {
      const s = await snap();
      const card = s?.hand?.[0];
      if (card && card.visible) await p.mouse.click(card.x, card.y);
      await p.waitForTimeout(500);
      continue;
    }
    if (await p.locator('button:has-text("БИТО")').count()) { await p.click('button:has-text("БИТО")'); await p.waitForTimeout(300); continue; }
    await p.waitForTimeout(150);
  }
  const n = handOf(await meta());
  await p.waitForTimeout(900);
  const s = await snap();
  const ys = (s?.hand ?? []).map((c) => Math.round(c.y));
  const rows = new Set(ys.map((y) => Math.round(y / 30)));
  log(`hand of ${n}: y = ${ys.join(',')} -> ${rows.size} row(s)`);
  await p.screenshot({ path: join(here, 'shot-two-rows.png') });
  if (n < 9) problems.push(`the hand never reached nine cards (${n})`);
  else if (rows.size < 2) problems.push('nine or more cards must be dealt in two rows');
  const band = await p.locator('.actionband').boundingBox();
  const topRow = Math.min(...ys);
  if (band && band.y + band.height > topRow + 10) problems.push(`the action band (${(band.y + band.height).toFixed(0)}) overlaps the upper row (${topRow})`);
} catch (err) {
  problems.push(String(err?.stack ?? err));
} finally {
  await browser.close();
}
if (problems.length) {
  console.log('PROBLEMS:\n' + problems.join('\n'));
  process.exit(1);
}
log('table test OK');
