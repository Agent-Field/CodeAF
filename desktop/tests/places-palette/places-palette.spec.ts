import { test, expect, type Page } from '@playwright/test';

// ⌘P against the designer's measurements (Places 6c): sheet 620x560 on a .18 scrim, 48px field, 34px rows, 10px swatch.
async function open(page: Page, theme: 'light' | 'dark' = 'light', query = '') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto(`/${query}`);
  await page.locator('#opener').click();
  await page.getByRole('combobox').waitFor();
}
const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');
const field = (page: Page) => page.getByRole('combobox');

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test('geometry: 620x560 sheet, 48px field, 34px rows, count, scrim .18 light, .4 dark', async ({ page }) => {
      await open(page, theme);
      // Layout sizes, not bounding boxes: the sheet scales in on open and a box would be read mid-animation.
      const size = (selector: string) => page.locator(selector).first().evaluate(el => [(el as HTMLElement).offsetWidth, (el as HTMLElement).offsetHeight]);
      expect(await size('dialog.palette')).toEqual([620, 560]);
      expect((await size('.palette-search'))[1]).toBe(48);
      expect((await size('.palette-row'))[1]).toBe(34);
      await expect(field(page)).toHaveAttribute('placeholder', 'Go to a place, or create one');
      await expect(page.locator('.palette-count')).toHaveText('5 places');
      await expect(page.getByRole('group', { name: 'Recent' }).getByRole('option')).toHaveCount(3);
      expect(await page.locator('dialog.palette').evaluate(el => getComputedStyle(el, '::backdrop').backgroundColor)).toContain(theme === 'light' ? '0.18' : '0.4');
      await expect(page.locator('.palette-hints')).toContainText('open in new window');
    });

    test('keys: ↑↓ rove, → expands, ← collapses, ↵ opens, ⌘↵ new window', async ({ page }) => {
      await open(page, theme);
      await field(page).press('ArrowDown');
      await expect(page.getByRole('option', { selected: true })).toContainText('Software');
      await field(page).press('Control+Enter');
      await expect(log(page)).toHaveText(['window software']);
      await field(page).press('Enter');
      await expect(log(page)).toHaveText(['window software', 'open software']);
    });

    test('tree: ←/→ close and open a branch', async ({ page }) => {
      await open(page, theme);
      const all = page.locator('.palette-scroll');
      const rowsBefore = await all.getByRole('option').count();
      for (let at = 0; at < 3; at++) await field(page).press('ArrowDown');
      await expect(page.getByRole('option', { selected: true })).toContainText('codeaf');
      await field(page).press('ArrowLeft');
      await expect(all.getByRole('option')).toHaveCount(rowsBefore - 2);
      await field(page).press('ArrowRight');
      await expect(all.getByRole('option')).toHaveCount(rowsBefore);
    });

    test('search: matches rank, no match offers Create, ⌘N creates with the typed name', async ({ page }) => {
      await open(page, theme);
      await field(page).fill('q3');
      await expect(page.getByRole('option')).toHaveCount(1);
      await field(page).fill('zzzz');
      await expect(page.getByRole('option')).toHaveText(/Create “zzzz”/);
      await field(page).press('Control+n');
      await expect(log(page)).toHaveText(['create zzzz']);
    });

    test('Esc closes and focus returns to the opener', async ({ page }) => {
      await open(page, theme);
      await field(page).press('Escape');
      await expect(page.locator('dialog.palette')).not.toHaveAttribute('open', '');
      await expect(page.locator('#opener')).toBeFocused();
    });

    test('narrow: ≤600 the sheet is full width', async ({ page }) => {
      await page.setViewportSize({ width: 480, height: 800 });
      await open(page, theme);
      expect(await page.locator('dialog.palette').evaluate(el => (el as HTMLElement).offsetWidth)).toBe(480);
    });

    test('200 places: only a window of rows is in the DOM', async ({ page }) => {
      await open(page, theme, '?big');
      await page.getByRole('option', { name: /Reports/ }).locator('.palette-disclosure').click();
      expect(await page.locator('.palette-scroll [role=option]').count()).toBeLessThan(60);
    });
  });
}
