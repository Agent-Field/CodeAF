import { expect, type Page } from '@playwright/test';

/** Normal chrome has only Places. Development specimens use the existing command palette. */
export async function openPage(page: Page, name: 'Design system' | 'Activity' | 'Settings' | 'Workspace' | 'Now') {
 const primary = await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control';
 if (await page.getByRole('dialog', { name: 'Navigation', exact: true }).isVisible()) await page.keyboard.press('Escape');
 if (name === 'Settings') {
  await page.keyboard.press(`${primary}+,`);
  await expect(page.getByRole('tabpanel', { name: 'Models' })).toBeVisible();
  return;
 }
 if (name === 'Now') {
  const toggle = page.getByRole('button', { name: 'Show sidebar', exact: true });
  if (await toggle.isVisible()) await toggle.click();
  await page.locator('.place-rail').getByRole('button', { name: 'Now', exact: true }).click();
  return;
 }
 await page.keyboard.press(`${primary}+k`);
 await page.getByRole('textbox', { name: 'Search commands' }).fill(name);
 await page.getByRole('button', { name: `Go to ${name}`, exact: true }).click();
}

/** Opens the actual appearance control in Settings; generic layout fixtures may instead follow System media. */
export async function openAppearance(page: Page) {
 if (!await page.getByRole('combobox', { name: 'Theme' }).isVisible()) await openPage(page, 'Settings');
 await page.getByRole('combobox', { name: 'Theme' }).click();
}
