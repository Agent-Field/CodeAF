import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

for (const theme of ['light', 'dark']) {
  test(`d5-pl-test-home: ${theme} Home is first, never closes, and All places closes`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { initial: { entries: [] }, turns: [] });
    await installMockPlaces(page, { places: [{ name: 'Reading', pinned: true }, { name: 'Garden', pinned: true }] });
    await page.goto('/');
    await page.getByRole('navigation', { name: 'Places' }).getByRole('button', { name: 'Reading', exact: true }).click();
    const home = page.locator('.home-tab');
    const tab = home.getByRole('tab');
    const strip = page.locator('.workspace-tabstrip');
    await expect(strip.getByRole('tab').first()).toHaveText('Reading');
    await expect(tab).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('.home-page')).toBeVisible();
    await expect(home.getByRole('button', { name: /^Close/ })).toHaveCount(0);
    const geometry = await home.evaluate(el => {
      const line = el.closest('.home-tab-slot')!.querySelector('.home-tab-hairline')!;
      const style = getComputedStyle(el);
      return { height: el.getBoundingClientRect().height, radius: style.borderRadius,
        lineWidth: line.getBoundingClientRect().width, lineHeight: line.getBoundingClientRect().height,
        first: el.closest('.home-tab-slot') === el.closest('.workspace-tabstrip')!.firstElementChild };
    });
    expect(geometry).toEqual({ height: 30, radius: '8px', lineWidth: 1, lineHeight: 16, first: true });
    const primary = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
    await page.keyboard.press(`${primary}+w`);
    await expect(tab).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press(`${primary}+t`);
    await expect(strip.getByRole('tab')).toHaveCount(2);
    await strip.getByRole('tab').last().focus();
    await page.keyboard.press('ArrowLeft');
    await expect(tab).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(strip.getByRole('tab').last()).toBeFocused();
    await page.keyboard.press(`${primary}+w`);
    await expect(strip.getByRole('tab')).toHaveCount(1);
    await page.keyboard.press(`${primary}+Shift+KeyP`);
    const root = strip.getByRole('tab').filter({ hasText: 'All places' });
    await expect(root).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('heading', { level: 1, name: 'All places', exact: true })).toBeVisible();
    await page.keyboard.press(`${primary}+Shift+KeyP`);
    await expect(root).toHaveCount(2);
    await page.getByRole('navigation', { name: 'Places' }).getByRole('button', { name: 'Garden', exact: true }).click();
    await expect(strip.getByRole('tab').first()).toHaveText('Garden');
    await page.getByRole('navigation', { name: 'Places' }).getByRole('button', { name: 'Reading', exact: true }).click();
    await expect(root).toHaveCount(2);
    await root.last().click();
    await page.keyboard.press(`${primary}+w`);
    await expect(root).toHaveCount(1);
    await page.keyboard.press(`${primary}+w`);
    await expect(root).toHaveCount(0);
    await expect(tab).toHaveAttribute('aria-selected', 'true');
    await page.reload();
    await expect(strip.getByRole('tab').first()).toHaveText('Reading');
    await expect(page.locator('.home-tab-hairline')).toHaveCount(1);
    for (const width of [320, 480, 600, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      await expect(home).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
    }
    // Hiding the rail makes the dedicated Home button own the switcher.
    await page.setViewportSize({ width: 1200, height: 560 });
    await page.getByRole('button', { name: 'Hide sidebar', exact: true }).click();
    await expect(home).toHaveAttribute('data-switcher', 'true');
    await tab.click();
    await expect(page.getByRole('menu', { name: 'Place switcher' })).toBeVisible();
    await page.keyboard.press('Escape');
  });
}
