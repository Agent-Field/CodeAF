import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';
import { installNativeHttpMock } from './support/native-http-mock';
import { openPage } from './support/shell-navigation';

// SH-030 / SH-031 use the live shell, so a row fixture cannot hide a missing workspace entry.
async function installWindowStub(page: Page) {
  await page.addInitScript(() => {
    const calls: { cmd: string; args: unknown }[] = [];
    const callbacks = new Map<number, (data: unknown) => void>();
    let nextId = 1;
    async function invoke(cmd: string, args: Record<string, unknown> = {}) {
      calls.push({ cmd, args });
      if (cmd.startsWith('plugin:http|')) return (window as unknown as { __engineHttpInvoke: (cmd: string, args: Record<string, unknown>) => Promise<unknown> }).__engineHttpInvoke(cmd, args);
      if (cmd === 'plugin:event|listen') return args.handler;
      if (cmd === 'engine_connection') return { url: location.origin, token: 'mock-token', model: 'deepseek/deepseek-v4.1-flash' };
      if (cmd === 'window_context') return { label: 'main', placeKey: 'now' };
      if (cmd === 'window_list') return [{ label: 'main', placeKey: 'now', focused: true, title: 'codeaf' }];
      if (cmd === 'window_claim_handoff') return null;
      if (cmd === 'window_open') return 'w-2';
      return null;
    }
    Object.assign(window, { isTauri: true, __menuWindow: { calls } });
    Object.assign(window, {
      __TAURI_INTERNALS__: {
        invoke,
        transformCallback(callback: (data: unknown) => void) { const id = nextId++; callbacks.set(id, callback); return id; },
        unregisterCallback(id: number) { callbacks.delete(id); },
        metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
      },
      __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener() { /* the stub holds no listener */ } },
    });
  });
}

const place = 'pl_0123456789abcdef';
const now = (page: Page) => page.locator('.place-rail').getByRole('button', { name: 'Now', exact: true });

async function mount(page: Page, theme: string, native = false) {
  if (native) { await installNativeHttpMock(page); await installWindowStub(page); }
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  await installMockEngine(page, {});
  await installMockPlaces(page, { places: [{ id: place, name: 'Reading', pinned: true, tint: 'sage' }] });
  await page.addInitScript(() => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
    tabs: [{ id: 'unplaced', kind: 'conversation', title: 'Unplaced draft', titleSource: 'manual', draft: 'Keep my Now draft', pinned: false }],
    groups: [], closed: [], activeId: 'unplaced', recentIds: ['unplaced'], nextNumber: 2,
  })));
  await page.goto('/');
  await page.getByRole('button', { name: /^Reading/ }).first().click();
  await expect(page.getByRole('tab', { name: 'Reading', exact: true })).toBeVisible();
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
}

for (const theme of ['light', 'dark']) {
  test(`Now restores unplaced tabs and graphite; browser command-click also enters the workspace · ${theme}`, async ({ page }) => {
    await page.addInitScript(() => {
      Object.assign(window, { __openedUrls: [], open: (url: string) => {
        (window as unknown as { __openedUrls: string[] }).__openedUrls.push(url);
        return null;
      } });
    });
    await mount(page, theme);
    await now(page).click();
    await expect(now(page)).toHaveAttribute('aria-current', 'page');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
    await expect(page.getByRole('tab', { name: 'Reading', exact: true })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep my Now draft');
    await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe('now');
    await expect(now(page)).toHaveCSS('font-size', '13px');
    expect((await now(page).boundingBox())!.height).toBe(32);
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await now(page).click({ button: 'middle' });
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
    await openPage(page, 'Settings');
    await now(page).click({ modifiers: ['Meta'] });
    await expect(now(page)).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('button', { name: 'New tab', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep my Now draft');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
    expect(await page.evaluate(() => (window as unknown as { __openedUrls: string[] }).__openedUrls)).toEqual([]);
    await now(page).click({ button: 'right' });
    await expect(page.getByRole('menu')).toHaveCount(0);
  });

  test(`native command-click and middle-click open Now without changing this window · ${theme}`, async ({ page }) => {
    await mount(page, theme, true);
    await now(page).click({ modifiers: ['Meta'] });
    await now(page).click({ button: 'middle' });
    await expect.poll(() => page.evaluate(() => (window as unknown as { __menuWindow: { calls: { cmd: string; args: unknown }[] } }).__menuWindow.calls.filter(call => call.cmd === 'window_open'))).toEqual([
      { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
      { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
    ]);
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
    await expect(page.getByRole('tab', { name: 'Reading', exact: true })).toHaveAttribute('aria-selected', 'true');
  });
}
