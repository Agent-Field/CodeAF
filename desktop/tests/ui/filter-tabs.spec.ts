import { test, expect, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { expectAccessible, expectNoUnstyledControls, tokenColor } from './contracts';

async function open(page: Page, theme: 'light' | 'dark') {
 await page.emulateMedia({ colorScheme: theme });
 await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
 await page.goto('/?specimen=filter-tabs');
 await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
}

test('filter tabs match the pill metrics in light and dark', async ({ page }) => {
 for (const theme of ['light', 'dark'] as const) {
  await open(page, theme);
  const group = page.getByRole('radiogroup', { name: 'Filter tabs', exact: true });
  const selected = group.getByRole('radio', { name: 'All' });
  const rest = group.getByRole('radio', { name: 'Decisions' });
  const metrics = await selected.evaluate(element => {
   const style = getComputedStyle(element);
   const box = element.getBoundingClientRect();
   return { height: box.height, padding: style.padding, radius: style.borderRadius, size: style.fontSize, weight: style.fontWeight };
  });
  expect(Math.round(metrics.height)).toBe(parseFloat(design.foundation['filter-tab-height']));
  const pads = metrics.padding.split(' ');
  expect(pads[0]).toBe('0px');
  expect(pads[1]).toBe(design.foundation['filter-tab-pad']);
  expect(metrics.radius === design.foundation['radius-filter-tab'] || metrics.radius.startsWith(`${design.foundation['radius-filter-tab']} `)).toBe(true);
  await expect(group).toHaveCSS('gap', design.foundation['space-1']);
  expect(metrics.size).toBe('12px');
  expect(metrics.weight).toBe('500');
  await expect(selected).toHaveCSS('background-color', await tokenColor(page, 'field'));
  await expect(selected).toHaveCSS('color', await tokenColor(page, 'ink'));
  await expect(rest).toHaveCSS('color', await tokenColor(page, 'ink-2'));
  await expect(rest).toHaveCSS('font-weight', '400');
  expect(await rest.evaluate(element => getComputedStyle(element).backgroundColor)).toMatch(/rgba\(0, 0, 0, 0\)|transparent/);
  await rest.hover();
  await expect(rest).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
  await expect(rest).toHaveCSS('color', await tokenColor(page, 'ink-2'));
  await page.mouse.down();
  expect(await rest.evaluate(element => getComputedStyle(element).transitionDuration)).toContain('0.08s');
  await expect.poll(() => rest.evaluate(element => getComputedStyle(element).filter)).toContain('0.94');
  await page.mouse.up();
  await expect(rest).toHaveAttribute('aria-checked', 'true');
  await expect(rest).toHaveCSS('background-color', await tokenColor(page, 'field'));
  await expect(rest).toHaveCSS('color', await tokenColor(page, 'ink'));
  const disabled = page.getByRole('radiogroup', { name: 'Disabled filter tabs', exact: true }).getByRole('radio', { name: 'All' });
  await expect(disabled).toBeDisabled();
  await expect(disabled).toHaveCSS('opacity', design.foundation['opacity-control-disabled']);
  await expectNoUnstyledControls(page);
  await expectAccessible(page);
 }
});

test('filter tabs are one tab stop and arrows, Home and End select', async ({ page }) => {
 await open(page, 'light');
 const group = page.getByRole('radiogroup', { name: 'Filter tabs', exact: true });
 const all = group.getByRole('radio', { name: 'All' });
 const decisions = group.getByRole('radio', { name: 'Decisions' });
 const tasks = group.getByRole('radio', { name: 'Tasks' });
 const archived = group.getByRole('radio', { name: 'Archived' });
 const openTab = group.getByRole('radio', { name: 'Open' });
 const terminal = group.getByRole('radio', { name: 'Terminal' });
 await page.keyboard.press('Tab');
 await expect(all).toBeFocused();
 await expect(all).toHaveAttribute('aria-checked', 'true');
 expect(await all.evaluate(element => getComputedStyle(element).boxShadow)).toContain(await tokenColor(page, 'accent'));
 expect(await all.evaluate(element => getComputedStyle(element).boxShadow)).toContain(design.foundation['focus-ring-width']);
 await page.keyboard.press('Tab');
 await expect(page.getByRole('button', { name: 'After filter tabs' })).toBeFocused();
 await page.keyboard.press('Shift+Tab');
 await expect(all).toBeFocused();
 await page.keyboard.press('ArrowRight');
 await expect(decisions).toBeFocused();
 await expect(decisions).toHaveAttribute('aria-checked', 'true');
 await page.keyboard.press('ArrowDown');
 await expect(group.getByRole('radio', { name: 'Files' })).toBeFocused();
 await page.keyboard.press('ArrowRight');
 await expect(tasks).toBeFocused();
 await page.keyboard.press('ArrowRight');
 await expect(openTab).toBeFocused();
 await expect(archived).toHaveAttribute('aria-checked', 'false');
 await expect(archived).toBeDisabled();
 await page.keyboard.press('ArrowLeft');
 await expect(tasks).toBeFocused();
 await page.keyboard.press('End');
 await expect(terminal).toBeFocused();
 await expect(terminal).toHaveAttribute('aria-checked', 'true');
 await page.keyboard.press('ArrowRight');
 await expect(all).toBeFocused();
 await page.keyboard.press('ArrowLeft');
 await expect(terminal).toBeFocused();
 await page.keyboard.press('Home');
 await expect(all).toBeFocused();
 await expect(all).toHaveAttribute('aria-checked', 'true');
 await decisions.click();
 await expect(decisions).toBeFocused();
 expect(await decisions.evaluate(element => getComputedStyle(element).boxShadow)).toBe('none');
});

test('filter tabs scroll at 320px and mask only an edge that has more', async ({ page }) => {
 await open(page, 'dark');
 await page.setViewportSize({ width: 1200, height: 800 });
 const group = page.getByRole('radiogroup', { name: 'Filter tabs', exact: true });
 await expect(group).not.toHaveAttribute('data-fade-end');
 await expect(group).not.toHaveAttribute('data-fade-start');
 await page.setViewportSize({ width: 320, height: 800 });
 await expect.poll(async () => group.getAttribute('data-fade-end')).toBe('');
 await expect(group).not.toHaveAttribute('data-fade-start');
 const overflow = await group.evaluate(element => element.scrollWidth > element.clientWidth + 1);
 expect(overflow).toBe(true);
 const mask = await group.evaluate(element => {
  const style = getComputedStyle(element);
  const image = style.getPropertyValue('mask-image');
  const prefixed = style.getPropertyValue('-webkit-mask-image');
  return image && image !== 'none' ? image : prefixed;
 });
 expect(mask).toContain(design.foundation['edge-fade-inline']);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
 await group.getByRole('radio', { name: 'All' }).focus();
 await page.keyboard.press('End');
 await expect(group.getByRole('radio', { name: 'Terminal' })).toHaveAttribute('aria-checked', 'true');
 await expect.poll(async () => group.getAttribute('data-fade-start')).toBe('');
 await expect.poll(async () => group.getAttribute('data-fade-end')).toBeNull();
 const endMask = await group.evaluate(element => {
  const style = getComputedStyle(element);
  const image = style.getPropertyValue('mask-image');
  const prefixed = style.getPropertyValue('-webkit-mask-image');
  return image && image !== 'none' ? image : prefixed;
 });
 expect(endMask).toContain(design.foundation['edge-fade-inline']);
 await expectAccessible(page);
});
