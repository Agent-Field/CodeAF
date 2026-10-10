import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

const row = (page: Page, name: string) => page.locator('.app-shell .place-rail .rail-row').filter({
  has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}`) }),
});

test('a closed place remains undoable after its toast disappears and text fields keep native undo', async ({ page }) => {
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, { places: [{ name: 'Garden', lastOpenedAt: 'now' }], chats: [] });
  await page.goto('/');
  await row(page, 'Garden').locator('.rail-place').click();
  await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Garden');
  const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
  await row(page, 'Garden').hover();
  await row(page, 'Garden').getByRole('button', { name: /^Close Garden/ }).click();
  await expect(row(page, 'Garden')).toHaveCount(0);
  // Dismissal must not consume the window step; no timer is needed to prove toast expiry independence.
  await page.locator('.toast').getByRole('button', { name: 'Undo', exact: true }).focus();
  await page.keyboard.press('Escape');
  await expect(page.locator('.toast')).toHaveCount(0);
  const composer = page.getByRole('textbox', { name: 'Message', exact: true });
  await composer.focus();
  await page.keyboard.press(`${modifier}+z`);
  await expect(row(page, 'Garden')).toHaveCount(0);
  await composer.evaluate(element => (element as HTMLElement).blur());
  await page.keyboard.press(`${modifier}+z`);
  await expect(row(page, 'Garden')).toBeVisible();
  expect(places.posts('/places/rail').filter(post => post.body.op === 'visit').map(post => post.body.place)).toEqual([places.id('Garden'), places.id('Garden')]);
  await page.keyboard.press(`${modifier}+z`);
  expect(places.posts('/places/rail').filter(post => post.body.op === 'visit')).toHaveLength(2);
});

test('keyboard Undo follows mixed tab and place chronology even while a place toast remains visible', async ({ page }) => {
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, { places: [], chats: [] });
  await page.goto('/');
  const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
  await page.keyboard.press(`${modifier}+Shift+p`);
  await page.getByRole('button', { name: 'Name a place' }).click();
  await page.getByRole('textbox', { name: 'Place name' }).fill('Garden');
  await page.keyboard.press('Enter');
  const tile = page.locator('.places-tile[data-mode="place"]').filter({ hasText: 'Garden' });
  await expect(tile).toBeVisible();
  const tabs = page.locator('.workspace-tabstrip').getByRole('tab');
  const count = await tabs.count();
  await page.keyboard.press(`${modifier}+t`);
  await expect(tabs).toHaveCount(count + 1);
  await page.keyboard.press(`${modifier}+w`);
  await expect(tabs).toHaveCount(count);
  await tabs.first().focus();
  await page.keyboard.press(`${modifier}+z`);
  await expect(tabs).toHaveCount(count + 1);
  expect(places.posts('/places/undo')).toHaveLength(0);
  await tabs.first().focus();
  await page.keyboard.press(`${modifier}+z`);
  await expect.poll(() => places.posts('/places/undo').length).toBe(1);
  expect(places.state().places).toHaveLength(0);
});
