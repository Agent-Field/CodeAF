import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => { await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme); });

    test('matches the measured design', async ({ page }) => {
      await page.goto('/');
      const card = page.getByRole('region', { name: 'Plan' });
      await expect(card).toHaveCSS('border-radius', '14px');
      await expect(card).toHaveCSS('padding', '14px 16px');
      await expect(card).toHaveCSS('max-width', '560px');
      const head = card.locator('.plan-card-head');
      await expect(head).toHaveText('Plan · reaches beyond this chat');
      await expect(head).toHaveCSS('font-size', '13px');
      await expect(head).toHaveCSS('font-weight', '600');
      const step = card.locator('.plan-card-step').first();
      await expect(step).toHaveCSS('font-size', '13px');
      await expect(step.locator('.plan-card-target')).toHaveText('Launch post');
      await expect(step.locator('.plan-card-target')).toHaveCSS('font-weight', '500');
      const glyph = step.locator('.app-icon');
      expect((await glyph.boundingBox())!.width).toBe(13);
      await expect(card.locator('.plan-card-step [data-icon]')).toHaveCount(3);
      expect(await card.locator('.plan-card-step [data-icon]').evaluateAll(n => n.map(e => e.getAttribute('data-icon'))))
        .toEqual(['cornerDownRight', 'messagesSquare', 'bookmark']);
      for (const name of ['Go', 'Edit', 'Cancel']) {
        const b = card.getByRole('button', { name, exact: true });
        await expect(b).toHaveCSS('height', '28px');
        await expect(b).toHaveCSS('border-radius', '8px');
      }
      await expect(card.getByRole('button', { name: 'Cancel' })).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    });

    test('Go asks once, Cancel only dismisses', async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'Go', exact: true }).click();
      await expect(page.locator('#log')).toHaveText('go');
      await page.getByRole('button', { name: 'Cancel' }).click();
      await expect(page.locator('#log')).toHaveText('go\ncancel');
    });

    test('Edit removes and rewrites steps, and Go runs the edited list', async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'Edit', exact: true }).click();
      await page.getByRole('button', { name: 'Remove step 2' }).click();
      await page.getByRole('textbox', { name: 'Step 2' }).fill('Remember: run it twice');
      await expect(page.getByRole('button', { name: /Add/ })).toHaveCount(0);
      await page.getByRole('button', { name: 'Go', exact: true }).click();
      await expect(page.locator('#log')).toHaveText('edit:Hold Launch post until the v1 suite passes|Remember: run it twice\ngo');
    });

    test('a deleted target reads as skipped', async ({ page }) => {
      await page.goto('/?mode=gone');
      await expect(page.getByText('Launch post was deleted, so this step was skipped')).toBeVisible();
    });
  });
}
