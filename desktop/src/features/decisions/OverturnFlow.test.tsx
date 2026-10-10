import { test, expect, type Page } from '@playwright/test';

const ROUTE = '**/api/engine/decisions/d1/overturn';

// Records every body the card posts to the overturn route.
async function spy(page: Page, status = 200) {
  const bodies: unknown[] = [];
  await page.route(ROUTE, route => { bodies.push(route.request().postDataJSON()); return route.fulfill({ status, body: '{}' }); });
  return bodies;
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => { await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme); });

    test('names the dependents and preselects nothing', async ({ page }) => {
      await page.goto('/');
      await expect(page.getByText('2 tasks used this')).toBeVisible();
      for (const name of ['Notify them', 'Pause them', 'Leave them']) await expect(page.getByRole('radio', { name })).toHaveAttribute('aria-checked', 'false');
    });

    test('sends the chosen handling to the overturn route', async ({ page }) => {
      const bodies = await spy(page);
      await page.goto('/');
      await page.getByRole('radio', { name: 'Pause them' }).click();
      await expect(page.getByRole('radio', { name: 'Notify them' })).toHaveAttribute('aria-checked', 'false');
      await page.getByRole('button', { name: 'Overturn' }).click();
      await expect(page.getByRole('status')).toContainText('Decision overturned.');
      expect(bodies).toEqual([{ choice: 'pause' }]);
    });

    test('no choice means no cascade', async ({ page }) => {
      const bodies = await spy(page);
      await page.goto('/');
      await page.getByRole('button', { name: 'Overturn' }).click();
      await expect(page.getByRole('status')).toContainText('Decision overturned.');
      expect(bodies).toEqual([{}]);
    });

    test('Undo in the toast takes the overturn back', async ({ page }) => {
      await spy(page);
      let undone = 0;
      await page.route('**/overturn/undo', route => { undone++; return route.fulfill({ status: 200, body: '{}' }); });
      await page.goto('/');
      await page.getByRole('button', { name: 'Overturn' }).click();
      await page.getByRole('button', { name: 'Undo' }).click();
      await expect.poll(() => undone).toBe(1);
    });

    test('a refusal stays on the card', async ({ page }) => {
      await spy(page, 500);
      await page.goto('/');
      await page.getByRole('button', { name: 'Overturn' }).click();
      await expect(page.getByRole('alert')).toHaveText('The engine refused.');
    });

    test('without dependents there is nothing to choose', async ({ page }) => {
      await page.goto('/?dependents=0');
      await expect(page.getByRole('radiogroup')).toHaveCount(0);
      await expect(page.getByText(/used this/)).toHaveCount(0);
    });
  });
}
