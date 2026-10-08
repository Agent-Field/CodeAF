import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, expectThemedSurface } from './contracts';
async function rename(page: Page, index: number, name: string) {
 await page.getByRole('tab').nth(index).click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Rename tab', exact: true }).click();
 const dialog = page.getByRole('dialog', { name: 'Rename tab', exact: true });
 await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
 await dialog.getByRole('button', { name: 'Save', exact: true }).click();
 await expect(dialog).not.toBeVisible();
}
test('top tabs preserve isolated drafts across closing, reopening and reload', async ({ page }) => {
 await page.goto('/');
 await rename(page, 0, 'Engine design');
 await page.getByRole('textbox', { name: 'Draft for Engine design', exact: true }).fill('Keep the benchmarked loop');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await rename(page, 1, 'Desktop UX');
 await page.getByRole('textbox', { name: 'Draft for Desktop UX', exact: true }).fill('Quiet chrome');
 await page.getByRole('tab', { name: 'Engine design', exact: true }).click();
 await expect(page.getByRole('textbox', { name: 'Draft for Engine design', exact: true })).toHaveValue('Keep the benchmarked loop');
 await page.getByRole('tab', { name: 'Desktop UX', exact: true }).click({ button: 'right' });
 await page.getByRole('menuitem', { name: /^Close tab/ }).click();
 await expect(page.getByRole('tab', { name: 'Engine design', exact: true })).toHaveAttribute('aria-selected', 'true');
 await page.getByRole('button', { name: 'Tab actions', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Reopen closed tab', exact: true }).click();
 await expect(page.getByRole('textbox', { name: 'Draft for Desktop UX', exact: true })).toHaveValue('Quiet chrome');
 await page.reload();
 await expect(page.getByRole('tab', { name: 'Desktop UX', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.getByRole('textbox', { name: 'Draft for Desktop UX', exact: true })).toHaveValue('Quiet chrome');
 const strip = await page.locator('.workspace-tabbar').boundingBox();
 const panel = await page.getByRole('tabpanel').boundingBox();
 expect(strip!.y + strip!.height).toBeLessThanOrEqual(panel!.y + 1);
 await expectAccessible(page); await expectNoUnstyledControls(page);
});
test('pinning, groups and keyboard context menus retain visible selection', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Pinned work');
 await page.getByRole('tab', { name: 'Pinned work', exact: true }).click({ button: 'right' });
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Actions for Pinned work', exact: true }));
 await page.getByRole('menuitem', { name: 'Pin tab', exact: true }).click();
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await rename(page, 1, 'Grouped work');
 await page.getByRole('tab', { name: 'Grouped work', exact: true }).click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Move to group', exact: true }).hover();
 await page.getByRole('menuitem', { name: 'New group', exact: true }).click();
 const group = page.getByRole('button', { name: /New group/ });
 await group.click(); await expect(group).toHaveAttribute('aria-expanded', 'false');
 await expect(page.getByRole('tab', { name: 'Grouped work', exact: true })).toBeVisible();
 await page.getByRole('tab', { name: 'Grouped work', exact: true }).focus();
 await page.keyboard.press('ArrowLeft');
 await expect(page.getByRole('tab', { name: 'Pinned work', exact: true })).toBeFocused();
 await page.keyboard.press('Shift+F10');
 await expect(page.getByRole('menu', { name: 'Actions for Pinned work', exact: true })).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(page.getByRole('tab', { name: 'Pinned work', exact: true })).toBeFocused();
 await expectAccessible(page);
});
test('many top tabs scroll without hiding narrow-screen actions', async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/');
 await expect(page.getByRole('button', { name: 'Scroll tabs left', exact: true })).not.toBeVisible();
 await expect(page.getByRole('button', { name: 'Scroll tabs right', exact: true })).not.toBeVisible();
 for (let index = 0; index < 8; index++) await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect(page.getByRole('tab')).toHaveCount(9);
 const strip = page.locator('.workspace-tabstrip');
 await expect(strip).toHaveCSS('scrollbar-width', 'none');
 await expect(page.getByRole('button', { name: 'Scroll tabs left', exact: true })).toBeVisible();
 const previousScroll = await strip.evaluate(el => el.scrollLeft);
 await page.getByRole('button', { name: 'Scroll tabs left', exact: true }).click();
 await expect.poll(() => strip.evaluate(el => el.scrollLeft)).toBeLessThan(previousScroll);
 await expect(page.getByRole('button', { name: 'Scroll tabs right', exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'New tab', exact: true })).toBeInViewport();
 await expect(page.getByRole('button', { name: 'All tabs', exact: true })).toBeInViewport();
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.getByRole('button', { name: 'All tabs', exact: true }).click();
 await expectThemedSurface(page, page.getByRole('dialog', { name: 'All tabs overview', exact: true }));
 await expectAccessible(page); await page.keyboard.press('Escape');
 await expect(page.getByRole('button', { name: 'All tabs', exact: true })).toBeFocused();
 await page.getByRole('button', { name: 'Tab actions', exact: true }).click();
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Tab actions', exact: true }));
 await expect(page.getByRole('menuitem', { name: /^New tab/ })).toBeFocused();
 await expectAccessible(page);
 await page.keyboard.press('Escape');
 await expect(page.getByRole('button', { name: 'Tab actions', exact: true })).toBeFocused();
});
test('malformed saved state recovers to a usable workspace', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'bad', title: 'bad', draft: '', pinned: false, groupId: 'missing' }], groups: [], closed: [null], activeId: 'bad', nextNumber: -1 })));
 await page.goto('/'); await expect(page.getByRole('tab')).toHaveCount(1);
 await expect(page.getByRole('textbox', { name: /^Draft for/ })).toBeVisible();
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect(page.getByRole('tab')).toHaveCount(2);
});


