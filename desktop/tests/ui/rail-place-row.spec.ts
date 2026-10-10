import { test, expect, type Page } from '@playwright/test';

async function mount(page: Page, theme = 'light') {
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await page.evaluate(async theme => {
    const path = '/tests/ui/fixtures/rail-row-mount.ts';
    const { mountRailRows } = await import(path);
    mountRailRows(theme);
  }, theme);
  await expect(page.locator('.rail-row-main').filter({ hasText: 'Marketing' })).toBeVisible();
}
const marketing = (page: Page) => page.locator('.rail-row-main').filter({ hasText: 'Marketing' });

for (const theme of ['light', 'dark']) {
  test(`place row geometry and marks, ${theme}`, async ({ page }) => {
    await mount(page, theme);
    const row = marketing(page);
    expect(await row.evaluate(el => {
      const s = getComputedStyle(el), r = el.getBoundingClientRect();
      return [r.height, s.fontSize, s.gap, s.borderRadius, s.paddingLeft, s.paddingRight];
    })).toEqual([32, '13px', '10px', '8px', '8px', '8px']);
    await expect(row).toHaveAttribute('aria-current', 'page');
    expect(await row.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');
    const swatch = row.locator('.place-swatch');
    expect(await swatch.evaluate(el => [el.getBoundingClientRect().width, el.getBoundingClientRect().height, getComputedStyle(el).borderRadius])).toEqual([10, 10, '3px']);
    expect((await row.locator('.status-mark').boundingBox())!.width).toBe(6);
    expect(await row.locator('.rail-place-path').evaluate(el => getComputedStyle(el).fontWeight)).toBe('400');
    expect(await page.locator('.rail-meta').first().evaluate(el => getComputedStyle(el).fontSize)).toBe('11px');
    await expect(page.locator('.rail-row[data-busy-closed]')).toContainText('closed · still running');
    await expect(page.locator('.rail-row[data-busy-closed] .rail-row-close')).toHaveCount(0);
    await expect(page.locator('[aria-label="Pinned"] .rail-row-close')).toHaveCount(0);
    await expect(page.locator('[data-rail-count]')).toHaveText('3');
    await expect(page.locator('.rail-row-main').filter({ hasText: 'Personal' }).locator('.status-mark')).toHaveCount(0);
    const long = page.locator('.rail-row-main').filter({ hasText: 'A place with' });
    expect(await long.locator('.rail-place-name').evaluate(el => getComputedStyle(el).maskImage)).toContain('linear-gradient');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });

  test(`hover close, tooltip and keyboard focus, ${theme}`, async ({ page }) => {
    await mount(page, theme);
    const row = marketing(page), close = page.getByRole('button', { name: 'Close Marketing', exact: true });
    expect(await close.evaluate(el => getComputedStyle(el).opacity)).toBe('0');
    await row.hover();
    await expect(row.locator('.status-mark')).toBeVisible();
    await row.locator('.status-mark').hover();
    await expect(page.getByRole('tooltip')).toHaveText('2 need you in Marketing');
    await expect(close).toHaveCSS('opacity', '1');
    await close.hover();
    await expect(page.getByRole('tooltip')).toHaveText('Close Marketing · 4 tabs⌘⇧W');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.keyboard.press('Tab');
    await row.focus();
    expect(await row.evaluate(el => getComputedStyle(el).boxShadow)).toMatch(/2px.*6px/);
    await close.click();
    await expect(row).toHaveCount(0);
    await expect(page.locator('#visited')).toHaveText('');
    const failed = page.locator('.rail-row-main').filter({ hasText: 'Config parser' });
    await failed.click();
    await expect(page.locator('#visited')).toHaveText('failed');
  });
}

test('touch keeps status and menu access without a hover close', async ({ browser }) => {
  const context = await browser.newContext({ hasTouch: true, viewport: { width: 320, height: 560 } });
  const page = await context.newPage();
  await mount(page);
  const row = marketing(page);
  await expect(page.getByRole('button', { name: 'Close Marketing', exact: true })).toBeHidden();
  await expect(row.locator('.status-mark')).toBeVisible();
  await row.dispatchEvent('pointerdown', { pointerType: 'touch', button: 0, clientX: 80, clientY: 150 });
  await expect(page.getByRole('menuitem', { name: /^Close (?!all)/ })).toBeVisible();
  await context.close();
});
