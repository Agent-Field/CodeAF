import { test, expect, type Page } from '@playwright/test';

async function mount(page: Page, theme = 'light', parentName = 'Config stack') {
  await page.goto('/');
  await page.evaluate(async ({ theme, parentName }) => {
    // Vite transforms the fixture's imports and serves the real components.
    const path = '/tests/ui/support/back-chip-harness.tsx';
    const { mountBackChip } = await import(path);
    mountBackChip(theme, parentName);
  }, { theme, parentName });
}

for (const theme of ['light', 'dark']) {
  test(`${theme}: measured chip and in-tab parent match Iteration 2`, async ({ page }) => {
    await mount(page, theme);
    const chip = page.getByRole('button', { name: /^Back to Config stack/ });
    await expect(chip).toBeVisible();
    const measure = (selector: string) => page.locator(selector).evaluate(element => {
      const s = getComputedStyle(element);
      return { height: element.getBoundingClientRect().height, padding: s.padding, radius: s.borderRadius, size: s.fontSize, weight: s.fontWeight, color: s.color };
    });
    expect(await measure('.back-chip')).toMatchObject({ height: 28, padding: '0px 10px 0px 8px', radius: '99px', size: '12px', weight: '500' });
    expect(await measure('.back-header-parent')).toMatchObject({ height: 28, padding: '0px 10px 0px 6px', radius: '8px', size: '13px', weight: '400' });
    expect(await page.locator('.back-chip .app-icon').evaluate(e => e.getBoundingClientRect().width)).toBe(13);
    expect(await page.locator('.back-header-parent .app-icon').evaluate(e => e.getBoundingClientRect().width)).toBe(14);
    await expect(page.locator('.back-header-current')).toHaveCSS('font-weight', '500');
    await expect(page.getByRole('button', { name: 'Conversation', exact: true })).toHaveCount(0);
    await chip.hover();
    await expect.poll(() => page.evaluate(() =>
      getComputedStyle(document.querySelector('.back-chip')!).color ===
      getComputedStyle(document.querySelector('.back-header-current')!).color,
    )).toBe(true);
    await chip.click();
    await expect(page.locator('main')).toHaveAttribute('data-action', 'back');
    await expect(chip).toBeVisible();
    await page.getByRole('button', { name: 'Config stack', exact: true }).click();
    await expect(page.locator('main')).toHaveAttribute('data-action', 'header-back');
    await expect(chip).toHaveCount(0);
  });
}

test('own click or key dismisses; chip keys and clicks do not', async ({ page }) => {
  await mount(page);
  const chip = page.locator('.back-chip');
  await chip.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('main')).toHaveAttribute('data-action', 'back');
  await expect(chip).toBeVisible();
  await page.getByRole('button', { name: 'Own action' }).click();
  await expect(chip).toHaveCount(0);
  await expect(page.locator('main')).toHaveAttribute('data-action', 'dismiss');
  await mount(page);
  await page.getByRole('textbox', { name: 'Draft' }).focus();
  await page.keyboard.type('a');
  await expect(chip).toHaveCount(0);
  await expect(page.getByRole('textbox')).toHaveValue('a');
});

test('unknown parent is absent; long names fit a 320px window', async ({ page }) => {
  await mount(page, 'light', '');
  await expect(page.locator('.back-chip')).toHaveCount(0);
  await page.setViewportSize({ width: 320, height: 560 });
  await mount(page, 'dark', 'A very long real parent title '.repeat(12));
  await expect(page.locator('.back-chip')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await expect(page.locator('.back-chip-label')).toHaveCSS('text-overflow', 'clip');
});