test('overview searches real drafts and restores focus after nested organization menus', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Engine');
 await page.getByRole('textbox', { name: 'Draft for Engine', exact: true }).fill('benchmark loop');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await rename(page, 1, 'Design');
 await page.getByRole('button', { name: 'All tabs', exact: true }).click();
 const overview = page.getByRole('dialog', { name: 'All tabs overview', exact: true });
 await expect(overview.getByRole('textbox', { name: 'Filter tabs', exact: true })).toBeFocused();
 await overview.getByRole('textbox', { name: 'Filter tabs', exact: true }).fill('benchmark');
 await expect(overview.getByRole('button', { name: 'Open Engine', exact: true })).toBeVisible();
 await expect(overview.getByRole('button', { name: 'Open Design', exact: true })).not.toBeVisible();
 await overview.getByRole('button', { name: 'Organize Engine', exact: true }).click();
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Organize Engine', exact: true }));
 await expectAccessible(page);
 await page.keyboard.press('Escape');
 await expect(overview).toBeVisible();
 await expect(overview.getByRole('button', { name: 'Organize Engine', exact: true })).toBeFocused();
 await overview.getByRole('button', { name: 'Open Engine', exact: true }).click();
 await expect(overview).not.toBeVisible();
 await expect(page.getByRole('tab', { name: 'Engine', exact: true })).toHaveAttribute('aria-selected', 'true');
});

test('held Control Tab previews recent tabs, Escape cancels, release commits', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'First');
 await page.getByRole('button', { name: 'New tab', exact: true }).click(); await rename(page, 1, 'Second');
 await page.getByRole('button', { name: 'New tab', exact: true }).click(); await rename(page, 2, 'Third');
 await page.getByRole('tab', { name: 'First', exact: true }).click();
 await page.keyboard.down('Control'); await page.keyboard.press('Tab');
 await expect(page.getByRole('listbox', { name: 'Switch tabs', exact: true })).toBeVisible();
 await expect(page.getByRole('option', { name: 'Third', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.getByRole('listbox', { name: 'Switch tabs', exact: true })).toBeFocused();
 await expectAccessible(page);
 await page.keyboard.press('Escape'); await page.keyboard.up('Control');
 await expect(page.getByRole('tab', { name: 'First', exact: true })).toHaveAttribute('aria-selected', 'true');
 await page.keyboard.down('Control'); await page.keyboard.press('Tab'); await page.keyboard.up('Control');
 await expect(page.getByRole('tab', { name: 'Third', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.getByRole('listbox', { name: 'Switch tabs', exact: true })).not.toBeVisible();
});


test('delayed hover previews show real draft text without moving focus', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Preview');
 const draft = page.getByRole('textbox', { name: 'Draft for Preview', exact: true });
 await draft.fill('A real saved thought');
 await page.getByRole('tab', { name: 'Preview', exact: true }).hover();
 await expect(page.getByRole('tooltip')).not.toBeVisible();
 await expect(page.getByRole('tooltip')).toContainText('A real saved thought');
 await expect(draft).toBeFocused();
 await page.keyboard.press('Escape'); await expect(page.getByRole('tooltip')).not.toBeVisible();
});
