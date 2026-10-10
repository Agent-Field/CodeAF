import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { NOW, designConversations, withHistory } from './support/scenarios-history';

async function open(page: import('@playwright/test').Page) {
  await page.clock.setFixedTime(NOW);
  const conversations = designConversations();
  conversations.find(row => row.id === 'lexer')!.archived = true;
  const engine = await installMockEngine(page, withHistory(conversations));
  await page.goto('/');
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  return engine;
}

for (const theme of ['light', 'dark']) {
  test(`d5-tab-hist-test: archived row unarchives through the engine (${theme})`, async ({ page }) => {
    const engine = await open(page);
    await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
    const row = page.getByRole('option', { name: /Fix it in the lexer/ });
    await row.click({ button: 'right' });
    const menu = page.getByRole('menu', { name: 'Fix it in the lexer actions' });
    await expect(menu.getByRole('menuitem')).toHaveText(['Continue', 'Read', 'Unarchive']);
    await expect(menu.getByRole('separator')).toHaveCount(1);
    await expect(menu.getByRole('menuitem', { name: 'Delete…' })).toHaveCount(0);
    await menu.getByRole('menuitem', { name: 'Unarchive' }).click();
    await expect.poll(() => engine.history.archived()).toEqual([{ id: 'lexer', archived: false }]);
    await row.click({ button: 'right' });
    await expect(page.getByRole('menuitem', { name: 'Archive', exact: true })).toBeVisible();
    await page.getByRole('menuitem', { name: 'Read', exact: true }).click();
    await expect(page.locator('.history-message')).toHaveCount(9);
  });
}

for (const key of ['Shift+F10', 'ContextMenu']) {
  test(`d5-tab-hist-test: ${key} opens the selected row and Escape restores list focus`, async ({ page }) => {
    await open(page);
    const list = page.getByRole('listbox', { name: 'Conversations' });
    await list.focus();
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press(key);
    await expect(page.getByRole('menu', { name: 'Release v2.4 actions' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Continue' })).toBeFocused();
    await expect(page.getByRole('menuitem', { name: 'Archive', exact: true })).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(list).toBeFocused();
  });
}

for (const gesture of ['command', 'middle'] as const) {
  test(`d5-tab-hist-test: ${gesture} click opens a background tab and keeps History selected`, async ({ page }) => {
    await open(page);
    const row = page.getByRole('option', { name: /Does JSON5 handle this/ });
    await row.click(gesture === 'command' ? { modifiers: ['ControlOrMeta'] } : { button: 'middle' });
    const history = page.getByRole('tab', { name: /History/ });
    await expect(history).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('tab', { name: /Does JSON5 handle this/ })).toHaveAttribute('aria-selected', 'false');
    // The same gesture on an existing tab keeps the current view as well.
    await row.click(gesture === 'command' ? { modifiers: ['ControlOrMeta'] } : { button: 'middle' });
    await expect(history).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('tab', { name: /Does JSON5 handle this/ })).toHaveCount(1);
  });
}
