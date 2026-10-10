import { test, expect } from '@playwright/test';
import { tokenColor } from './contracts';

for (const theme of ['light', 'dark']) {
 for (const width of [320, 1200]) {
  test(`PR-BTN-9: control is 28px and tray is 30px in ${theme} at ${width}px`, async ({ page }) => {
   await page.setViewportSize({ width, height: 560 });
   await page.goto('/tests/button-size/index.html');
   await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
   for (const variant of ['primary', 'raised', 'quiet', 'ghost', 'danger']) {
    for (const [label, height, size] of [['Default', 28, 'control'], ['Control', 28, 'control'], ['Tray', 30, 'tray']] as const) {
     const button = page.getByRole('button', { name: `${label} ${variant}`, exact: true });
     await expect(button).toHaveAttribute('data-size', size);
     await expect(button).toHaveCSS('min-height', `${height}px`);
     expect((await button.boundingBox())!.height).toBe(height);
     await expect(button).not.toHaveAttribute('size');
    }
   }
   await expect(page.getByRole('button', { name: 'Disabled tray' })).toBeDisabled();
   await expect(page.getByRole('button', { name: 'Loading tray' })).toBeDisabled();
   await expect(page.getByRole('button', { name: 'Loading tray' })).toHaveAttribute('aria-busy', 'true');
   const answer = page.getByRole('button', { name: 'Answer', exact: true });
   await answer.focus();
   await page.keyboard.press('Enter');
   await expect(page.getByRole('button', { name: 'Answered' })).toBeFocused();
   expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
 }
}

for (const theme of ['light', 'dark'] as const) {
 test(`PR-TAG-1: Suggested tag is transparent ink-3 in ${theme}`, async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 560 });
  await page.goto('/tests/tag-plain/index.html');
  await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
  const tag = page.getByText('Suggested', { exact: true });
  await expect(tag).toHaveAttribute('data-tone', 'plain');
  await expect(tag).toHaveCSS('color', await tokenColor(page, 'ink-3'));
  await expect(tag).toHaveCSS('font-size', '11px');
  await expect(tag).toHaveCSS('font-weight', '500');
  const paint = await tag.evaluate(element => {
   const style = getComputedStyle(element);
   return { backgroundColor: style.backgroundColor, padding: style.padding, borderRadius: style.borderRadius };
  });
  expect(paint.backgroundColor === 'rgba(0, 0, 0, 0)' || paint.backgroundColor === 'transparent').toBe(true);
  expect(paint.padding).toBe('0px');
  expect(paint.borderRadius).toBe('0px');
 });
}
