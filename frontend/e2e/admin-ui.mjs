// Admin UI on a short viewport (360x600, like a small phone or a compact Mini
// App): the lobby keeps its create/join buttons with the admin chip on top,
// the waiting room can go back to the menu, and an admin at 0 XP has every
// premium reaction (💣 plays the boom effect).
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
const inView = async (loc, h) => {
  const b = await loc.boundingBox();
  return b !== null && b.y >= 0 && b.y + b.height <= h + 1;
};
try {
  const H = 600;
  const ctx = await browser.newContext({ viewport: { width: 360, height: H }, deviceScaleFactor: 2, locale: 'ru-RU' });
  await ctx.addInitScript(() => localStorage.setItem('durag.dev', JSON.stringify({ id: 'admin-e2e', name: 'Admin' })));
  const p = await ctx.newPage();
  p.on('console', (m) => { if (m.type() === 'error' && !m.location()?.url?.includes('telegram.org')) problems.push(m.text()); });
  p.on('pageerror', (e) => problems.push(e.message));
  await p.goto(BASE);
  await p.locator('.lobby__admin').waitFor({ timeout: 10000 });
  const create = p.locator('button:has-text("Создать стол")');
  const joinBtn = p.locator('button:has-text("Войти")');
  log('lobby: admin chip in view', await inView(p.locator('.lobby__admin'), H), '· create in view', await inView(create, H), '· join in view', await inView(joinBtn, H));
  if (!(await inView(create, H)) || !(await inView(p.locator('.lobby__admin'), H))) problems.push('lobby: the create button or the admin chip is off-screen at 360x600');
  const skinsBtn = p.locator('button:has-text("СКИНЫ")');
  const rulesBtn = p.locator('button:has-text("ПРАВИЛА")');
  log('lobby: skins in view', await inView(skinsBtn, H), '· rules in view', await inView(rulesBtn, H));
  if (!(await inView(skinsBtn, H)) || !(await inView(rulesBtn, H))) problems.push('lobby: skins or rules button off-screen at 360x600');
  await p.screenshot({ path: join(here, 'shot-admin-lobby-small.png') });
  await rulesBtn.click();
  await p.locator('.rules').waitFor({ timeout: 5000 });
  log('rules screen opened; sections:', await p.locator('.rules__section').count());
  await p.screenshot({ path: join(here, 'shot-rules-small.png') });
  await p.locator('.overlay .btn').last().click();
  await p.locator('.rules').waitFor({ state: 'detached', timeout: 5000 });
  await skinsBtn.click();
  await p.locator('.skins').waitFor({ timeout: 5000 });
  const lockedSkins = await p.locator('.skin--locked').count();
  log('skins screen opened; locked skins for the admin:', lockedSkins);
  if (lockedSkins !== 0) problems.push(`the admin must own every skin, ${lockedSkins} locked`);
  await p.screenshot({ path: join(here, 'shot-skins-small.png') });
  await p.locator('.overlay .btn').last().click();
  await p.locator('.skins').waitFor({ state: 'detached', timeout: 5000 });

  await create.click();
  await p.locator('.zine-title--code').waitFor({ timeout: 5000 });
  const menu = p.locator('.waiting__top .chip--link');
  const leave = p.locator('.waiting__actions button:has-text("Выйти")');
  const scroll = await p.evaluate(() => { const s = document.querySelector('.screen'); return { sh: s.scrollHeight, ch: s.clientHeight }; });
  log(`waiting: menu chip in view ${await inView(menu, H)} · leave in view ${await inView(leave, H)} · screen ${scroll.ch}/${scroll.sh}`);
  if (!(await inView(menu, H))) problems.push('waiting room: the menu chip is off-screen');
  if (!(await inView(leave, H))) {
    await p.evaluate(() => { const s = document.querySelector('.screen'); s.scrollTop = s.scrollHeight; });
    await p.waitForTimeout(200);
    log('waiting: leave in view after scrolling', await inView(leave, H));
    if (!(await inView(leave, H))) problems.push('waiting room: the leave button cannot be scrolled into view');
  }
  await p.screenshot({ path: join(here, 'shot-admin-waiting-small.png') });
  await menu.click();
  await p.locator('button:has-text("Создать стол")').waitFor({ timeout: 5000 });
  const room = await p.evaluate(() => sessionStorage.getItem('durag.room'));
  log('back in the lobby via the menu chip; stored room =', room);
  if (room) problems.push('leaving via the menu chip did not clear the room');

  // a game as admin: every premium reaction at 0 XP
  await p.locator('button:has-text("Создать стол")').click();
  await p.locator('.zine-title--code').waitFor({ timeout: 5000 });
  await p.click('button:has-text("ДОБАВИТЬ БОТА")');
  await p.locator('.players__item').nth(1).waitFor({ timeout: 5000 });
  await p.click('.waiting__actions button:has-text("Готов")');
  await p.waitForSelector('canvas', { timeout: 15000 });
  await p.locator('.me .avatar-btn').waitFor({ timeout: 10000 });
  await p.click('.me .avatar-btn');
  await p.locator('.emoji-panel').waitFor({ timeout: 5000 });
  await p.waitForTimeout(300); // the slam-in animation
  const locked = await p.locator('.emoji-panel__btn--locked').count();
  const hint = await p.locator('.emoji-panel__hint').textContent();
  log(`admin panel of reactions: locked=${locked} hint="${hint}"`);
  if (locked !== 0 || !/админ/i.test(hint)) problems.push(`admin should see everything unlocked: locked=${locked} hint=${hint}`);
  await p.screenshot({ path: join(here, 'shot-admin-emoji-small.png') });
  await p.click('.emoji-panel__btn--fx:has-text("💣")');
  await p.locator('.fx--boom').waitFor({ timeout: 5000 });
  await p.locator('.effects-layer').waitFor({ timeout: 5000 });
  log('💣 accepted for the admin at 0 XP: boom effect rendered');
  await p.waitForTimeout(300);
  await p.screenshot({ path: join(here, 'shot-admin-boom-small.png') });
} catch (e) {
  problems.push(e.stack ?? String(e));
} finally {
  await browser.close();
}
if (problems.length) {
  console.log('PROBLEMS:\n' + problems.join('\n'));
  process.exit(1);
}
log('admin UI test OK');
