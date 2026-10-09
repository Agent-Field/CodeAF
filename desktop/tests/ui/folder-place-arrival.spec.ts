import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';
import { installDeepLinkMock } from './support/deep-link-mock';

for (const theme of ['light', 'dark']) test(`new folder Place arrives at Home before captured graph contains it · ${theme}`, async ({ page }) => {
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  await installDeepLinkMock(page);
  await installMockEngine(page, { initial: { entries: [] } });
  const places = await installMockPlaces(page, { disk: ['/work/fresh-folder'] });
  await page.goto('/');
  await page.getByRole('button', { name: /^All places/ }).click();
  await page.evaluate(() => {
    const bridge = (window as unknown as { __TAURI_INTERNALS__: { invoke: (command: string, args?: unknown) => Promise<unknown> } }).__TAURI_INTERNALS__;
    const invoke = bridge.invoke;
    bridge.invoke = (command, args) => command === 'dialog_pick'
      ? Promise.resolve({ status: 'picked', paths: [{ path: '/work/fresh-folder', name: 'fresh-folder' }] })
      : invoke(command, args);
  });
  await page.getByRole('button', { name: 'Open a folder or repo', exact: true }).click();
  await expect(page.locator('.workspace-tab.is-place-home')).toContainText('fresh-folder');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveAttribute('placeholder', 'Start something in fresh-folder');
  const id = places.id('fresh-folder');
  expect(places.state().places.find(place => place.id === id)?.sources).toHaveLength(1);
  await expect.poll(() => places.posts(`/places/${id}/visit`).length).toBe(1);
  await expect(page.getByText('That place no longer exists.', { exact: true })).toHaveCount(0);
  await page.reload();
  await expect(page.locator('.workspace-tab.is-place-home')).toContainText('fresh-folder');
});
