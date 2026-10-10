import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  for (const input of ['click', 'shortcut']) {
    test(`${theme}: ${input} returns exactly one window focus step`, async ({ page }) => {
      await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { value: 'MacIntel' }));
      await page.goto('/tests/ui/support/in-tab-back.html');
      await page.evaluate(async theme => {
        const path = '/tests/ui/support/in-tab-back-harness.tsx';
        const { mountInTabBack } = await import(path);
        mountInTabBack(theme);
      }, theme);
      const nav = page.getByRole('navigation', { name: 'Breadcrumb' });
      await expect(nav).toHaveText('Config stack/Update fixtures');
      if (input === 'click') await nav.getByRole('button', { name: 'Config stack' }).click();
      else await page.keyboard.press('Meta+BracketLeft');
      // The previous focus was a different tab, not the task's structural parent.
      await expect(page.locator('main')).toHaveAttribute('data-destination', 'other');
      await expect(page.locator('main')).toHaveAttribute('data-cursor', '1');
      await expect(page.locator('main')).toHaveAttribute('data-steps', '3');
      await page.keyboard.press('Meta+BracketRight');
      await expect(page.locator('main')).toHaveAttribute('data-destination', 'task');
      await expect(page.locator('main')).toHaveAttribute('data-cursor', '2');
      await page.keyboard.press('Meta+Shift+BracketLeft');
      await expect(page.locator('main')).toHaveAttribute('data-cursor', '2');
      await page.keyboard.press('Meta+BracketLeft');
      await page.keyboard.press('Meta+BracketLeft');
      await expect(page.locator('main')).toHaveAttribute('data-cursor', '0');
      await expect(nav.getByRole('button')).toHaveCount(0);
      await page.keyboard.press('Meta+BracketLeft');
      await expect(page.locator('main')).toHaveAttribute('data-destination', 'chat');
    });
  }
  test(`${theme}: task route records drill-in and parent returns locally without a provider`, async ({ page }) => {
    await page.goto('/tests/ui/support/in-tab-back.html');
    await page.evaluate(async theme => {
      const path = '/tests/ui/support/in-tab-back-harness.tsx';
      const { mountLocalTaskBack } = await import(path);
      mountLocalTaskBack(theme);
    }, theme);
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText('Config stack/Update fixtures');
    await expect(page.locator('main')).toHaveAttribute('data-route', 'task');
    await expect(page.locator('main')).toHaveAttribute('data-back', '[""]');
    await page.getByRole('button', { name: 'Config stack' }).click();
    await expect(page.locator('main')).toHaveAttribute('data-route', 'chat');
  });
}
