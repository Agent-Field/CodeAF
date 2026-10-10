import { test, expect } from '@playwright/test';
import { tokenColorIn } from './contracts';

for (const theme of ['light', 'dark'] as const) {
 test(`measured end card and return in ${theme}`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto('/tests/end-card/harness/');
  const card = page.getByRole('region', { name: "You're clear" });
  await expect(card.getByRole('status')).toHaveText('5 answered. 4 tasks are running.');
  const measured = await card.evaluate(el => {
   const css = getComputedStyle(el);
   const title = getComputedStyle(el.querySelector('.nextup-end-card-title')!);
   const note = getComputedStyle(el.querySelector('.nextup-end-card-note')!);
   const glyph = el.querySelector('.app-icon')!;
   const button = el.querySelector('button')!;
   return { height: el.getBoundingClientRect().height, shadow: css.boxShadow, surface: css.backgroundColor, buttonFill: getComputedStyle(button).backgroundColor, radius: css.borderRadius, padding: css.padding, gap: css.gap,
    titleSize: title.fontSize, titleWeight: title.fontWeight, noteSize: note.fontSize,
    glyphWidth: glyph.getBoundingClientRect().width, glyphColor: getComputedStyle(glyph).color,
    buttonHeight: button.getBoundingClientRect().height, buttonRadius: getComputedStyle(button).borderRadius,
    rightInset: el.getBoundingClientRect().right - button.getBoundingClientRect().right,
    ink3: css.getPropertyValue('--ink-3').trim(), noteColor: note.color, ink2: css.getPropertyValue('--ink-2').trim() };
  });
  expect(measured).toMatchObject({ height: 68, radius: '18px', padding: '18px 20px', gap: '14px', titleSize: '14px', titleWeight: '600', noteSize: '12px', glyphWidth: 18, buttonHeight: 30, buttonRadius: '8px', rightInset: 20 });
  expect(measured.glyphColor).toBe(await tokenColorIn(card, 'ink-3'));
  expect(measured.noteColor).toBe(await tokenColorIn(card, 'ink-2'));
  expect(measured.surface).toBe(await tokenColorIn(card, 'surface'));
  await expect(card.getByRole('button')).toHaveCSS('background-color', await tokenColorIn(card, 'accent'));
  expect(measured.shadow).not.toBe('none');
  const button = card.getByRole('button', { name: 'Back to Config stack' });
  await page.keyboard.press('Tab');
  await expect(button).toBeFocused();
  expect(await button.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');
  await button.click();
  await expect(page.getByText('Returned to Config stack')).toBeVisible();
 });
}

test('Escape returns, including before the button is focused', async ({ page }) => {
 await page.goto('/tests/end-card/harness/');
 await expect(page.getByRole('region')).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(page.getByText('Returned to Config stack')).toBeVisible();
});

for (const mode of ['unknown', 'zero', 'one']) {
 test(`honest ${mode} counts`, async ({ page }) => {
  await page.goto(`/tests/end-card/harness/?mode=${mode}`);
  if (mode === 'one') await expect(page.getByRole('status')).toHaveText('5 answered. 1 task is running.');
  else await expect(page.getByRole('status')).toHaveCount(0);
 });
}

for (const theme of ['light', 'dark'] as const) test(`narrow widths keep the return action reachable in ${theme}`, async ({ page }) => {
 await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
 await page.emulateMedia({ reducedMotion: 'reduce' });
 for (const width of [320, 480, 600, 800, 1200]) {
  await page.setViewportSize({ width, height: 560 });
  await page.goto('/tests/end-card/harness/');
  const button = page.getByRole('button');
  await expect(button).toBeVisible();
  const bounds = await button.boundingBox();
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
 }
});
