import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => { await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme); });

    test('matches the measured design', async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'Why?' }).click();
      const card = page.getByRole('dialog', { name: 'Decided automatically' });
      await expect(card).toHaveCSS('width', '360px');
      await expect(card).toHaveCSS('border-radius', '12px');
      await expect(card).toHaveCSS('padding', '14px 16px');
      await expect(card).toHaveCSS('row-gap', '10px');
      await expect(card.locator('.why-title')).toHaveCSS('font-weight', '600');
      await expect(card.locator('.why-facts')).toHaveCSS('font-size', '12px');
      await expect(card.locator('.why-facts')).toHaveCSS('column-gap', '14px');
      await expect(card.locator('.why-fact')).toHaveText(['ByConfig parser', 'BecauseYou allowed go test here 6 times. Reversible.', 'Sure97%']);
      await expect(card.locator('.why-next')).toHaveCSS('border-top-width', /^(0\.5|1)px$/);
      for (const name of ['Always ask me', 'Fine, keep deciding', 'Overturn', 'Open the decision']) {
        const b = card.getByRole(name.startsWith('Always') || name.startsWith('Fine') ? 'radio' : 'button', { name });
        await expect(b).toHaveCSS('height', '28px');
        await expect(b).toHaveCSS('border-radius', '8px');
      }
      expect(await card.evaluate(e => getComputedStyle(e).animationName)).toBe('popover-enter');
      expect(await card.evaluate(e => getComputedStyle(e).transformOrigin.split(' ').map(parseFloat))).toEqual([0, 0]);
    });

    test('Next time… is one choice at a time and reports it', async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'Why?' }).click();
      const ask = page.getByRole('radio', { name: 'Always ask me' });
      const keep = page.getByRole('radio', { name: 'Fine, keep deciding' });
      await expect(ask).toHaveAttribute('aria-checked', 'false');
      await expect(keep).toHaveAttribute('aria-checked', 'false');
      await ask.click();
      await keep.click();
      await expect(ask).toHaveAttribute('aria-checked', 'false');
      await expect(keep).toHaveAttribute('aria-checked', 'true');
      await expect(page.locator('#log')).toHaveText('next:ask\nnext:keep');
    });

    test('Overturn and Open the decision close the card and ask once', async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'Why?' }).click();
      await page.getByRole('button', { name: 'Overturn' }).click();
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await page.getByRole('button', { name: 'Why?' }).click();
      await page.getByRole('button', { name: 'Open the decision' }).click();
      await expect(page.locator('#log')).toHaveText('overturn\nopen');
    });

    test('Escape closes and focus returns to Why?', async ({ page }) => {
      await page.goto('/');
      const why = page.getByRole('button', { name: 'Why?' });
      await why.click();
      await expect(page.getByRole('button', { name: 'Overturn' })).toBeFocused();
      await page.keyboard.press('Escape');
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expect(why).toBeFocused();
    });

    test('unknown facts render as nothing', async ({ page }) => {
      await page.goto('/?mode=bare');
      await page.getByRole('button', { name: 'Why?' }).click();
      await expect(page.locator('.why-facts')).toHaveCount(0);
      await expect(page.getByText('0%')).toHaveCount(0);
    });
  });
}
