import { test, expect, type Page } from '@playwright/test';
import { openPage } from './support/shell-navigation';
import { dismissCoveringToasts, emitState, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';

const url = 'https://pkg.go.dev/encoding/json#Decoder';
test.beforeEach(async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
});

// Only live-pane tests need native views; the Design system draws its states without IPC.
async function prepareNativePane(page: Page) {
  await installNativeWebMock(page);
  await seedWebTab(page);
}

for (const theme of ['light', 'dark']) {
  test(`${theme}: measured address geometry, selection, navigation and Escape`, async ({ page }) => {
    await prepareNativePane(page);
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.goto('/');
    await dismissCoveringToasts(page);
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, 'webpane1', { loading: false, canBack: true, canForward: false });
    const header = page.locator('.web-header');
    await expect(header).toHaveCSS('height', '48px');
    await expect(header).toHaveCSS('gap', '4px');
    await expect(header).toHaveCSS('padding', '0px 10px');
    for (const button of await header.getByRole('button').all()) {
      if (await button.isVisible() && !(await button.getAttribute('class'))?.includes('web-address')) {
        await expect(button).toHaveCSS('width', '28px');
        await expect(button).toHaveCSS('height', '28px');
        await expect(button).toHaveCSS('border-radius', '8px');
        expect(await button.getAttribute('aria-label')).toBeTruthy();
      }
    }
    const address = header.locator('.web-address');
    await expect(address).toHaveCSS('height', '30px');
    await expect(address).toHaveCSS('max-width', '520px');
    await expect(address).toHaveCSS('padding', '0px 12px');
    await expect(address).toHaveCSS('gap', '8px');
    await expect(address).toHaveCSS('font-size', '12px');
    await expect(address).toHaveCSS('border-radius', '8px');
    await expect(address.locator('.tab-monogram')).toHaveCSS('width', '14px');
    await expect(address.locator('.tab-monogram')).toHaveCSS('border-radius', '4px');
    await expect(header.getByRole('button', { name: 'Forward', exact: true })).toBeDisabled();
    await address.click();
    const input = page.getByRole('textbox', { name: 'Address', exact: true });
    expect(await input.evaluate((e: HTMLInputElement) => [e.selectionStart, e.selectionEnd])).toEqual([0, url.length]);
    await expect(input).toHaveCSS('height', '30px');
    await expect(input).toHaveCSS('padding', '0px 12px');
    await expect(input).toHaveCSS('font-size', '12px');
    await input.fill('example.org');
    await input.press('Escape');
    await expect(header.locator('.web-address-site')).toHaveText('pkg.go.dev');
    await expect(header.locator('.web-address')).not.toBeFocused();
    expect(await nativeCalls(page, 'web_navigate')).toHaveLength(0);
    await header.locator('.web-address').click();
    await input.fill('go.dev/doc');
    await input.press('Enter');
    await expect.poll(async () => (await nativeCalls(page, 'web_navigate')).at(-1)?.args.url).toBe('https://go.dev/doc');
    await header.getByRole('button', { name: 'Open in browser', exact: true }).click();
    await expect.poll(async () => (await nativeCalls(page, 'open_url')).at(-1)?.args.url).toBe('https://go.dev/doc');
  });

  for (const width of [320, 480, 600, 601]) {
    test(`${theme}: page actions fold at ${width}px`, async ({ page }) => {
      await prepareNativePane(page);
      await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
      await page.setViewportSize({ width, height: 800 });
      await page.goto('/');
      await dismissCoveringToasts(page);
      const header = page.locator('.web-header');
      const more = header.getByRole('button', { name: 'Page actions', exact: true });
      if (width > 600) {
        await expect(more).toBeHidden();
        await expect(header.getByRole('button', { name: 'Open in browser', exact: true })).toBeVisible();
        return;
      }
      await expect(header.getByRole('button', { name: 'Open in browser', exact: true })).toBeHidden();
      await expect(header.getByRole('button', { name: 'Start a conversation with this page' })).toBeHidden();
      await more.click();
      const menu = page.getByRole('menu', { name: 'Page actions' });
      await expect(menu.getByRole('menuitem', { name: 'Start a conversation with this page' })).toBeVisible();
      await menu.getByRole('menuitem', { name: 'Open in browser' }).click();
      await expect.poll(async () => (await nativeCalls(page, 'open_url')).at(-1)?.args.url).toBe(url);
      await more.click();
      await page.keyboard.press('Escape');
      await expect(more).toBeFocused();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    });
  }

  test(`${theme}: Design system includes all four address states`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.goto('/');
    await openPage(page, 'Design system');
    const specimen = page.locator('[aria-label="Web address specimen"]');
    await expect(specimen).toBeVisible();
    for (const state of ['rest', 'loading', 'editing', 'no-forward']) await expect(specimen.locator(`[data-state="${state}"] .web-header`)).toBeVisible();
    await expect(specimen.locator('[data-state="editing"] input')).toHaveValue(url);
    await expect(specimen.locator('[data-state="loading"]').getByRole('button', { name: 'Stop loading' })).toBeVisible();
    await expect(specimen.locator('[data-state="no-forward"]').getByRole('button', { name: 'Forward', exact: true })).toBeDisabled();
  });
}
