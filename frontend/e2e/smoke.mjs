// End-to-end smoke test: two headless browser contexts create/join a table,
// get ready, attack, provoke an ERROR (invalid transfer), take, and reload.
//
// Prerequisites (not part of the project's dependencies on purpose):
//   npm i -D playwright-core            # or: npm i -D playwright && npx playwright install chromium
//   # backend:  ALLOW_DEV_AUTH=true REDIS_ADDR= go run ./cmd/server   (port 8080)
//   # frontend: npm run build && npm run preview                      (port 4173, proxies /ws)
//   BASE_URL=http://127.0.0.1:4173/ CHROME_PATH=/path/to/chrome node e2e/smoke.mjs
//
// Screenshots are written next to this file. The UI is driven in Russian
// (locale ru-RU); status texts come from src/i18n.ts.
import { chromium } from 'playwright-core';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BASE = process.env.BASE_URL ?? 'http://127.0.0.1:4173/';
const launch = {
  args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
  ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
};
const browser = await chromium.launch(launch);
const problems = [];
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);

async function open(name) {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'ru-RU' });
  const p = await ctx.newPage();
  p.on('console', (m) => {
    if (m.type() === 'error' && !m.text().includes('telegram.org')) problems.push(`[${name}] ${m.text()}`);
  });
  p.on('pageerror', (e) => problems.push(`[${name}] ${e.message}`));
  await p.goto(BASE);
  return p;
}

// Hand geometry for a 390x844 viewport (mirrors src/pixi/layout.ts).
const cardW = 62;
const cardH = 90;
const spacing = Math.min(cardW * 0.74, (390 - 28 - cardW) / 5);
const x0 = 195 - (spacing * 5) / 2;
const handY = 844 - cardH / 2 - 58;
const handX = (i) => x0 + i * spacing;

async function drag(p, fromX, fromY, toX, toY) {
  await p.mouse.move(fromX, fromY);
  await p.mouse.down();
  for (let i = 1; i <= 12; i++) await p.mouse.move(fromX + ((toX - fromX) * i) / 12, fromY + ((toY - fromY) * i) / 12);
  await p.mouse.up();
}

const status = (p) => p.locator('.status').first().textContent();

try {
  const a = await open('A');
  await a.fill('input.input', 'Alice');
  await a.click('button:has-text("Создать стол")');
  const code = (await a.locator('.zine-title--code').textContent({ timeout: 5000 })).trim();
  log('table', code);

  const b = await open('B');
  await b.fill('input.input', 'Bob');
  await b.fill('input.input--code', code);
  await b.click('button[type=submit]');
  await b.locator('.zine-title--code').waitFor({ timeout: 5000 });
  await a.waitForSelector('.players__item >> nth=1', { timeout: 5000 });

  await a.click('button:has-text("ГОТОВ")');
  await b.click('button:has-text("ГОТОВ")');
  await a.waitForSelector('canvas', { timeout: 10000 });
  await b.waitForSelector('canvas', { timeout: 10000 });
  await a.waitForTimeout(1500);
  await a.screenshot({ path: join(here, 'shot-table.png') });

  const attacker = (await status(a)).includes('АТАКУЙ') ? a : b;
  const defender = attacker === a ? b : a;

  await drag(attacker, handX(5), handY, 195, 420);
  await attacker.waitForFunction(() => document.querySelector('.status')?.textContent?.includes('ПОДКИНЬ'), null, { timeout: 5000 });
  await defender.waitForFunction(() => document.querySelector('.status')?.textContent?.includes('ОТБИВАЙСЯ'), null, { timeout: 5000 });
  log('attack accepted');
  await defender.waitForTimeout(800);
  await defender.screenshot({ path: join(here, 'shot-defender.png') });

  // Transfer with an arbitrary card is (almost always) illegal -> ERROR toast + card returns.
  await drag(defender, handX(0), handY, 195, 300);
  const outcome = await Promise.race([
    defender.locator('.toast').waitFor({ timeout: 4000 }).then(() => 'toast'),
    defender.waitForFunction(() => document.querySelector('.status')?.textContent?.includes('ХОД:'), null, { timeout: 4000 }).then(() => 'transferred'),
  ]).catch(() => 'nothing');
  log('transfer attempt ->', outcome, outcome === 'toast' ? await defender.locator('.toast').textContent() : '');
  if (outcome === 'nothing') problems.push('neither ERROR nor transfer after dropping a card');

  const take = defender.locator('button:has-text("БЕРУ")');
  if (await take.count()) {
    await take.click();
    await defender.waitForTimeout(1200);
    log('after take:', await status(a), '|', await status(b));
  }

  await attacker.reload();
  await attacker.waitForSelector('canvas', { timeout: 10000 });
  await attacker.waitForTimeout(1000);
  log('rejoined after reload:', await status(attacker));
} catch (err) {
  problems.push(String(err));
} finally {
  await browser.close();
}

if (problems.length) {
  console.error('PROBLEMS:\n  ' + problems.join('\n  '));
  process.exit(1);
}
log('smoke test OK');
