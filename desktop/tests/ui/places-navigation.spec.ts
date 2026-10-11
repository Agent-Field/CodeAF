import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

const rail = (page: Page) => page.locator('.app-shell .place-rail').first();
// The Home in front. Places are libraries, so other places' Homes can sit in the strip beside it.
const home = (page: Page) => page.locator('.workspace-tab.is-place-home[data-active="true"]');
const tabs = (page: Page) => page.locator('.workspace-tabstrip').getByRole('tab');
const row = (page: Page, name: string) => rail(page).locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}`) }) });

async function boot(page: Page) {
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, {
    places: [{ name: 'Parent', pinned: true }, { name: 'Child', parents: ['Parent'], lastOpenedAt: 'now' }],
    chats: [],
  });
  await page.goto('/');
  await row(page, 'Child').locator('.rail-place').click();
  await expect(home(page)).toContainText('Child');
  return places;
}

test('Home goes to its primary parent with Up as a tab, then opens All places as a tab', async ({ page }) => {
  await boot(page);
  const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
  await page.locator('.home-scroll').focus();
  await page.keyboard.press(`${modifier}+ArrowUp`);
  await expect(home(page)).toContainText('Parent');
  await page.locator('.home-scroll').focus();
  await page.keyboard.press(`${modifier}+ArrowUp`);
  await expect(page.getByRole('heading', { name: 'All places', exact: true })).toBeVisible();
  // Nothing was replaced: the Child and Parent Homes are still tabs beside All places.
  await expect(tabs(page).filter({ hasText: 'Child' })).toHaveCount(1);
  await expect(tabs(page).filter({ hasText: 'Parent' })).toHaveCount(1);
});

test('browser new-window action uses this window; closing a place leaves its tab open and going there again focuses it', async ({ page, context }) => {
  await boot(page);
  const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
  await page.keyboard.press(`${modifier}+KeyT`);
  await expect(page.getByRole('combobox', { name: 'Search or start' })).toBeFocused();
  await row(page, 'Parent').locator('.rail-place').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Open in new window/ }).click();
  await expect(home(page)).toContainText('Parent');
  expect(context.pages()).toHaveLength(1);
  await row(page, 'Child').locator('.rail-place').click();
  await expect(tabs(page)).toHaveCount(4);
  await row(page, 'Child').hover();
  await row(page, 'Child').getByRole('button', { name: /^Close Child/ }).click();
  // Closing the place takes it off the rail and closes no tab.
  await expect(row(page, 'Child')).toHaveCount(0);
  await expect(tabs(page)).toHaveCount(4);
  await page.keyboard.press(`${modifier}+KeyP`);
  await page.getByRole('dialog').getByRole('combobox').fill('Child');
  await page.keyboard.press('Enter');
  await expect(home(page)).toContainText('Child');
  await expect(tabs(page)).toHaveCount(4);
});
