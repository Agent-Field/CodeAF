import { test, expect, type Page, type Route } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import type { WorldRow } from '../../src/features/world/types';
import { installNativeHttpMock } from './support/native-http-mock';
import { expectAccessible } from './contracts';
import { expectNoHorizontalOverflow } from './support/conversation';

const otherPlace = 'pl_0123456789abcdef';
const tab = (id: string, sessionFile?: string) => ({ id, title: id, kind: 'conversation', titleSource: 'manual', draft: '', pinned: false, ...(sessionFile ? { sessionFile } : {}) });
const strip = (page: Page) => page.locator('.workspace-tabstrip');
const chip = (page: Page, title: string) => strip(page).getByRole('tab', { name: title, exact: true });
const primary = process.platform === 'darwin' ? 'Meta' : 'Control';

async function appearance(page: Page, theme: string, width: number) {
  await page.setViewportSize({ width, height: 800 });
  await page.emulateMedia({ colorScheme: theme as 'light' | 'dark', reducedMotion: 'reduce' });
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
}

// One handler closure owns both pages' engine and workspace documents. Independent
// mocks would make a passing tab-sync test impossible, or conceal isolation bugs.
async function sharedEngine(pages: Page[]) {
  const shared = new Proxy(pages[0], {
    get(target, key) {
      if (key === 'route') return async (url: string, handler: (route: Route) => Promise<unknown>) => {
        for (const page of pages) await page.route(url, handler);
      };
      const value = Reflect.get(target, key);
      return typeof value === 'function' ? value.bind(target) : value;
    },
  });
  const engine = await installMockEngine(shared, { initial: { sessionFile: 'mock-session-1.jsonl', running: true }, world: { rows: [], items: [] }, places: { now: '2026-10-10T12:00:00.000Z', places: [{ id: otherPlace, name: 'Elsewhere', pinned: true }], chats: [] } });
  const workspace = { schema: 1, tabs: [tab('Alpha', 'mock-session-1.jsonl'), tab('Beta')], groups: [], closed: [], nextNumber: 3 };
  let revision = 1;
  let document: unknown = workspace;
  for (const page of pages) await page.route('**/api/engine/workspaces/now*', async route => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON();
      if (body.revision !== revision) return route.fulfill({ status: 409, json: { code: 'conflict', current: { key: 'now', revision, workspace: document } } });
      document = body.workspace;
      revision++;
    } else if (new URL(route.request().url()).searchParams.has('wait')) {
      // The real store long-polls; bound this fixture wait to keep the renderer from busy-polling.
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    await route.fulfill({ json: { key: 'now', revision, workspace: document } });
  });
  let seq = 1;
  const rows: WorldRow[] = [{ chatId: 'Alpha', sessionFile: 'mock-session-1.jsonl', title: 'Alpha', running: true, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: true, archived: false }];
  for (const page of pages) await page.route('**/api/engine/events?*', async route => {
    // The renderer consumes the strip's typed world records, rather than a conversation snapshot.
    if (Number(new URL(route.request().url()).searchParams.get('after')) >= seq) await new Promise(resolve => setTimeout(resolve, 100));
    await route.fulfill({ contentType: 'text/event-stream', body: `id: ${seq}\ndata: ${JSON.stringify({ epoch: 'window-contract', seq, type: 'reset', at: '2026-10-10T12:00:00Z', payload: { rows, items: [] } })}\n\n` });
  });
  return Object.assign(engine, { publishNeedsYou() { rows[0] = { ...rows[0], needsYou: 1 }; seq++; } });
}

