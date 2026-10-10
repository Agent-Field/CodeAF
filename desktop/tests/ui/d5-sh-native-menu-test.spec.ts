import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';

// Native menu items reach the page as codeaf:desktop-tab-action events; each must do what its key does (Cov SH-016, SH-283, SH-284).
const SESSION = 'mock-session-1.jsonl';
const rail = (page: Page) => page.locator('.app-shell > .sidebar');
const strip = (page: Page) => page.locator('.workspace-tabbar');
const menu = (page: Page, action: string) => page.evaluate(detail => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail })), action);
const stops = (engine: Awaited<ReturnType<typeof installMockEngine>>) => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop')).length;

for (const theme of ['light', 'dark'] as const) for (const width of [320, 600, 1200]) {
  test.describe(`${theme} ${width}px`, () => {
    test.use({ viewport: { width, height: 800 }, colorScheme: theme });

    test('rail, focus and settings events do what their keys do', async ({ page }) => {
      await page.route('**/api/engine/**', route => route.abort());
      await page.goto('/');
      if (width > 600) {
        // A pointer resting on the left or top edge peeks a collapsed rail or strip, which is a different state than the one asserted here.
        await page.mouse.move(width / 2, 400);
        await expect(rail(page)).toBeVisible();
        await menu(page, 'sidebar');
        await expect(rail(page)).toBeHidden();
        await menu(page, 'sidebar');
        await expect(rail(page)).toBeVisible();
        await menu(page, 'focus');
        await expect(strip(page)).toBeHidden();
        await expect(rail(page)).toBeHidden();
        await menu(page, 'focus');
        await expect(strip(page)).toBeVisible();
      }
      await menu(page, 'settings');
      await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
      await menu(page, 'settings');
      await expect(page.getByRole('tab', { name: 'Models', exact: true })).toHaveCount(1);
      await expectAccessible(page);
    });

    test('history opens its tab and close-stop stops the running session', async ({ page }) => {
      const engine = await installMockEngine(page, { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Trailing commas' }] } });
      const tab = { id: 'a', title: 'Config stack', draft: '', titleSource: 'manual', kind: 'conversation', pinned: false, sessionFile: SESSION };
      await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); },
        JSON.stringify({ tabs: [tab, { ...tab, id: 'b', title: 'Idle', sessionFile: undefined }], groups: [], closed: [], activeId: 'a', nextNumber: 3, recentIds: ['a', 'b'] }));
      await page.goto('/');
      await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
      await page.waitForTimeout(300);
      await menu(page, 'close-stop');
      await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveCount(0);
      await expect.poll(() => stops(engine)).toBe(1);
      await menu(page, 'history');
      await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveCount(1);
      await menu(page, 'history');
      await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveCount(1);
    });
  });
}
