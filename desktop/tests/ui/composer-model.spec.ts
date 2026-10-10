import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openApp } from './support/conversation';

const fresh = () => ({ ...plainReply(), initial: { entries: [], title: '' } });
const chip = (page: Page) => page.getByRole('button', { name: 'Model: DeepSeek v4.1 Flash' });

test('the model chip says DS Flash from a fresh tab and opens the quick-swap popover with only what the engine has', async ({ page }) => {
  await installMockEngine(page, fresh());
  await openApp(page);
  await expect(chip(page)).toHaveText('DS Flash');
  await chip(page).click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  await expect(popover).toBeVisible();
  // One real model: one pinned segment, one checked row, no invented models, no Effort row, no shortcuts.
  await expect(popover.getByRole('radiogroup', { name: 'Pinned models', exact: true }).getByRole('radio')).toHaveText(['DS Flash']);
  const rows = popover.getByRole('radiogroup', { name: 'Models', exact: true }).getByRole('radio');
  await expect(rows).toHaveText(['DeepSeek v4.1 Flash']);
  await expect(rows.first()).toHaveAttribute('aria-checked', 'true');
  await expect(popover.getByText('Effort')).toHaveCount(0);
  await expect(popover.getByText('All models…')).toHaveCount(0);
  await expect(popover.locator('kbd')).toHaveCount(0);
});

test('hover is a fill only, the popover uses sh-2, and Esc returns focus to the chip', async ({ page }) => {
  await installMockEngine(page, fresh());
  await openApp(page);
  await chip(page).click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  const style = await popover.evaluate(node => { const c = getComputedStyle(node); return { shadow: c.boxShadow, radius: c.borderRadius, width: c.width }; });
  expect(style.shadow).not.toBe('none');
  expect(style.radius).toBe('14px');
  expect(style.width).toBe('290px');
  const row = popover.getByRole('radiogroup', { name: 'Models', exact: true }).getByRole('radio');
  await row.hover();
  const hovered = await row.evaluate(node => { const c = getComputedStyle(node); return { bg: c.backgroundColor, border: c.borderTopWidth, shadow: c.boxShadow, transform: c.transform }; });
  expect(hovered.bg).not.toBe('rgba(0, 0, 0, 0)');
  expect(hovered.border).toBe('0px');
  expect(hovered.shadow).toBe('none');
  expect(hovered.transform).toBe('none');
  await page.keyboard.press('Escape');
  await expect(popover).toHaveCount(0);
  await expect(chip(page)).toBeFocused();
});

test('arrows move between controls, Enter closes with focus on the chip, and a scroll closes the popover', async ({ page }) => {
  await installMockEngine(page, fresh());
  await openApp(page);
  await chip(page).click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  const stops = popover.getByRole('radio');
  await expect(stops.nth(1)).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(stops.nth(0)).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(stops.nth(1)).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(popover).toHaveCount(0);
  await expect(chip(page)).toBeFocused();
  await chip(page).click();
  await expect(popover).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event('scroll')));
  await expect(popover).toHaveCount(0);
});

test('with routing and effort the popover shows pinned segments, Effort, shortcuts, and the chord swaps directly', async ({ page }) => {
  await installMockEngine(page, fresh());
  await page.goto('/');
  await openPage(page, 'Design system');
  const section = page.getByLabel('Model popover try-out');
  await section.scrollIntoViewIfNeeded();
  await section.getByRole('button', { name: /^Model:/ }).click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  await expect(popover.getByRole('radiogroup', { name: 'Pinned models', exact: true }).getByRole('radio')).toHaveText(['Flash', 'Pro', 'Sonnet']);
  const effort = popover.getByRole('radiogroup', { name: 'Effort' });
  await expect(effort.getByRole('radio', { name: 'Medium' })).toHaveAttribute('aria-checked', 'true');
  await expect(effort.getByRole('radio', { name: 'Medium' })).toHaveCSS('font-weight', '500');
  await expect(effort.getByRole('radio', { name: 'Low' })).toHaveCSS('font-weight', '400');
  await expect(popover.locator('kbd')).toHaveCount(3);
  await popover.getByRole('radio', { name: 'Sonnet' }).last().click();
  await expect(popover).toHaveCount(0);
  await expect(section.getByRole('button', { name: 'Model: Claude Sonnet' })).toHaveText('Sonnet');
  await page.keyboard.press(`${process.platform === 'darwin' ? 'Alt+Meta' : 'Alt+Control'}+2`);
  await expect(section.getByRole('button', { name: 'Model: DeepSeek v4.1 Pro' })).toHaveText('Pro');
});
