import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  test(`${theme}: root Home matches Places 8c geometry and opens real rows`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=root');
    // The live shell owns graphite; the isolated Home harness supplies the same inherited palette.
    await page.evaluate(() => document.body.dataset.tint = 'graphite');
    await expect(page.getByRole('heading', { level: 1, name: 'All places' })).toBeVisible();
    await expect(page.getByTestId('all-places-count')).toHaveText('5 at the top level · 58 in all');
    await expect(page.locator('.all-places-search')).toHaveCSS('width', '240px');
    await expect(page.getByRole('searchbox', { name: 'Search places' })).toHaveCSS('height', '30px');
    await expect(page.locator('.home-places > .type-section-label')).toHaveCount(0);
    await expect(page.locator('.places-tile-grid').first()).toHaveCSS('grid-template-columns', '149px 149px 149px 149px');
    await expect(page.locator('[data-place-id="pl_codeaf"]')).toHaveCSS('height', '108px');
    await expect(page.getByRole('button', { name: 'New place', exact: true })).toHaveCSS('height', '84px');
    await expect(page.getByRole('heading', { name: 'Not in any place · 38' })).toBeVisible();
    await page.locator('[data-place-id="pl_reports"] button').click();
    await expect(page.getByRole('list', { name: 'Callback log' }).locator('li').last()).toHaveText('goTo:pl_reports');
    await page.getByRole('button', { name: /Quick regex for semver/ }).click();
    await expect(page.getByRole('list', { name: 'Callback log' }).locator('li').last()).toHaveText('openChat:chat_regex');
  });

  test(`${theme}: search filters nested places by name and path and unplaced chats by title`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=root');
    const search = page.getByRole('searchbox', { name: 'Search places' });
    await search.fill(' READING ');
    await expect(page.locator('.places-tile[data-mode="place"]')).toHaveCount(2);
    await expect(page.locator('[data-place-id="pl_papers"]')).toContainText('in Reading');
    await expect(page.locator('.home-chats')).toHaveCount(0);
    await search.fill('PAPERS');
    await expect(page.locator('.places-tile[data-mode="place"]')).toHaveCount(1);
    await search.fill('regex');
    await expect(page.locator('.places-tile')).toHaveCount(0);
    await expect(page.locator('.places-chat-row')).toHaveCount(1);
    await expect(page.getByRole('heading', { name: 'Not in any place · 38' })).toBeVisible();
    await expect(page.getByText('No place called “regex”.')).toHaveCount(0);
    await search.press('Escape');
    await expect(search).toHaveValue('');
    await expect(page.locator('.places-tile')).toHaveCount(6);
    await expect(page.locator('.places-chat-row')).toHaveCount(2);
  });
}

for (const width of [320, 480, 600, 800, 1200]) {
  test(`root Home keeps search and tiles reachable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 560 });
    for (const theme of ['light', 'dark']) {
      await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
      await page.goto('/?scenario=root');
      await expect(page.getByRole('searchbox', { name: 'Search places' })).toBeVisible();
      await expect(page.locator('[data-place-id="pl_codeaf"]')).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
      expect(await page.locator('.home-column').evaluate(el => el.scrollWidth - el.clientWidth)).toBe(0);
    }
  });
}
