import { test, expect } from '@playwright/test';

// SH-OQ4 and SH-OQ14: middle-click closes an ordinary tab, and Now opens no menu.
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

test('middle-click closes an unpinned tab and leaves a pinned one', async ({ page }) => {
  const state = {
    tabs: [
      { id: 'pin', title: 'Pinned note', draft: '', pinned: true, kind: 'conversation' },
      { id: 'solo', title: 'Solo note', draft: '', pinned: false, kind: 'conversation' },
    ],
    groups: [], closed: [], activeId: 'pin', nextNumber: 3, recentIds: ['pin', 'solo'],
  };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
  await page.goto('/');
  const solo = page.getByRole('tab', { name: 'Solo note' });
  await expect(solo).toBeVisible();
  await solo.click({ button: 'middle' });
  await expect(solo).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Pinned note' })).toBeVisible();
});

test('right-click on Now opens no menu', async ({ page }) => {
  await page.goto('/');
  const now = page.getByRole('button', { name: 'Now', exact: true });
  await expect(now).toBeVisible();
  await now.click({ button: 'right' });
  await expect(page.getByRole('menu')).toHaveCount(0);
});
