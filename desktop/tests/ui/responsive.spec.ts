import { openAppearance, openPage } from './support/shell-navigation';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, expectThemedSurface } from './contracts';

async function expectNoHorizontalOverflow(page: Page) {
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 for (const container of await page.locator('.content-pane,.page-content,.sidebar-drawer[open]').all()) {
  expect(await container.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
 }
}
async function openNavigation(page: Page) {
 if (page.viewportSize()!.width <= design.breakpoints.small) {
  await page.getByRole('button', { name: 'Show sidebar' }).click();
  await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).toBeVisible();
 }
}

// One test per theme and width: the Design system specimen is large, so a single session over every width
// is slow in webkit and cannot run in parallel.
for (const theme of ['Light', 'Dark']) {
 for (const width of [320, 480, 600, design.nativeWindow.minWidth, 1200]) {
  test(`${theme}: layouts stay usable from 320px browser to native minimum at ${width}px`, async ({ page }) => {
   await page.goto('/');
   await openAppearance(page);
   await page.getByRole('option', { name: `${theme} appearance`, exact: true }).click();
   await page.setViewportSize({ width, height: width < design.nativeWindow.minWidth ? 480 : design.nativeWindow.minHeight });
   for (const name of ['Now', 'Activity', 'Design system']) {
    await openNavigation(page);
    if (name === 'Now') await page.getByRole('button', { name, exact: true }).click();
    else { await page.keyboard.press('Escape'); await openPage(page, name as 'Activity' | 'Design system'); }
    await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).not.toBeVisible();
    await expectNoHorizontalOverflow(page);
    await expectNoUnstyledControls(page);
    await expectAccessible(page);
    if (name === 'Design system') {
     const bottom = page.getByRole('button', { name: 'Open the new-tab field', exact: true });
     await bottom.scrollIntoViewIfNeeded();
     await bottom.click();
     await expect(page.getByRole('combobox', { name: 'Search or start' })).toBeFocused();
     await expectNoHorizontalOverflow(page);
     await openPage(page, 'Activity');
     await expect(page.locator('.page-title')).toHaveText('Activity');
    }
   }
  });
 }
}

test('narrow navigation traps focus, themes nested menus, dismisses, and preserves desktop preference', async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 480 });
 await page.goto('/');
 await expect(page.locator('.content-pane')).toHaveCSS('min-width', '0px');
 await expect(page.getByRole('button', { name: 'Now', exact: true })).not.toBeVisible();
 const show = page.getByRole('button', { name: 'Show sidebar' });
 await show.click();
 const drawer = page.getByRole('dialog', { name: 'Navigation', exact: true });
 await expect(drawer).toHaveCSS('opacity', '1');
 await expect(page.getByRole('button', { name: 'Hide sidebar' })).toBeFocused();
 await expectAccessible(page);
 // Native modals may allow browser-chrome focus, but background app controls stay inert.
 await page.locator('.workspace-tab-actions').getByRole('button', { name: 'New tab', exact: true, includeHidden: true }).evaluate(el => (el as HTMLElement).focus());
 await expect(page.getByRole('button', { name: 'Hide sidebar' })).toBeFocused();
 await page.keyboard.press('Escape');
 await expect(drawer).not.toBeVisible();
 await expect(show).toBeFocused();
 await openAppearance(page);
 await expectThemedSurface(page, page.getByRole('listbox'));
 await expect(drawer).not.toBeVisible();
 await expectAccessible(page);
 await page.getByRole('option', { name: 'Dark appearance' }).click();
 await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
 await expect(page.getByRole('listbox')).not.toBeVisible();
 await expect(page.getByRole('combobox', { name: 'Theme' })).toBeFocused();
 await page.keyboard.press('Escape');
 await expect(drawer).not.toBeVisible();
 await show.click();
 await page.mouse.click(300, 200);
 await expect(drawer).not.toBeVisible();
 await show.click();
 await page.keyboard.press('Control+k');
 await expect(drawer).not.toBeVisible();
 await expect(page.getByRole('combobox', { name: 'Search or start' })).toBeFocused();
 await expectAccessible(page);
 await page.setViewportSize({ width: 1200, height: 800 });
 await expect(page.getByRole('button', { name: 'Now', exact: true })).toBeVisible();
 await page.getByRole('button', { name: 'Hide sidebar' }).click();
 await page.setViewportSize({ width: 320, height: 480 });
 await show.click(); await openPage(page, 'Activity');
 await page.setViewportSize({ width: 1200, height: 800 });
 await expect(page.getByRole('button', { name: 'Now', exact: true })).not.toBeVisible();
 await expect(show).toBeVisible();
});

test('narrow drawer and new-tab field keep reduced motion and usable short-height scrolling', async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 320 });
 await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' });
 await page.goto('/');
 await openNavigation(page);
 await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).toHaveCSS('animation-duration', '0s');
 await page.keyboard.press('Escape');
 await openAppearance(page);
 await expectThemedSurface(page, page.getByRole('listbox'));
 await page.keyboard.press('Escape');
 await openPage(page, 'Design system');
 await page.getByRole('button', { name: 'Open the new-tab field', exact: true }).click();
 await expect(page.getByRole('combobox', { name: 'Search or start' })).toBeFocused();
 await expectNoHorizontalOverflow(page);
 await expectAccessible(page);
});
