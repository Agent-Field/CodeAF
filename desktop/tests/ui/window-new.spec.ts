import { test, expect, type Page } from '@playwright/test';

// SH-279, SH-303. ⌘N / Ctrl N and File ▸ New Window open a window on Now.
// In a plain browser the chord is left alone: absent, not a navigation of this window.

const callsOf = (page: Page) => page.evaluate(() => (window as unknown as { __menuWindow: { calls: { cmd: string; args: unknown }[] } }).__menuWindow.calls);

async function installWindowStub(page: Page) {
  await page.addInitScript(() => {
    const calls: { cmd: string; args: unknown }[] = [];
    const callbacks = new Map<number, (data: unknown) => void>();
    let nextId = 1;
    async function invoke(cmd: string, args: Record<string, unknown> = {}) {
      calls.push({ cmd, args });
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

async function watchChord(page: Page) {
  await page.evaluate(() => {
    const seen: boolean[] = [];
    Object.assign(window, { __nwPrevented: seen });
    // Capture, and on this same window: a taken chord calls stopPropagation, so a bubble listener never hears it.
    window.addEventListener('keydown', event => {
      if (event.key.toLowerCase() === 'n' && (event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey) seen.push(event.defaultPrevented);
    }, true);
  });
}

const prevented = (page: Page) => page.evaluate(() => (window as unknown as { __nwPrevented: boolean[] }).__nwPrevented);
const opens = (page: Page) => callsOf(page).then(calls => calls.filter(call => call.cmd === 'window_open'));

test('in a browser Ctrl N is left to the browser and the menu event opens nothing', async ({ page }) => {
  const opened: string[] = [];
  await page.addInitScript(() => {
    const openedUrls: string[] = [];
    Object.assign(window, { __openedUrls: openedUrls, open: (url?: string | URL) => { openedUrls.push(String(url ?? '')); return null; } });
  });
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await expect(page.locator('.app-shell')).toBeVisible();
  await watchChord(page);
  await page.keyboard.press('Control+n');
  await expect.poll(() => prevented(page)).toEqual([false]);
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'new-window' })));
  expect(await page.evaluate(() => (window as unknown as { __openedUrls: string[] }).__openedUrls)).toEqual(opened);
  await expect(page.locator('.app-shell')).toBeVisible();
});

test('under the Tauri stub Ctrl N and the New Window menu event open Now', async ({ page }) => {
  await installWindowStub(page);
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await expect(page.locator('.app-shell')).toBeVisible();
  await watchChord(page);
  await page.keyboard.press('Control+n');
  await expect.poll(() => opens(page)).toEqual([{ cmd: 'window_open', args: { request: { placeKey: 'now' } } }]);
  await expect.poll(() => prevented(page)).toEqual([true]);
  await page.keyboard.press('Control+p');
  await expect(page.locator('.goto-chooser')).toBeVisible();
  await page.keyboard.press('Control+n');
  await expect.poll(() => opens(page)).toEqual([{ cmd: 'window_open', args: { request: { placeKey: 'now' } } }]);
  await page.keyboard.press('Escape');
  await expect(page.locator('.goto-chooser')).toHaveCount(0);
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'new-window' })));
  await expect.poll(() => opens(page)).toEqual([
    { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
    { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
  ]);
});
