import { test, expect, type Page } from '@playwright/test';
import { newConversation } from './support/new-tab';
import { expectAccessible, expectNoUnstyledControls, expectThemedSurface, menuSurface, tokenColor } from './contracts';
import design from '../../src/design/tokens.json' with { type: 'json' };
// Tab behaviour never needs the engine; a send fails fast and keeps its draft.
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });
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
 await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Keep the benchmarked loop');
 await newConversation(page);
 await rename(page, 1, 'Desktop UX');
 await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Quiet chrome');
 await page.getByRole('tab', { name: 'Engine design', exact: true }).click();
 await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep the benchmarked loop');
 await page.getByRole('tab', { name: 'Desktop UX', exact: true }).click({ button: 'right' });
 await page.getByRole('menuitem', { name: /^Close tab\b/ }).click();
 await expect(page.getByRole('tab', { name: 'Engine design', exact: true })).toHaveAttribute('aria-selected', 'true');
 // Design 3g: the tab menu no longer lists "Reopen closed tab"; the key and the strip's overflow menu do.
 await page.keyboard.press(`${process.platform === 'darwin' ? 'Meta' : 'Control'}+Shift+t`);
 await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Quiet chrome');
 await page.reload();
 await expect(page.getByRole('tab', { name: 'Desktop UX', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Quiet chrome');
 const strip = await page.locator('.workspace-tabbar').boundingBox();
 const panel = await page.getByRole('tabpanel').boundingBox();
 expect(strip!.y + strip!.height).toBeLessThanOrEqual(panel!.y + 1);
 await expectAccessible(page); await expectNoUnstyledControls(page);
});
test('pinning, groups and keyboard context menus retain visible selection', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Pinned work');
 await page.getByRole('tab', { name: 'Pinned work', exact: true }).click({ button: 'right' });
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Actions for Pinned work', exact: true }), menuSurface);
 await page.getByRole('menuitem', { name: 'Pin tab', exact: true }).click();
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await rename(page, 1, 'Grouped work');
 await page.getByRole('tab', { name: 'Grouped work', exact: true }).click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Add to group', exact: true }).hover();
 await page.getByRole('menuitem', { name: /^New group…/ }).click();
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
test('many top tabs scroll under a mask with a +N menu and keep narrow-screen actions reachable', async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/');
 await expect(page.getByRole('button', { name: 'Tab actions', exact: true })).not.toBeVisible();
 for (let index = 0; index < 8; index++) await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect(page.getByRole('tab')).toHaveCount(9);
 const strip = page.locator('.workspace-tabstrip');
 await expect(strip).toHaveCSS('scrollbar-width', 'none');
 await expect(page.getByRole('button', { name: /Scroll tabs/ })).toHaveCount(0);
 const more = page.getByRole('button', { name: 'Tab actions', exact: true });
 await expect(more).toBeVisible();
 await expect(more).toHaveText(/^\+\d+/);
 await expect(strip).toHaveAttribute('data-fade-end', 'true');
 await strip.evaluate(el => { el.scrollLeft = 0; });
 await expect.poll(() => strip.evaluate(el => el.scrollLeft)).toBe(0);
 await expect(page.getByRole('button', { name: 'New tab', exact: true })).toBeInViewport();
 await expect(page.getByRole('button', { name: 'All tabs', exact: true })).toBeInViewport();
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.getByRole('button', { name: 'All tabs', exact: true }).click();
 // The overview is a full-window layer on the frame ground (design 3h), not a raised overlay.
 await expect(page.getByRole('dialog', { name: 'All tabs overview', exact: true })).toHaveCSS('background-color', await tokenColor(page, 'frame'));
 await expectAccessible(page); await page.keyboard.press('Escape');
 await expect(page.getByRole('button', { name: 'All tabs', exact: true })).toBeFocused();
 await more.click();
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Tab actions', exact: true }), menuSurface);
 await expect(page.getByRole('menuitem', { name: /^New tab/ })).toBeFocused();
 await expectAccessible(page);
 await page.keyboard.press('Escape');
 await expect(more).toBeFocused();
});
test('malformed saved state recovers to a usable workspace', async ({ page }) => {
 await page.addInitScript(() => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'bad', title: 'bad', draft: '', pinned: false, groupId: 'missing' }], groups: [], closed: [null], activeId: 'bad', nextNumber: -1 })));
 await page.goto('/'); await expect(page.getByRole('tab')).toHaveCount(1);
 await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect(page.getByRole('tab')).toHaveCount(2);
});


