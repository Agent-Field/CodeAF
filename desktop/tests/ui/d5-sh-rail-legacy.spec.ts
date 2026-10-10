import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from './contracts';

// SH-033 / SH-241: the live rail is the toggle, Now, places, and a bottom Settings row.
// Address search, Activity, Find anything and the theme control are gone. Design system is a development row above Settings.
const rail = (page: Page) => page.locator('.app-shell > .sidebar');
/** Computed time is 120ms in one engine and 0.12s in the other. Both are the token. */
const millis = (value: string) => { const n = parseFloat(value); return value.trim().endsWith('ms') ? n : n * 1000; };

test.beforeEach(async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), 'light');
});

for (const theme of ['light', 'dark'] as const) {
  test(`retired rail items stay gone and Settings is the bottom row · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme === 'dark' ? 'dark' : 'light');
    const side = rail(page);
    await expect(side.getByRole('button', { name: 'Activity', exact: true })).toHaveCount(0);
    await expect(side.getByRole('button', { name: 'Find anything', exact: true })).toHaveCount(0);
    await expect(side.getByRole('button', { name: 'Inbox', exact: true })).toHaveCount(0);
    await expect(side.locator('.address-field, .sidebar-bottom, .brand-mark')).toHaveCount(0);
    await expect(side.getByRole('combobox', { name: 'Theme' })).toHaveCount(0);
    await expect(side.getByText('Find anything')).toHaveCount(0);

    const settings = side.getByRole('button', { name: 'Settings', exact: true });
    const design = side.getByRole('button', { name: 'Design system', exact: true });
    const places = side.getByRole('button', { name: 'All places' });
    await expect(settings).toBeVisible();
    await expect(design).toBeVisible();
    const measured = await settings.evaluate(el => {
      const s = getComputedStyle(el);
      const r = el.getBoundingClientRect();
      return { height: r.height, top: r.top, bottom: r.bottom, fontSize: s.fontSize, borderLeftWidth: s.borderLeftWidth, color: s.color, background: s.backgroundColor };
    });
    expect(measured.height).toBe(32);
    expect(measured.fontSize).toBe('13px');
    expect(measured.borderLeftWidth).toBe('0px');
    expect(millis(await settings.evaluate(el => getComputedStyle(el).transitionDuration))).toBe(120);
    expect(measured.color).toBe(await tokenColor(page, 'ink-2'));
    expect(measured.background).toBe('rgba(0, 0, 0, 0)');
    const above = await Promise.all([places, design].map(row => row.evaluate(el => el.getBoundingClientRect().bottom)));
    for (const bottom of above) expect(bottom).toBeLessThanOrEqual(measured.top);
    const railBottom = (await side.boundingBox())!;
    expect(railBottom.y + railBottom.height - measured.bottom).toBeLessThanOrEqual(12);

    await settings.hover();
    await expect(settings).toHaveCSS('background-color', await tokenColor(page, 'tab-hover'));
    const box = (await settings.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    expect(millis(await settings.evaluate(el => getComputedStyle(el).transitionDuration))).toBe(80);
    await page.mouse.up();
    await settings.click({ button: 'right' });
    await expect(page.getByRole('menu')).toHaveCount(0);
  });
}

test('the narrow drawer still traps focus', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 480 });
  await page.goto('/');
  const show = page.getByRole('button', { name: 'Show sidebar' });
  await show.click();
  const drawer = page.getByRole('dialog', { name: 'Navigation', exact: true });
  await expect(drawer.getByRole('button', { name: 'Hide sidebar' })).toBeFocused();
  await expect(drawer.getByRole('button', { name: 'Settings', exact: true })).toBeVisible();
  await expect(drawer.getByRole('button', { name: 'Activity', exact: true })).toHaveCount(0);
  await expect(drawer.locator('.address-field, .sidebar-bottom, .brand-mark')).toHaveCount(0);
  await page.locator('.workspace-tab-actions').getByRole('button', { name: 'New tab', exact: true, includeHidden: true }).evaluate(el => (el as HTMLElement).focus());
  await expect(drawer.getByRole('button', { name: 'Hide sidebar' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(show).toBeFocused();
});
