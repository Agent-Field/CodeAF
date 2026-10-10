import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';

const reply = Array.from({ length: 90 }, (_, i) => `Scroll memory paragraph ${i + 1}.`).join('\n\n');

for (const bottom of [false, true]) {
  test(`reload restores ${bottom ? 'bottom following' : 'a mid-transcript position'} in this window`, async ({ page }) => {
    await installMockEngine(page, {
      initial: { title: 'Scroll memory', entries: [], running: false },
      turns: [{ entries: [{ Role: 'assistant', Text: reply, Answer: true }] }],
    });
    await openApp(page);
    await send(page, 'Remember where I am reading');
    const scroller = page.locator('.conversation-scroll');
    await expect(page.getByText('Scroll memory paragraph 90.', { exact: true })).toBeAttached();
    await expect.poll(() => scroller.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThanOrEqual(2);
    // Programmatic positioning avoids platform wheel momentum while exercising the real scroll listener and persistence.
    const target = await scroller.evaluate((el, following) => {
      const max = el.scrollHeight - el.clientHeight;
      el.dispatchEvent(new WheelEvent('wheel', { bubbles: true }));
      el.scrollTop = following ? max : Math.round(max / 2);
      el.dispatchEvent(new Event('scroll'));
      return el.scrollTop;
    }, bottom);
    expect(target).toBeGreaterThan(100);
    await expect.poll(() => page.evaluate(() => {
      const key = Object.keys(localStorage).find(k => k.startsWith('codeaf.desktop.tabScroll.v2.') && !k.endsWith('.index'));
      return key ? localStorage.getItem(key) : null;
    })).toContain(`"top":${Math.round(target)}`);
    await page.reload();
    await expect(page.getByText('Scroll memory paragraph 90.', { exact: true })).toBeAttached();
    await expect.poll(() => scroller.evaluate((el, saved) => Math.abs(el.scrollTop - (saved.following ? el.scrollHeight - el.clientHeight : saved.target)), { following: bottom, target })).toBeLessThanOrEqual(2);
  });
}

// SH-126, SH-127, SH-278. An unsent conversation has no durable link, so Copy link is absent in a browser and under
// the Tauri stub. Move to new window is listed only when that stub reports multiwindow, and choosing it calls
// tab_move_to_window. A disabled placeholder is not drawn in either place.
const mac = process.platform === 'darwin';
const chord = mac ? 'Meta+Shift+C' : 'Control+Shift+C';

function seedUnsent(page: Page) {
  const state = {
    tabs: [
      { id: 'stay', title: 'Staying', draft: '', titleSource: 'manual', kind: 'conversation', pinned: false },
      { id: 'move', title: 'Moving', draft: '', titleSource: 'manual', kind: 'conversation', pinned: false },
    ],
    groups: [], closed: [], activeId: 'move', nextNumber: 3, recentIds: ['move', 'stay'],
  };
  return page.addInitScript(value => {
    if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value);
  }, JSON.stringify(state));
}

async function installWindowStub(page: Page) {
  await page.addInitScript(() => {
    const calls: { cmd: string; args: unknown }[] = [];
    const copied: string[] = [];
    const callbacks = new Map<number, (data: unknown) => void>();
    let nextId = 1;
    async function invoke(cmd: string, args: Record<string, unknown> = {}) {
      calls.push({ cmd, args });
      if (cmd === 'plugin:event|listen') return args.handler;
      if (cmd === 'engine_connection') return { url: location.origin, token: 'mock-token', model: 'deepseek/deepseek-v4.1-flash' };
      if (cmd === 'window_context') return { label: 'main', placeKey: 'now' };
      if (cmd === 'window_list') return [{ label: 'main', placeKey: 'now', focused: true, title: 'codeaf' }];
      if (cmd === 'window_claim_handoff') return null;
      if (cmd === 'tab_move_to_window') return 'w-2';
      return null;
    }
    Object.assign(window, { isTauri: true, __menuWindow: { calls, copied } });
    Object.assign(window, {
      __TAURI_INTERNALS__: {
        invoke,
        transformCallback(callback: (data: unknown) => void) { const id = nextId++; callbacks.set(id, callback); return id; },
        unregisterCallback(id: number) { callbacks.delete(id); },
        metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
      },
      __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener() { /* the stub holds no listener */ } },
    });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { copied.push(text); } } });
  });
}

async function recordClipboard(page: Page) {
  await page.addInitScript(() => {
    const copied: string[] = [];
    Object.assign(window, { __menuWindow: { calls: [] as { cmd: string; args: unknown }[], copied } });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { copied.push(text); } } });
  });
}

const copied = (page: Page) => page.evaluate(() => (window as unknown as { __menuWindow: { copied: string[] } }).__menuWindow.copied);
const moveCalls = (page: Page) => page.evaluate(() => (window as unknown as { __menuWindow: { calls: { cmd: string; args: unknown }[] } }).__menuWindow.calls.filter(call => call.cmd === 'tab_move_to_window'));

async function openMovingMenu(page: Page) {
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Moving', exact: true })).toBeVisible();
  await page.keyboard.press(chord);
  await expect(page.getByText(/Copied the link|Could not move/)).toHaveCount(0);
  expect(await copied(page)).toEqual([]);
  await page.getByRole('tab', { name: 'Moving', exact: true }).click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Actions for Moving', exact: true });
  await expect(menu).toBeVisible();
  return menu;
}

test('in a browser Move to new window and Copy link are absent, and the copy chord does nothing', async ({ page }) => {
  await recordClipboard(page);
  await seedUnsent(page);
  const menu = await openMovingMenu(page);
  await expect(menu.getByRole('menuitem', { name: 'Move to new window' })).toHaveCount(0);
  await expect(menu.getByRole('menuitem', { name: /^Copy link/ })).toHaveCount(0);
});

test('under the Tauri stub Move to new window calls tab_move_to_window, and Copy link stays absent', async ({ page }) => {
  await installWindowStub(page);
  await seedUnsent(page);
  const menu = await openMovingMenu(page);
  await expect(menu.getByRole('menuitem', { name: /^Copy link/ })).toHaveCount(0);
  const item = menu.getByRole('menuitem', { name: 'Move to new window' });
  await expect(item).toBeVisible();
  await expect(item).toBeEnabled();
  await item.click();
  await expect.poll(() => moveCalls(page)).toEqual([{ cmd: 'tab_move_to_window', args: { request: { tabId: 'move', placeKey: 'now' } } }]);
  await expect(page.getByRole('tab', { name: 'Staying', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tab', { name: 'Moving', exact: true })).toBeVisible();
  await expect(page.getByText('Could not move')).toHaveCount(0);
});