test('overview searches real drafts and restores focus after nested organization menus', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Engine');
 await page.getByRole('textbox', { name: 'Message', exact: true }).fill('benchmark loop');
 await newConversation(page);
 await rename(page, 1, 'Design');
 await page.getByRole('button', { name: 'All tabs', exact: true }).click();
 const overview = page.getByRole('dialog', { name: 'All tabs overview', exact: true });
 await expect(overview.getByRole('textbox', { name: 'Filter tabs', exact: true })).toBeFocused();
 await overview.getByRole('textbox', { name: 'Filter tabs', exact: true }).fill('benchmark');
 await expect(overview.getByRole('button', { name: 'Open Engine', exact: true })).toBeVisible();
 await expect(overview.getByRole('button', { name: 'Open Design', exact: true })).not.toBeVisible();
 await overview.getByRole('button', { name: 'Open Engine', exact: true }).click({ button: 'right' });
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Actions for Engine', exact: true }), menuSurface);
 await expectAccessible(page);
 await page.keyboard.press('Escape');
 await expect(overview).toBeVisible();
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
 const draft = page.getByRole('textbox', { name: 'Message', exact: true });
 await draft.fill('A real saved thought');
 // A preview belongs to a tab you are not on.
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 const draftOfOther = page.getByRole('combobox', { name: 'Search or start', exact: true });
 const preview = page.getByRole('group', { name: 'Preview of Preview', exact: true });
 await page.getByRole('tab', { name: 'Preview', exact: true }).hover();
 await expect(preview).not.toBeVisible();
 await expect(preview).toContainText('A real saved thought');
 await expect(draftOfOther).toBeFocused();
 await page.keyboard.press('Escape'); await expect(preview).not.toBeVisible();
});

test('a press closes the hover preview and it stays shut until the pointer leaves the tab', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Preview');
 await page.getByRole('textbox', { name: 'Message', exact: true }).fill('A real saved thought');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 const preview = page.getByRole('group', { name: 'Preview of Preview', exact: true });
 const tab = page.getByRole('tab', { name: 'Preview', exact: true });
 await tab.hover();
 await expect(preview).toBeVisible();
 await tab.click();
 await expect(preview).not.toBeVisible();
 // Focus and the hover timer would reopen it over the conversation; it stays shut.
 await page.waitForTimeout(design.interaction.previewOpenDelay * 2);
 await expect(preview).not.toBeVisible();
});

