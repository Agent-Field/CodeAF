import { test, expect } from '@playwright/test';
import { openPage } from './support/shell-navigation';

// Shell 3f/3k "New tab is a field, not a page" (SH-OQ5, SH-240, SH-241): ⌘K lands in the New-tab field; the
// four-page command palette no longer exists.
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
const field = (page: import('@playwright/test').Page) => page.getByRole('combobox', { name: 'Search or start' });
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

test('d5-sh-palette-test: ⌘K opens the New-tab field, focuses it, and never opens a dialog', async ({ page }) => {
  await page.goto('/');
  const before = await page.getByRole('tab').count();
  await page.keyboard.press(`${mod}+k`);
  await expect(field(page)).toBeFocused();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Search commands' })).toHaveCount(0);
  const after = await page.getByRole('tab').count();
  expect(after).toBeLessThanOrEqual(before + 1);
});

test('a second ⌘K reuses the empty New tab, and ⌘K from another tab returns to it', async ({ page }) => {
  await page.goto('/');
  await page.keyboard.press(`${mod}+k`);
  await expect(field(page)).toBeFocused();
  const count = await page.getByRole('tab').count();
  await page.keyboard.press(`${mod}+k`);
  await expect(field(page)).toBeFocused();
  await expect(page.getByRole('tab')).toHaveCount(count);
  await page.getByRole('tab').first().click();
  await page.keyboard.press(`${mod}+k`);
  await expect(field(page)).toBeFocused();
  await expect(page.getByRole('tab')).toHaveCount(count);
});

test('⌘K from another page brings the workspace back to the field', async ({ page }) => {
  await page.goto('/');
  await openPage(page, 'Design system');
  await page.keyboard.press(`${mod}+k`);
  await expect(field(page)).toBeFocused();
});
