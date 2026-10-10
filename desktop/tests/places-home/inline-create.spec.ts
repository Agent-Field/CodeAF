import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  test(`d5-pl-test-home: ${theme} inline create and cancel`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=root');
    const add = page.getByRole('button', { name: 'New place', exact: true });
    const field = page.getByRole('textbox', { name: 'Place name' });
    const choices = page.getByRole('radiogroup', { name: 'Tint' });
    const log = page.getByRole('list', { name: 'Callback log' }).locator('li');
    await add.click();
    await expect(field).toBeFocused();
    expect(await choices.getByRole('radio').evaluateAll(nodes => nodes.map(node => node.getAttribute('aria-label')))).toEqual(['Tide', 'Rose', 'Sage', 'Sand', 'Iris']);
    await expect(choices.getByRole('radio', { name: 'Tide' })).toHaveAttribute('aria-checked', 'true');
    await expect(page.locator('.places-tile[data-mode="creating"]')).toHaveCSS('height', '84px');
    await expect(choices).toHaveCSS('gap', '5px');
    await expect(choices.locator('.place-swatch').first()).toHaveCSS('width', '12px');
    await expect(page.locator('.places-tile-hint')).toHaveText('↵ create · Esc cancel');
    await field.fill('   ');
    await field.press('Enter');
    await expect(field).toBeVisible();
    await expect(log).toHaveCount(0);
    await field.fill('codeaf');
    await field.press('Enter');
    await expect(field).toHaveAttribute('aria-invalid', 'true');
    await expect(log).toHaveCount(0);
    await field.press('Escape');
    await expect(add).toBeFocused();
    await add.press('Enter');
    await choices.getByRole('radio', { name: 'Rose' }).click();
    await page.keyboard.press('Escape');
    await expect(add).toBeFocused();
    await add.press('Enter');
    await field.fill('  Talks  ');
    await field.press('Enter');
    await expect(log.last()).toHaveText('create:Talks:tide:root');
    await expect(field).toHaveCount(0);
    await add.click();
    await expect(choices.getByRole('radio', { name: 'Rose' })).toHaveAttribute('aria-checked', 'true');
    await choices.getByRole('radio', { name: 'Rose' }).press('ArrowRight');
    await expect(choices.getByRole('radio', { name: 'Sage' })).toBeFocused();
    await field.fill('Another');
    await field.press('Enter');
    await expect(log.last()).toHaveText('create:Another:sage:root');
  });
  test(`d5-pl-test-home: ${theme} current Home is the parent`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.route('**/api/engine/**', route => route.fulfill({ status: 404, json: { error: 'No section data' } }));
    await page.goto('/?scenario=place');
    await page.getByRole('button', { name: 'New place', exact: true }).click();
    const field = page.getByRole('textbox', { name: 'Place name' });
    await field.fill('Talks');
    await page.getByRole('radio', { name: 'Iris' }).click();
    await field.press('Enter');
    await expect(page.getByRole('list', { name: 'Callback log' }).locator('li').last()).toHaveText('create:Talks:iris:pl_codeaf');
  });
}

for (const width of [320, 600]) {
  test(`d5-pl-test-home: inline picker fits ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 560 });
    for (const theme of ['light', 'dark']) {
      await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
      await page.goto('/?scenario=first');
      await page.getByRole('button', { name: 'Name a place', exact: true }).click();
      await expect(page.getByRole('textbox', { name: 'Place name' })).toBeFocused();
      await expect(page.getByRole('radio', { name: 'Tide' })).toHaveAttribute('aria-checked', 'true');
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
      expect(await page.locator('.places-tile[data-mode="creating"]').evaluate(el => el.scrollWidth - el.clientWidth)).toBe(0);
    }
  });
}