test('platform tab shortcuts create, close, reopen, navigate and open overview', async ({ page }) => {
 await page.goto('/');
 await expect(page.getByRole('tab')).toHaveCount(1);
 const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
 const primary = mac ? 'Meta' : 'Control';
 await page.keyboard.press(`${primary}+t`);
 await expect(page.getByRole('tab')).toHaveCount(2);
 // The shared tooltip carries the platform shortcut hint for icon-only buttons.
 await page.getByRole('button', { name: 'New tab', exact: true }).hover();
 await expect(page.getByRole('tooltip')).toHaveText(mac ? 'New tab (⌘ T)' : 'New tab (Ctrl T)');
 await page.mouse.move(0, 400);
 await page.keyboard.press(`${primary}+w`);
 await expect(page.getByRole('tab')).toHaveCount(1);
 await page.keyboard.press(`${primary}+Shift+t`);
 await expect(page.getByRole('tab')).toHaveCount(2);
 await page.getByRole('tab').first().click();
 await page.keyboard.press(mac ? 'Meta+Shift+]' : 'Control+PageDown');
 await expect(page.getByRole('tab').nth(1)).toHaveAttribute('aria-selected', 'true');
 await page.keyboard.press(mac ? 'Meta+Shift+[' : 'Control+PageUp');
 await expect(page.getByRole('tab').first()).toHaveAttribute('aria-selected', 'true');
 await page.keyboard.press(mac ? 'Meta+Shift+\\' : 'Control+Shift+a');
 await expect(page.getByRole('dialog', { name: 'All tabs overview', exact: true })).toBeVisible();
 await page.keyboard.press('Escape');
 await page.getByRole('tab').first().click({ button: 'right' });
 const menu = page.getByRole('menu').first();
 await expect(menu.getByRole('menuitem', { name: /^Close tab\b/ }).locator('kbd')).toHaveText(mac ? '⌘W' : 'Ctrl W');
 await page.keyboard.press('Escape');
 const commandTabHandled = await page.evaluate(() => {
  const event = new KeyboardEvent('keydown', { key: 'Tab', code: 'Tab', metaKey: true, bubbles: true, cancelable: true });
  window.dispatchEvent(event); return event.defaultPrevented;
 });
 expect(commandTabHandled).toBe(false);
 await expect(page.getByRole('listbox', { name: 'Switch tabs', exact: true })).not.toBeVisible();
});

test('native tab actions attach to workspace without invoking the engine', async ({ page }) => {
 await page.goto('/');
 await page.getByRole('button', { name: 'Activity', exact: true }).click();
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'new' })));
 await expect(page.getByRole('tab')).toHaveCount(2);
 await expect(page.getByRole('tab').nth(1)).toHaveAttribute('aria-selected', 'true');
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'overview' })));
 await expect(page.getByRole('dialog', { name: 'All tabs overview', exact: true })).toBeVisible();
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'close' })));
 await expect(page.getByRole('dialog', { name: 'All tabs overview', exact: true })).not.toBeVisible();
 await expect(page.getByRole('tab')).toHaveCount(1);
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'reopen' })));
 await expect(page.getByRole('tab')).toHaveCount(2);
 await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'unrecognized' })));
 await expect(page.getByRole('tab')).toHaveCount(2);
});

test('tab close sits in a fixed slot: shown on hover and on the active tab, never changing width', async ({ page }) => {
 await page.goto('/');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 const tab = page.getByRole('tab').first();
 const wrapper = page.locator('.workspace-tab').first();
 const close = wrapper.getByRole('button', { name: /^Close / });
 const activeClose = page.locator('.workspace-tab[data-active="true"]').getByRole('button', { name: /^Close / });
 await page.mouse.move(600, 400);
 await page.evaluate(async () => { await Promise.all(document.getAnimations().map(animation => animation.finished.catch(() => undefined))); });
 await expect(close).toHaveCSS('opacity', '0');
 await expect(activeClose).toHaveCSS('opacity', '1');
 const resting = await wrapper.boundingBox();
 await tab.hover();
 await expect(close).toHaveCSS('opacity', '1');
 const hovering = await wrapper.boundingBox();
 const control = await close.boundingBox();
 expect(hovering!.width).toBe(resting!.width);
 expect(control!.x).toBeGreaterThanOrEqual(hovering!.x);
 expect(control!.x + control!.width).toBeLessThanOrEqual(hovering!.x + hovering!.width);
 const slot = await wrapper.locator('.workspace-tab-close-slot').boundingBox();
 expect(slot!.width).toBe(parseFloat(design.foundation['tab-close-slot']));
 await page.mouse.move(600, 400);
 await close.focus();
 await expect(close).toHaveCSS('opacity', '1');
 await expect(close).toBeFocused();
});

