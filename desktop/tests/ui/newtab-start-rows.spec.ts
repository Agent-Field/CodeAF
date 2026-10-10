import { test, expect, type Page } from '@playwright/test';

// Shell 3f Start: New terminal while a terminal is backed, and ⌘O / Ctrl O opens or focuses a New tab
// whose caption asks for part of a file name.
const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start' });
const caption = (page: Page) => page.getByText('Type part of a file name.');
const primary = async (page: Page) => (await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control');
const terminalHint = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? '⌃`' : 'Ctrl `'));
const fileHint = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? '⌘O' : 'Ctrl O'));

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

test('Start shows New terminal with its chord and Open file', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await expect(page.getByRole('option', { name: /^New terminal/ })).toContainText(await terminalHint(page));
  await expect(page.getByRole('option', { name: /^Open file…/ })).toContainText(await fileHint(page));
});

test('command O from a conversation opens a New tab and asks for part of a file name', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('tab')).toHaveCount(1);
  await page.getByRole('tab').first().press(`${await primary(page)}+o`);
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(caption(page)).toBeVisible();
  await expect(field(page)).toBeFocused();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await page.getByRole('tab', { name: 'New tab', exact: true }).press(`${await primary(page)}+o`);
  await expect(caption(page)).toBeVisible();
  await expect(page.getByRole('tab')).toHaveCount(2);
});
