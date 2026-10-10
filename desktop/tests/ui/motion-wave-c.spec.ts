import { expect, test } from '@playwright/test';
import { openAppearance, openPage } from './support/shell-navigation';

for (const theme of ['Light', 'Dark']) {
 test(`${theme}: dropdown enters from .98 at its origin in 120ms`, async ({ page }) => {
  await page.goto('/');
  await openAppearance(page);
  await page.getByRole('option', { name: `${theme} appearance`, exact: true }).click();
  await openPage(page, 'Design system');
  // Pausing the animation at its first frame measures the entrance rather than racing its 120ms lifetime.
  await page.addStyleTag({ content: '.app-menu { animation-play-state: paused !important; }' });
  await page.getByRole('button', { name: 'Open themed menu', exact: true }).click();
  const menu = page.getByRole('menu', { name: 'Open themed menu', exact: true });
  await expect(menu).toBeVisible();
  await expect(menu).toHaveCSS('animation-name', 'popover-enter');
  await expect(menu).toHaveCSS('animation-duration', '0.12s');
  await expect(menu).toHaveCSS('animation-timing-function', 'cubic-bezier(0.2, 0.8, 0.2, 1)');
  await expect(menu).toHaveCSS('transform', 'matrix(0.98, 0, 0, 0.98, 0, 0)');
  const geometry = await menu.evaluate(el => {
   const s = getComputedStyle(el);
   return { width: el.getBoundingClientRect().width, layout: (el as HTMLElement).offsetWidth, origin: s.transformOrigin, anchor: s.getPropertyValue('--radix-dropdown-menu-content-transform-origin').trim() };
  });
  expect(geometry.width / geometry.layout).toBeCloseTo(.98, 2);
  const [x, y] = geometry.anchor.split(' ');
  const originX = x.endsWith('%') ? geometry.layout * parseFloat(x) / 100 : parseFloat(x);
  expect(geometry.origin).toBe(`${originX}px ${y}`);
 });
}

test('copy confirmation lasts exactly 1200ms after a successful write', async ({ page }) => {
 await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { value: { writeText: async () => {} } }));
 await page.goto('/');
 await openPage(page, 'Design system');
 const clockStart = new Date('2026-10-10T12:00:00Z');
 await page.clock.install({ time: clockStart });
 await page.clock.pauseAt(new Date(clockStart.getTime() + 1000));
 const copy = page.locator('.markdown .copy-button').first();
 // A direct DOM press keeps the paused clock independent of Playwright's animation-frame stability checks.
 await copy.getByRole('button', { name: 'Copy code', exact: true }).evaluate(el => (el as HTMLButtonElement).click());
 await expect(copy).toHaveAttribute('data-copied', 'true');
 await expect(copy.locator('.app-icon')).toHaveAttribute('data-icon', 'check');
 await page.clock.runFor(1199);
 await expect(copy).toHaveAttribute('data-copied', 'true');
 await page.clock.runFor(1);
 await expect(copy).toHaveAttribute('data-copied', 'false');
 await expect(copy.locator('.app-icon')).toHaveAttribute('data-icon', 'copy');
});

test('reduced motion resolves foundation distances, scales and loops to rest', async ({ page }) => {
 await page.emulateMedia({ reducedMotion: 'reduce' });
 await page.goto('/');
 const values = await page.evaluate(() => {
  const s = getComputedStyle(document.documentElement);
  return Object.fromEntries(['motion-rise-row', 'motion-rise-send', 'motion-rise-toast', 'motion-place-slide', 'motion-scale-popover', 'motion-scale-quicklook', 'breathe-from', 'breathe-to', 'breathe-duration', 'cf-shimmer-duration', 'latest-shimmer-duration', 'task-view-shimmer-duration', 'motion-tab-collapse'].map(key => [key, s.getPropertyValue(`--${key}`).trim()]));
 });
 for (const [key, value] of Object.entries(values)) expect(value, key).toBe(key.includes('scale') ? '1' : key.includes('duration') || key === 'motion-tab-collapse' ? '0ms' : '0px');
});
