import { expect, test } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  test(`${theme}: measured rows, sources and replacement styling`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await expect(page.getByText('What Marketing knows · 4')).toBeVisible();
    await expect(page.locator('[data-knows-row]')).toHaveCount(3);
    await expect(page.getByText('You said in Launch post · Tue')).toBeVisible();
    await expect(page.getByText('Replaced Tue · kept for 7 days')).toBeVisible();
    const row = page.locator('[data-knows-row="a"]');
    await row.hover();
    expect((await row.boundingBox())!.height).toBe(46);
    expect(await row.evaluate(el => { const s = getComputedStyle(el); return { minHeight: s.minHeight, padding: s.padding, radius: s.borderRadius }; })).toEqual({ minHeight: '38px', padding: '4px 12px', radius: '9px' });
    const edit = page.getByRole('button', { name: 'Edit Run the v1 suite before any launch goes out', exact: true });
    expect(await edit.evaluate(el => { const s = getComputedStyle(el); const glyph = el.querySelector('svg')!; return { width: s.width, height: s.height, radius: s.borderRadius, glyph: getComputedStyle(glyph).width }; })).toEqual({ width: '24px', height: '24px', radius: '6px', glyph: '12px' });
    expect(await page.locator('[data-replaced]').evaluate(el => getComputedStyle(el).textDecorationLine)).toBe('line-through');
    await expect(edit).toBeVisible();
  });
}

test('Enter adds real returned lines, edits cancel or save, Delete removes with Undo', async ({ page }) => {
  await page.goto('/');
  const field = page.getByRole('textbox', { name: 'Add something Marketing should know' });
  await field.fill('  Keep the copy short  '); await field.press('Enter');
  await expect(page.getByText('Keep the copy short', { exact: true })).toBeVisible();
  await expect(field).toHaveValue('');
  await expect(field).toBeFocused();
  await expect(page.getByText('you added', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit Keep the copy short', exact: true }).click();
  const editor = page.getByRole('textbox', { name: 'Edit knowledge' });
  await editor.fill('cancel this'); await editor.press('Escape');
  await expect(page.getByText('Keep the copy short', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit Keep the copy short', exact: true }).click();
  await editor.fill('Use concise copy'); await editor.press('Enter');
  const row = page.locator('[data-knows-row]').filter({ hasText: 'Use concise copy' });
  await expect(row).toBeFocused(); await row.press('Delete');
  await expect(page.getByText('Use concise copy', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Undo', exact: true }).last().click();
  await expect(page.getByText('Use concise copy', { exact: true })).toBeVisible();
});

test('All reveals every row; old unused knowledge offers Yes and Remove', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'All', exact: true }).click();
  await expect(page.locator('[data-knows-row]')).toHaveCount(4);
  await expect(page.getByText('still true?', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Yes', exact: true }).click();
  await expect(page.getByText('still true?', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('status', { name: 'Confirmed line' })).toHaveText('d');
  const first = page.locator('[data-knows-row]').first();
  await first.focus(); await first.press('ArrowDown');
  await expect(page.locator('[data-knows-row]').nth(1)).toBeFocused();
});

test('empty and narrow layouts, failed writes preserve the draft, read-only has no mutations', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/?empty');
  await expect(page.getByText('What Marketing knows', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'All', exact: true })).toHaveCount(0);
  const field = page.getByRole('textbox', { name: 'Add something Marketing should know' });
  await field.fill('refuse'); await field.press('Enter');
  await expect(page.getByRole('alert')).toHaveText('The engine refused this line.');
  await expect(field).toHaveValue('refuse');
  await field.fill(' '); await field.press('Enter');
  await expect(page.locator('[data-knows-row]')).toHaveCount(0);
  await page.goto('/?readonly');
  await expect(page.getByRole('textbox')).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Edit / })).toHaveCount(0);
  await page.locator('[data-knows-row]').first().focus(); await page.keyboard.press('Delete');
  await expect(page.locator('[data-knows-row]')).toHaveCount(3);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

for (const theme of ['light', 'dark']) {
  test(`${theme}: responsive rows and keyboard focus with reduced motion`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/');
    await page.getByRole('button', { name: 'All', exact: true }).click();
    for (const width of [320, 480, 600, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const rows = page.locator('[data-knows-row]');
      for (let i = 0; i < 4; i++) {
        await rows.nth(i).focus();
        await page.keyboard.press('Tab');
        if (i === 3) {
          await expect(page.getByRole('button', { name: 'Yes', exact: true })).toBeFocused();
          await page.keyboard.press('Tab');
          await expect(page.getByRole('button', { name: 'Remove', exact: true })).toBeFocused();
          await page.keyboard.press('Tab');
        }
        await expect(page.getByRole('button', { name: /^Edit / }).nth(i)).toBeFocused();
      }
    }
    await page.getByRole('button', { name: 'Remove', exact: true }).click();
    await expect(page.getByText('Use plain words', { exact: true })).toHaveCount(0);
    await page.getByRole('button', { name: 'Undo', exact: true }).click();
    await expect(page.getByText('Use plain words', { exact: true })).toBeVisible();
  });
}