async function nativeStub(page: Page, restore = false) {
  await installNativeHttpMock(page);
  await page.addInitScript(restore => {
    const calls: { cmd: string; args: Record<string, unknown> }[] = [];
    const callbacks = new Map<number, (data: unknown) => void>();
    let id = 0;
    Object.assign(window, { isTauri: true, __windowCalls: calls, __TAURI_INTERNALS__: {
      metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
      transformCallback(callback: (data: unknown) => void) { callbacks.set(++id, callback); return id; },
      unregisterCallback(id: number) { callbacks.delete(id); },
      async invoke(cmd: string, args: Record<string, unknown> = {}) {
        if (cmd.startsWith('plugin:http|')) return (window as unknown as { __engineHttpInvoke: (cmd: string, args: Record<string, unknown>) => Promise<unknown> }).__engineHttpInvoke(cmd, args);
        calls.push({ cmd, args });
        if (cmd === 'engine_connection') return { url: location.origin, token: 'mock-token' };
        if (cmd === 'plugin:event|listen') return args.handler;
        if (cmd === 'window_context') return { label: 'main', placeKey: 'now' };
        if (cmd === 'window_list') return [{ label: 'main', placeKey: 'now', focused: true, title: 'codeaf' }];
        if (cmd === 'window_open' || cmd === 'tab_move_to_window') return 'w-2';
        return null;
      },
    }, __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener() {} } });
    if (restore) localStorage.setItem('codeaf.desktop.workspace.windows', JSON.stringify([{ label: 'main', placeKey: 'now' }, { label: 'w-2', placeKey: 'pl_0123456789abcdef' }]));
  }, restore);
}
const nativeCalls = (page: Page, command: string) => page.evaluate(command => (window as unknown as { __windowCalls: { cmd: string; args: unknown }[] }).__windowCalls.filter(call => call.cmd === command), command);

for (const theme of ['light', 'dark']) for (const width of [320, 600, 1200]) {
  test(`SH-090/152/192/300/301/302/311: same-place windows share tabs, keep focus and toast local · ${theme} · ${width}px`, async ({ context, page }) => {
    const b = await context.newPage();
    const foreign = await context.newPage();
    const pages = [page, b, foreign];
    for (const view of pages) await appearance(view, theme, width);
    const engine = await sharedEngine(pages);
    await page.goto('/?place=now&tab=Alpha');
    await b.goto('/?place=now&tab=Beta');
    await foreign.goto(`/?place=${otherPlace}`);
    await expect(chip(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
    await expect(chip(b, 'Beta')).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press(`${primary}+t`);
    await expect(strip(page).getByRole('tab')).toHaveCount(3);
    await expect(strip(b).getByRole('tab')).toHaveCount(3);
    await expect(chip(b, 'Beta')).toHaveAttribute('aria-selected', 'true');
    await expect(chip(foreign, 'Alpha')).toHaveCount(0);
    engine.publishNeedsYou();
    for (const view of [page, b]) await expect(chip(view, 'Alpha').locator('..').getByRole('img', { name: 'Needs you', exact: true })).toBeVisible();
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+Shift+Backslash' : 'Control+Shift+a');
    const overview = page.getByRole('dialog', { name: 'All tabs overview' });
    await expect(overview).toBeVisible();
    await expect(overview.locator('[data-card-id]')).toHaveCount(3);
    await expect(b.getByRole('dialog', { name: 'All tabs overview' })).toHaveCount(0);
    await expectNoHorizontalOverflow(page);
    await expectAccessible(page);
    await page.keyboard.press('Escape');
    await chip(page, 'Alpha').click();
    await page.keyboard.press(`${primary}+w`);
    await expect(page.getByText('closed and still running', { exact: false })).toBeVisible();
    await expect(b.getByText('closed and still running', { exact: false })).toHaveCount(0);
    await expect(chip(b, 'Alpha')).toHaveCount(0);
    const kept = await page.evaluate(async () => (await (await fetch('/api/engine/workspaces/now')).json()).workspace.tabs.map((tab: { id: string }) => tab.id));
    await page.close();
    await b.reload();
    await expect.poll(() => strip(b).getByRole('tab').evaluateAll(nodes => nodes.map(node => node.id.replace(/^tab-/, '')))).toEqual(kept);
    expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toEqual([]);
  });

  for (const method of ['menu', 'tear-off'] as const) test(`SH-082/127: ${method} moves the tab without stopping work · ${theme} · ${width}px`, async ({ page }) => {
    await appearance(page, theme, width);
    await nativeStub(page);
    const engine = await sharedEngine([page]);
    await page.goto('/?place=now&tab=Alpha');
    await expect(chip(page, 'Alpha')).toHaveAttribute('aria-selected', 'true');
    if (method === 'menu') {
      await chip(page, 'Alpha').focus();
      await page.keyboard.press('Shift+F10');
      await page.getByRole('menuitem', { name: 'Move to new window', exact: true }).click();
    } else {
      const dataTransfer = await page.evaluateHandle(() => new DataTransfer());
      const target = chip(page, 'Alpha').locator('..');
      await target.dispatchEvent('dragstart', { dataTransfer });
      await target.dispatchEvent('dragend', { dataTransfer, clientX: -8, clientY: 20, screenX: 92, screenY: 120 });
    }
    const request = { tabId: 'Alpha', placeKey: 'now', ...(method === 'tear-off' ? { at: { x: 92, y: 120 } } : {}) };
    await expect.poll(() => nativeCalls(page, 'tab_move_to_window')).toEqual([{ cmd: 'tab_move_to_window', args: { request } }]);
    await expect(chip(page, 'Alpha')).toHaveCount(0);
    await expect(chip(page, 'Beta')).toHaveAttribute('aria-selected', 'true');
    expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toEqual([]);
  });
}

for (const theme of ['light', 'dark']) {
  test(`SH-303/312: New Window keyboard and menu request Now · ${theme}`, async ({ page }) => {
    await appearance(page, theme, 1200);
    await nativeStub(page);
    await installMockEngine(page, { initial: {} });
    await page.goto('/');
    await expect(page.locator('.app-shell')).toBeVisible();
    await page.keyboard.press(`${primary}+n`);
    await expect.poll(() => nativeCalls(page, 'window_open')).toEqual([{ cmd: 'window_open', args: { request: { placeKey: 'now' } } }]);
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('codeaf:desktop-tab-action', { detail: 'new-window' })));
    await expect.poll(() => nativeCalls(page, 'window_open')).toHaveLength(2);
  });
  test(`SH-307: relaunch opens each recorded non-main window on its place · ${theme}`, async ({ page }) => {
    await appearance(page, theme, 1200);
    await nativeStub(page, true);
    await installMockEngine(page, { initial: {} });
    await page.goto('/');
    await expect(page.locator('.app-shell')).toBeVisible();
    await expect.poll(() => nativeCalls(page, 'window_open')).toEqual([{ cmd: 'window_open', args: { request: { placeKey: 'pl_0123456789abcdef' } } }]);
  });
}

