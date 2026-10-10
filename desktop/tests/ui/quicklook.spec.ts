import { test, expect, type Page } from '@playwright/test';

// The Quick Look primitive (C-OVL-4, F-MO-16, I-IKY-23): geometry, scrim, focus return, Space and Esc, narrow width, reduced motion.
async function mount(page: Page, theme: string) {
  await page.goto('/');
  await page.evaluate(async theme => {
    const path = '/tests/ui/fixtures/quicklook-mount.ts';
    const { mountQuickLook } = await import(path);
    mountQuickLook(theme);
  }, theme);
  await page.locator('#opener').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('dialog.quick-look')).toBeVisible();
  await page.waitForTimeout(300);
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme });

    test('quick look sheet: geometry and scrim', async ({ page }) => {
      await mount(page, scheme);
      const sheet = await page.locator('dialog.quick-look').evaluate(el => {
        const s = getComputedStyle(el); const r = el.getBoundingClientRect();
        const root = getComputedStyle(document.documentElement);
        return { width: r.width, radius: s.borderTopLeftRadius, shadow: s.boxShadow, bg: s.backgroundColor, canvas: root.getPropertyValue('--canvas'), scrim: getComputedStyle(el, '::backdrop').backgroundColor, sh3: root.getPropertyValue('--sh-3').trim() };
      });
      expect(sheet.width).toBe(420);
      expect(sheet.radius).toBe('14px');
      expect(sheet.shadow).not.toBe('none');
      expect(sheet.scrim).toMatch(/^(rgba|color)\(.*\)$|oklab|oklch/);
      const title = await page.locator('.quick-look-title').evaluate(el => { const s = getComputedStyle(el); return [s.fontSize, s.fontWeight]; });
      expect(title).toEqual(['17px', '600']);
      await expect(page.locator('.quick-look-hint')).toHaveText('Space to close');
      // The .5 line snaps to a device pixel, so the test reads the style and the token rather than the snapped width.
      const foot = await page.locator('.quick-look-foot').evaluate(el => ({ style: getComputedStyle(el).borderTopStyle, hairline: getComputedStyle(document.documentElement).getPropertyValue('--hairline').trim() }));
      expect(foot).toEqual({ style: 'solid', hairline: '.5px' });
      const alpha = scheme === 'light' ? 0.2 : 0.4;
      expect(sheet.scrim.replace(/\s/g, '')).toContain(String(alpha).replace('0.', '.'));
    });

    test('quick look sheet: Esc closes and focus returns to the opener', async ({ page }) => {
      await mount(page, scheme);
      await page.keyboard.press('Escape');
      await expect(page.locator('dialog.quick-look')).toBeHidden();
      await expect(page.locator('#opener')).toBeFocused();
    });

    test('quick look sheet: Space closes unless focus is in a text field', async ({ page }) => {
      await mount(page, scheme);
      await page.locator('#field').focus();
      await page.keyboard.type('a b');
      await expect(page.locator('dialog.quick-look')).toBeVisible();
      await expect(page.locator('#field')).toHaveValue('a b');
      await page.locator('.quick-look-title').click();
      await page.keyboard.press('Space');
      await expect(page.locator('dialog.quick-look')).toBeHidden();
      await expect(page.locator('#opener')).toBeFocused();
    });
  });
}

test('quick look sheet: stays 32px inside a 600px-wide window', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 800 });
  await mount(page, 'light');
  const width = await page.locator('dialog.quick-look').evaluate(el => el.getBoundingClientRect().width);
  expect(width).toBe(420);
  await page.setViewportSize({ width: 360, height: 800 });
  const narrow = await page.locator('dialog.quick-look').evaluate(el => el.getBoundingClientRect().width);
  expect(narrow).toBe(360 - 32);
});

test('quick look sheet: reduced motion fades without scaling', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  await page.evaluate(async () => { const path = '/tests/ui/fixtures/quicklook-mount.ts'; (await import(path)).mountQuickLook('light'); });
  await page.locator('#opener').click();
  const name = await page.locator('dialog.quick-look').evaluate(el => getComputedStyle(el).animationName);
  expect(name).toBe('quick-look-fade');
});
