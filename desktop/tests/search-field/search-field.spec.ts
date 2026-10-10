import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

for (const theme of ['light', 'dark']) {
 test(`search field geometry, keyboard, responsive hint and axe: ${theme}`, async ({ page }) => {
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  await page.emulateMedia({ colorScheme: theme as 'light' | 'dark' });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  const field = page.getByRole('searchbox', { name: 'Search conversations' });
  const surface = page.locator('.search-field');
  const hint = surface.locator('kbd');
  await expect(field).toHaveAttribute('type', 'search');
  await expect(surface).toHaveCSS('height', '40px');
  await expect(surface).toHaveCSS('border-radius', '12px');
  await expect(surface).toHaveCSS('padding', '0px 14px');
  await expect(surface).toHaveCSS('gap', '10px');
  await expect(field).toHaveCSS('font-size', '13px');
  await expect(surface.locator('[data-icon="search"]')).toHaveCSS('width', '15px');
  await expect(surface.locator('[data-icon="search"]')).toHaveCSS('height', '15px');
  await expect(hint).toHaveCSS('font-size', '11px');
  await expect(hint).toHaveText(/^(⌘F|Ctrl\+F)$/);
  const rest = await surface.evaluate(e => {
   const style = getComputedStyle(e);
   const probe = document.createElement('span'); e.append(probe);
   probe.style.background = 'var(--surface)'; probe.style.boxShadow = 'var(--sh-1)';
   const expected = getComputedStyle(probe);
   const result = { background: style.backgroundColor, shadow: style.boxShadow, expectedBackground: expected.backgroundColor, expectedShadow: expected.boxShadow, placeholder: getComputedStyle(e.querySelector('input')!, '::placeholder').color, ink3: style.color };
   probe.remove(); return result;
  });
  expect(rest.background).toBe(rest.expectedBackground);
  expect(rest.shadow).toBe(rest.expectedShadow);
  expect(rest.placeholder).toBe(rest.ink3);
  const rects = await surface.evaluate(e => ({ field: e.getBoundingClientRect().toJSON(), icon: e.querySelector('.app-icon')!.getBoundingClientRect().toJSON(), input: e.querySelector('input')!.getBoundingClientRect().toJSON() }));
  expect(rects.icon.x - rects.field.x).toBe(14);
  expect(rects.input.x - rects.icon.right).toBe(10);

  await page.keyboard.press('Tab');
  await page.keyboard.press('Tab');
  await expect(field).toBeFocused();
  const ring = await surface.evaluate(e => getComputedStyle(e).boxShadow);
  expect(ring).toContain('2px'); expect(ring).toContain('6px');
  await field.fill('lexer');
  await expect(hint).toHaveCount(0);
  await expect(page.getByLabel('Search query')).toHaveText('lexer');
  await field.press('Escape');
  await expect(field).toHaveValue(''); await expect(field).not.toBeFocused();
  await expect(hint).toBeVisible();
  await field.click(); await field.press('a');
  expect(await surface.evaluate(e => getComputedStyle(e).boxShadow)).toBe(rest.shadow);
  await field.press('Escape');
  await page.getByRole('button', { name: 'Focus search' }).focus();
  await page.keyboard.press('Enter'); await expect(field).toBeFocused();
  await field.press('Escape'); await expect(field).not.toBeFocused();
  // The primitive does not register the page's global shortcut.
  await page.keyboard.press('Control+f'); await expect(field).not.toBeFocused();
  await page.mouse.move(0, 0);

  for (const width of [320, 480, 600, 601, 800, 1200]) {
   await page.setViewportSize({ width, height: 560 });
   if (width <= 600) await expect(hint).toBeHidden(); else await expect(hint).toBeVisible();
   expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
  // Only the design's explicitly dim inline hint has a contrast waiver; every other axe rule remains enforced.
  const axe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  const violations = axe.violations.flatMap(v => v.nodes.filter(n => !(v.id === 'color-contrast' && n.html.includes('class="keyboard-shortcut"') && n.html.includes('data-variant="inline"'))).map(n => ({ id: v.id, target: n.target })));
  expect(violations).toEqual([]);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(surface).toHaveCSS('transition-duration', '0s');
 });
}

test('search field follows System appearance, Mac hint, hover and press tokens', async ({ page }) => {
 await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { get: () => 'MacIntel' }));
 await page.emulateMedia({ colorScheme: 'dark' });
 await page.goto('/');
 await expect(page.locator('html')).toHaveAttribute('data-theme', 'system');
 await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', 'dark');
 await expect(page.locator('.search-field kbd')).toHaveText('⌘F');
 const surface = page.locator('.search-field');
 const colors = await surface.evaluate(e => {
  const probe = document.createElement('span'); e.append(probe);
  probe.style.color = 'var(--field)'; const hover = getComputedStyle(probe).color;
  probe.style.color = 'var(--field-2)'; const press = getComputedStyle(probe).color;
  probe.remove(); return { hover, press };
 });
 await surface.hover();
 await expect(surface).toHaveCSS('transition-duration', '0.12s');
 await expect(surface).toHaveCSS('background-color', colors.hover);
 await page.mouse.down();
 await expect(surface).toHaveCSS('transition-duration', '0.08s');
 await expect(surface).toHaveCSS('background-color', colors.press);
 await page.mouse.up();
});
