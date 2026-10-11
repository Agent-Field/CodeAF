import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  for (const width of [320, 480, 600, 800, 1200]) {
    test(`${theme} ${width}: pill, views and steering remain reachable`, async ({ page }) => {
      await page.setViewportSize({ width, height: 560 });
      await page.emulateMedia({ colorScheme: theme as 'light' | 'dark', reducedMotion: 'reduce' });
      await page.goto('/?specimen=places-guess');
      const pill = page.getByRole('combobox', { name: 'Change suggested place' });
      await expect(pill).toHaveText(/Desk app/);
      const measured = await pill.evaluate(element => {
        const rect = element.getBoundingClientRect(); const css = getComputedStyle(element);
        return { height: rect.height, font: css.fontSize, radius: css.borderRadius, width: document.documentElement.scrollWidth };
      });
      expect(measured).toEqual({ height: 24, font: '12px', radius: '6px', width });
      await pill.focus(); await page.keyboard.press('Enter');
      await page.getByRole('option', { name: '→ Papers', exact: true }).click();
      await expect(pill).toHaveText(/Papers/);
      await expect(pill).toBeFocused();
      await expect(page.getByText('also in Papers', { exact: true })).toBeVisible();
      const chip = page.getByRole('button', { name: 'by month', exact: true });
      await chip.click(); await expect(chip).toHaveAttribute('aria-pressed', 'true'); await expect(page.getByRole('status')).toHaveText('chat-one');
      await chip.click(); await expect(chip).toHaveAttribute('aria-pressed', 'false'); await expect(page.getByRole('status')).toHaveText('All chats');
      const field = page.getByRole('textbox', { name: 'Steer this place' });
      await field.fill('unknown wish'); await field.press('Enter');
      await expect(page.getByText("I did not understand that; try 'by month' or 'by project'", { exact: true })).toBeVisible();
      await expect(field).toHaveValue('unknown wish');
      await field.fill('by month'); await page.getByRole('button', { name: 'Apply', exact: true }).click();
      await expect(field).toHaveValue('');
      await expect(page.getByText("I did not understand that; try 'by month' or 'by project'", { exact: true })).toHaveCount(0);
    });
  }
}
