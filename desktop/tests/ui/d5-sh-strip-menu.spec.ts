import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

for (const theme of ['light', 'dark'] as const) {
 test(`SH-089 background menu actions and keyboard door · ${theme}`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, plainReply());
  await page.goto('/');
  const spacer = page.locator('.workspace-tab-spacer');
  const menu = page.getByRole('menu', { name: 'Tab strip actions' });
  const tabs = page.locator('.workspace-tabstrip [role="tab"]');
  await expect(spacer).toBeVisible();
  const before = await tabs.count();
  await spacer.click({ button: 'right' });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole('menuitem')).toHaveCount(3);
  await expect(menu.getByRole('menuitem', { name: /^Reopen closed tab/ })).toBeDisabled();
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await expect(menu.getByRole('menuitem', { name: /^New tab/ })).toContainText(mac ? '⌘ T' : 'Ctrl T');
  await expect(menu.getByRole('menuitem', { name: /^Reopen closed tab/ })).toContainText(mac ? '⌘ ⇧ T' : 'Ctrl Shift T');
  await expect(menu.getByRole('menuitem', { name: /^Show all tabs/ })).toContainText(mac ? '⌘ ⇧ \\' : 'Ctrl Shift A');
  await menu.getByRole('menuitem', { name: /^New tab/ }).click();
  await expect(tabs).toHaveCount(before + 1);
  const primary = mac ? 'Meta' : 'Control';
  await page.keyboard.press(`${primary}+w`);
  await expect(tabs).toHaveCount(before);
  await spacer.click({ button: 'right' });
  await expect(menu.getByRole('menuitem', { name: /^Reopen closed tab/ })).toBeEnabled();
  await menu.getByRole('menuitem', { name: /^Reopen closed tab/ }).click();
  await expect(tabs).toHaveCount(before + 1);
  const trigger = page.getByRole('button', { name: 'Tab strip actions', exact: true });
  // Tab traversal reaches the hidden button without relying on pointer coordinates.
  await page.getByRole('button', { name: 'New tab', exact: true }).focus();
  await page.keyboard.press('Tab');
  await expect(trigger).toBeFocused();
  await page.keyboard.press('Shift+F10');
  await expect(menu).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: /^New tab/ })).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog', { name: 'All tabs overview' })).toBeVisible();
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  // The empty bar padding also opens the menu; a tab still opens its own menu.
  await page.locator('.workspace-tabbar').click({ button: 'right', position: { x: 1, y: 1 } });
  await expect(menu).toBeVisible();
  await page.keyboard.press('Escape');
  await tabs.last().click({ button: 'right' });
  await expect(menu).not.toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Rename tab', exact: true })).toBeVisible();
 });
}

for (const theme of ['light', 'dark'] as const) {
 test(`SH-089 keyboard menu fits a narrow window · ${theme}`, async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 560 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, plainReply());
  await page.goto('/');
  const trigger = page.getByRole('button', { name: 'Tab strip actions', exact: true });
  await expect(trigger).toHaveCSS('clip-path', 'inset(50%)');
  await trigger.focus();
  await page.keyboard.press('ContextMenu');
  const menu = page.getByRole('menu', { name: 'Tab strip actions' });
  await expect(menu).toBeVisible();
  const box = (await menu.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(320);
  await expect(page.locator('.workspace-tab-spacer')).toHaveAttribute('data-tauri-drag-region', 'true');
  await page.keyboard.press('Escape');
  await expect(menu).not.toBeVisible();
 });
}
