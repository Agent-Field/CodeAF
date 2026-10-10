import { test, expect } from '@playwright/test';
import { dismissCoveringToasts, emitNewTab, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';

// TW-13: a page's target=_blank / window.open is a background web tab after the
// opener. The browser has no native views, so this drives the typed web://new-tab
// event the native side already emits and reads the strip.
const PANE = 'webpane1';

test.beforeEach(async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  await installNativeWebMock(page);
  await seedWebTab(page);
  const go = page.goto.bind(page);
  page.goto = async (url, options) => {
    const response = await go(url, options);
    await dismissCoveringToasts(page);
    return response;
  };
});

test('a target=_blank event adds one background tab after the opener', async ({ page }) => {
  await page.goto('/');
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
  await emitNewTab(page, PANE, 'https://go.dev/play');
  const titles = page.locator('.workspace-tabstrip .workspace-tab-title');
  await expect(titles).toHaveText(['Config stack', 'pkg.go.dev', 'go.dev']);
  await expect(page.locator('.workspace-tab[data-active="true"] .workspace-tab-title')).toHaveText('pkg.go.dev');
  expect(page.context().pages()).toHaveLength(1);
  expect(await nativeCalls(page, 'open_url')).toEqual([]);
  expect((await nativeCalls(page)).filter(call => /new_window|window_open|create_window/.test(call.cmd))).toEqual([]);
  expect((await nativeCalls(page, 'web_open')).length).toBe(1);

  await emitNewTab(page, PANE, 'javascript:alert(1)');
  await expect(titles).toHaveText(['Config stack', 'pkg.go.dev', 'go.dev']);

  await page.locator('.workspace-tab').filter({ has: page.locator('.workspace-tab-title', { hasText: /^go\.dev$/ }) }).getByRole('tab').click();
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(2);
  expect((await nativeCalls(page, 'web_open'))[1].args.url).toBe('https://go.dev/play');
  await expect(page.locator('.web-address-site')).toHaveText('go.dev');
});