test('overview cards show persisted work and mark the active tab with a ring', async ({ page }) => {
 await page.goto('/');
 const instruction = 'Inspect the shared engine boundary';
 await page.getByRole('textbox', { name: 'Message', exact: true }).fill(instruction);
 await page.getByRole('textbox', { name: 'Message', exact: true }).press('Enter');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await page.reload();
 await page.getByRole('button', { name: 'All tabs', exact: true }).click();
 const overview = page.getByRole('dialog', { name: 'All tabs overview' });
 await expect(overview.locator('.preview-text').first()).toHaveText(`Draft: ${instruction}`);
 await expect(overview).toContainText('No work yet');
 const selected = overview.locator('.overview-card[data-active="true"]');
 await expect(selected).toHaveCount(1);
 expect(await selected.evaluate(el => getComputedStyle(el).boxShadow)).toMatch(/0px 0px 0px 2px/);
 expect(await overview.locator('.overview-card[data-active="false"]').evaluate(el => getComputedStyle(el).boxShadow)).not.toMatch(/0px 0px 0px 2px/);
 await expectAccessible(page);
});

test('groups have distinct names and support overview moves, rename and reload', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'One');
 await newConversation(page); await rename(page, 1, 'Two');
 await page.getByRole('button', { name: 'All tabs', exact: true }).click();
 const overview = page.getByRole('dialog', { name: 'All tabs overview', exact: true });
 async function organize(name: string, choice: string) {
  await overview.getByRole('button', { name: `Open ${name}`, exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group', exact: true }).hover();
  await page.getByRole(choice === 'Create group' ? 'menuitem' : 'menuitemcheckbox', { name: choice, exact: true }).click();
 }
 await organize('One', 'Create group');
 await organize('Two', 'Create group');
 await expect(overview.locator('.overview-section-title')).toHaveText(['New group', 'New group 2']);
 await organize('Two', 'New group');
 await expect(overview.locator('.overview-section-title')).toHaveText(['New group']);
 await expect(overview.locator('.overview-section-count')).toHaveText(['2']);
 await overview.getByRole('button', { name: 'Done', exact: true }).click();
 await expect(overview).not.toBeVisible();
 const group = page.locator('.workspace-group-label');
 await expect(group).toHaveCount(1);
 await group.click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
 await page.getByRole('dialog', { name: 'Rename group' }).getByRole('textbox', { name: 'Name' }).fill('Release');
 await page.getByRole('button', { name: 'Save', exact: true }).click();
 await group.click();
 await expect(group).toHaveAttribute('aria-expanded', 'false');
 await expect(page.getByRole('tab', { name: 'Two', exact: true })).toBeVisible();
 await expect(page.getByRole('tab', { name: 'One', exact: true })).not.toBeVisible();
 await page.reload();
 await expect(page.locator('.workspace-group-label')).toContainText('Release');
 await expect(page.locator('.workspace-group-label')).toHaveAttribute('aria-expanded', 'false');
 await page.locator('.workspace-group-label').click();
 await expect(page.getByRole('tab', { name: 'One', exact: true })).toBeVisible();
 await expectAccessible(page);
});

test('dragging onto a collapsed group label groups the tab and preserves its draft', async ({ page }) => {
 await page.goto('/'); await rename(page, 0, 'Grouped');
 await page.getByRole('tab', { name: 'Grouped', exact: true }).click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Add to group', exact: true }).hover();
 await page.getByRole('menuitem', { name: /^New group…/ }).click();
 await newConversation(page);
 // The new tab opens last, after the group (design 2b), so it is the second tab.
 await rename(page, 1, 'Incoming');
 await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Keep my context');
 const group = page.locator('.workspace-group-label');
 await group.click(); await expect(group).toHaveAttribute('aria-expanded', 'false');
 await page.getByRole('tab', { name: 'Incoming', exact: true }).dragTo(group);
 await expect(page.locator('.workspace-tab-group .workspace-tab')).toHaveCount(2);
 await expect(group).toHaveAttribute('aria-expanded', 'true');
 await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep my context');
 await page.reload();
 await expect(page.locator('.workspace-tab-group .workspace-tab')).toHaveCount(2);
});
