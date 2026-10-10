import { test, expect, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { expectAccessible, tokenColor } from './contracts';

const f = design.foundation;

async function open(page: Page, theme: 'light' | 'dark') {
 await page.emulateMedia({ colorScheme: theme });
 await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
 await page.goto('/?specimen=select');
 await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
}

async function shadow(page: Page, token: string) {
 return page.evaluate(name => {
  const probe = document.createElement('div');
  probe.style.boxShadow = `var(--${name})`;
  document.body.append(probe);
  const value = getComputedStyle(probe).boxShadow;
  probe.remove();
  return value;
 }, token);
}

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: select rows are 28px, the highlight is field-2, and the menu is surface`, async ({ page }) => {
  await open(page, theme);
  const trigger = page.getByRole('combobox', { name: 'Sample choice' });
  expect(await trigger.evaluate(element => Math.round(element.getBoundingClientRect().height))).toBe(28);
  await expect(trigger).toHaveCSS('border-top-left-radius', f['radius-control']);
  await expect(trigger).toHaveCSS('background-color', await tokenColor(page, 'field'));
  await trigger.click();
  const menu = page.getByRole('listbox');
  await expect(menu).toBeVisible();
  await expect(menu).toHaveCSS('background-color', await tokenColor(page, 'surface'));
  await expect(menu).toHaveCSS('border-top-left-radius', f['radius-menu']);
  await expect(menu).toHaveCSS('padding-top', f['menu-pad']);
  await expect(menu).toHaveCSS('box-shadow', await shadow(page, 'sh-2'));
  const highlighted = page.locator('.select-option[data-highlighted]');
  expect(await highlighted.evaluate(element => (element as HTMLElement).offsetHeight)).toBe(28);
  await expect(highlighted).toHaveCSS('border-top-left-radius', f['radius-sm']);
  await expect(highlighted).toHaveCSS('font-size', f['font-size-prose']);
  await expect(highlighted).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
  await expect(highlighted).toHaveCSS('color', await tokenColor(page, 'ink'));
  const checked = page.locator('.select-option[data-state="checked"]');
  await expect(checked).toHaveAttribute('data-highlighted', '');
  await expect(checked.locator('.app-icon')).toHaveCSS('color', await tokenColor(page, 'ink-2'));
  const disabled = page.getByRole('option', { name: 'Gamma' });
  await expect(disabled).toHaveAttribute('data-disabled', '');
  await expect(disabled).toHaveCSS('opacity', f['opacity-control-disabled']);
  await expectAccessible(page);
 });
}

test('select keyboard Home, End, Enter and Escape', async ({ page }) => {
 await open(page, 'light');
 const trigger = page.getByRole('combobox', { name: 'Sample choice' });
 await trigger.focus();
 await page.keyboard.press('Enter');
 const menu = page.getByRole('listbox');
 await expect(menu).toBeVisible();
 await expect(page.getByRole('option', { name: 'Beta' })).toBeFocused();
 await page.keyboard.press('Home');
 await expect(page.getByRole('option', { name: 'Alpha' })).toBeFocused();
 await page.keyboard.press('End');
 await expect(page.getByRole('option', { name: 'Delta' })).toBeFocused();
 await expect(page.getByRole('option', { name: 'Gamma' })).not.toBeFocused();
 await page.keyboard.press('Enter');
 await expect(menu).not.toBeVisible();
 await expect(trigger).toContainText('Delta');
 await page.keyboard.press('Enter');
 await expect(menu).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(menu).not.toBeVisible();
 await expect(trigger).toBeFocused();
 await expect(trigger).toContainText('Delta');
});