// SH-309 uses the real conversation scroll listener and its window-local saved memory.
// Both a reader in the middle and a reader following the tail must survive reload.
for (const theme of ['light', 'dark']) for (const width of [320, 600, 1200]) for (const bottom of [false, true]) {
  test(`SH-309: reload restores ${bottom ? 'tail following' : 'reading position'} · ${theme} · ${width}px`, async ({ page }) => {
    await appearance(page, theme, width);
    const text = Array.from({ length: 90 }, (_, index) => `Window scroll paragraph ${index + 1}.`).join('\n\n');
    await installMockEngine(page, { initial: {}, turns: [{ entries: [{ Role: 'assistant', Text: text, Answer: true }] }] });
    await page.goto('/');
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Remember this reading position');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    const scroller = page.locator('.conversation-scroll');
    await expect(page.getByText('Window scroll paragraph 90.', { exact: true })).toBeAttached();
    await expect.poll(() => scroller.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThanOrEqual(2);
    const target = await scroller.evaluate((el, following) => {
      const max = el.scrollHeight - el.clientHeight;
      el.dispatchEvent(new WheelEvent('wheel', { bubbles: true }));
      el.scrollTop = following ? max : Math.round(max / 2);
      el.dispatchEvent(new Event('scroll'));
      return el.scrollTop;
    }, bottom);
    expect(target).toBeGreaterThan(100);
    await expect.poll(() => page.evaluate(() => {
      const key = Object.keys(localStorage).find(key => key.startsWith('codeaf.desktop.tabScroll.v2.') && !key.endsWith('.index'));
      return key ? localStorage.getItem(key) : null;
    })).toContain(`"top":${Math.round(target)}`);
    await page.reload();
    await expect(page.getByText('Window scroll paragraph 90.', { exact: true })).toBeAttached();
    await expect.poll(() => scroller.evaluate((el, saved) => Math.abs(el.scrollTop - (saved.bottom ? el.scrollHeight - el.clientHeight : saved.target)), { bottom, target })).toBeLessThanOrEqual(2);
    await expectNoHorizontalOverflow(page);
  });
}
