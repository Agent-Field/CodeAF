import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

const release = 'pl_0000000000000001';
const marketing = 'pl_0000000000000002';

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: boot destination wins before the workspace mounts and Activity stays retired`, async ({ page }) => {
    await page.addInitScript(({ theme, marketing }) => {
      localStorage.setItem('codeaf-theme', theme);
      localStorage.setItem('codeaf.desktop.window.main.place', marketing);
    }, { theme, marketing });
    await installMockEngine(page, { initial: { entries: [], title: '' } });
    await installMockPlaces(page, { places: [
      { id: release, name: 'Release', pinned: true },
      { id: marketing, name: 'Marketing', pinned: true },
    ] });
    await page.goto(`/?place=${release}`);
    await expect(page.getByRole('tab', { name: 'Release', exact: true })).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe(release);
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:dev-page', { detail: 'Activity' })));
    await expect(page.getByRole('heading', { name: 'Activity', exact: true })).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'Release', exact: true })).toBeVisible();
  });

  test(`${theme}: frame blur stays on chrome and turns solid without focus`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.route('**/api/engine/**', route => route.abort());
    await page.goto('/');
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await expect(page.locator('html')).toHaveAttribute('data-window-active', 'true');
    await expect(page.locator('html')).toHaveAttribute('data-material', 'glass');
    await expect(page.locator('.sidebar')).toHaveCSS('backdrop-filter', 'blur(40px) saturate(1.4)');
    await expect(page.locator('.workspace-tabbar')).toHaveCSS('backdrop-filter', 'blur(40px) saturate(1.4)');
    await expect(page.locator('.app-shell')).toHaveCSS('backdrop-filter', 'none');
    await page.evaluate(() => window.dispatchEvent(new Event('blur')));
    await expect(page.locator('html')).toHaveAttribute('data-material', 'solid');
    await expect(page.locator('.sidebar')).toHaveCSS('backdrop-filter', 'none');
    await expect(page.locator('.workspace-tabbar')).toHaveCSS('backdrop-filter', 'none');
  });
}

test('a new native window boots Now without reading or overwriting the main window place', async ({ page }) => {
  await page.addInitScript(marketing => {
    localStorage.setItem('codeaf.desktop.window.main.place', marketing);
    // Only window identity and IPC receipts are stubbed; no engine work is represented as live.
    Object.assign(window, {
      isTauri: true,
      __TAURI_INTERNALS__: {
        invoke: async (cmd: string, args: Record<string, unknown> = {}) => {
          if (cmd === 'plugin:event|listen') return args.handler;
          if (cmd === 'window_context') return { label: 'w-7', placeKey: 'now' };
          if (cmd === 'window_claim_handoff') return null;
          throw new Error('No native engine in this fixture');
        },
        transformCallback: () => 1,
        unregisterCallback: () => {},
        metadata: { currentWindow: { label: 'w-7' }, currentWebview: { windowLabel: 'w-7', label: 'w-7' } },
      },
      __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener: () => {} },
    });
  }, marketing);
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await expect(page.locator('.app-shell')).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.w-7.place'))).toBe('now');
  expect(await page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe(marketing);
});
