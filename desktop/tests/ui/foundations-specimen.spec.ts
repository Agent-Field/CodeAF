import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { installMockEngine } from './support/mock-engine';
import { openPage } from './support/shell-navigation';

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: Foundations contains the nine fixture sections and accessible shared primitives`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' } });
  await page.goto('/');
  await openPage(page, 'Design system');
  const specimen = page.locator('.foundations-specimen');
  await expect(specimen.getByRole('heading', { name: 'Foundations', exact: true })).toBeVisible();
  await expect(specimen.locator('.fd-section')).toHaveCount(9);
  await expect(specimen.locator('.fd-tint')).toHaveCount(12);
  await expect(specimen.locator('.place-swatch')).toHaveCount(12);
  await expect(specimen.locator('.status-mark')).toHaveCount(8);
  await expect(specimen.locator('[data-category]')).toHaveCount(13);
  await expect(specimen.locator('[data-shimmer="live"]')).toHaveCount(1);
  await expect(specimen.locator('.cf-breathe')).toHaveCount(1);
  await expect(specimen.locator('[data-material-option]')).toHaveCount(3);
  await expect(specimen.locator('[data-type="title"]')).toHaveCSS('font-size', '20px');
  await expect(specimen.locator('[data-type="title"]')).toHaveCSS('line-height', '26px');
  for (const ramp of await specimen.locator('.fd-ramp-pair').all()) {
   await expect(ramp.locator('[data-theme="light"]')).toHaveCount(1);
   await expect(ramp.locator('[data-theme="dark"]')).toHaveCount(1);
   const colors = await ramp.locator('span').evaluateAll(cells => cells.map(cell => getComputedStyle(cell).backgroundColor));
   expect(colors[0]).not.toBe(colors[1]);
   await expect(ramp.locator('span').first()).toHaveCSS('width', '22px');
  }
  const result = await new AxeBuilder({ page }).include('.foundations-specimen').analyze();
  expect(result.violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => ({ target: n.target, failure: n.failureSummary })) }))).toEqual([]);
  for (const width of [600, 320]) {
   await page.setViewportSize({ width, height: 800 });
   const bounds = await specimen.evaluate(root => ({ width: root.clientWidth, scroll: root.scrollWidth, overflowing: [...root.querySelectorAll('*')].filter(el => el.getBoundingClientRect().right > root.getBoundingClientRect().right + 1).map(el => el.className) }));
   expect(bounds.scroll, JSON.stringify(bounds)).toBeLessThanOrEqual(bounds.width);
   expect(bounds.overflowing).toEqual([]);
  }
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(specimen.locator('[data-shimmer="live"]')).toHaveCSS('animation-name', 'none');
  await expect(specimen.locator('.cf-breathe')).toHaveCSS('animation-name', 'none');
 });
}
