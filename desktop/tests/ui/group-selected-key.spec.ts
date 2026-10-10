import { test, expect, type Page } from '@playwright/test';

// ⌘G groups the selected tabs (Shell 2h; Interactions Shortcuts ⌘G): the selection clears and rename opens on the label.
const KEY = 'codeaf.desktop.workspace.v1';
const tab = (id: string, title: string) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual' });
async function seed(page: Page) {
 const tabs = [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')];
 const state = { tabs, groups: [], closed: [], nextNumber: 4, recentIds: ['a', 'b', 'c'], activeId: 'a' };
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('gsk')) { localStorage.setItem(key, json); sessionStorage.setItem('gsk', '1'); } }, [KEY, JSON.stringify(state)] as const);
}
const primary = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control'));

test('⌘G groups the selected tabs, clears the selection and opens rename on the new group', async ({ page }) => {
 await seed(page);
 await page.goto('/');
 const mod = await primary(page);
 for (const name of ['Alpha', 'Gamma']) await page.getByRole('tab', { name, exact: true }).click({ modifiers: [mod as 'Meta'] });
 await page.keyboard.press(`${mod}+g`);
 const dialog = page.locator('dialog.workspace-rename[open]');
 await expect(dialog).toBeVisible();
 await expect(dialog.getByRole('textbox')).toHaveValue('New group');
 await page.keyboard.press('Enter');
 const label = page.locator('.workspace-tab-group', { has: page.locator('.workspace-group-name', { hasText: /^New group$/ }) });
 await expect(label.getByRole('tab')).toHaveCount(2);
 await expect(page.getByRole('tab', { name: 'Beta', exact: true })).not.toHaveAttribute('aria-description', /selected for grouping/);
});

test('⌘G with no selection groups the active tab alone', async ({ page }) => {
 await seed(page);
 await page.goto('/');
 await page.getByRole('tab', { name: 'Alpha', exact: true }).click();
 await page.keyboard.press(`${await primary(page)}+g`);
 await expect(page.locator('dialog.workspace-rename[open]')).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(page.locator('.workspace-group-name', { hasText: /^New group$/ })).toHaveCount(1);
});
