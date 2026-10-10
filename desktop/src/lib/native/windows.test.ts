import assert from 'node:assert/strict';
import test from 'node:test';
import { createNativeWindows, FOCUS_TAB_EVENT, isPlaceKey, isTabId, reportsMultiwindow, tabMoveToWindow, type WindowsBridge } from './windows.ts';

type Call = { command: string; args?: Record<string, unknown> };

function stub(over: Partial<WindowsBridge> & { reply?: unknown } = {}) {
  const calls: Call[] = [];
  const tabs: string[] = [];
  const listeners: Record<string, (payload: unknown) => void> = {};
  const bridge: WindowsBridge = {
    desktop: true,
    invoke: async <T,>(command: string, args?: Record<string, unknown>) => {
      calls.push({ command, args });
      return over.reply as T;
    },
    listen: async (event, handler) => {
      listeners[event] = handler;
      return () => delete listeners[event];
    },
    search: () => '',
    openBrowserTab: (url) => void tabs.push(url),
    ...over,
  };
  return { bridge, calls, tabs, listeners };
}

const PLACE = 'p-0123456789ab';

test('place keys and tab ids follow the Rust rules', () => {
  for (const ok of ['now', PLACE]) assert.ok(isPlaceKey(ok));
  for (const bad of ['', 'Now', 'p-0123456789AB', 'p-0123456789a', 'p-0123456789abc', 'now&x=1', 'p-0123456789ab?x', 7, null]) {
    assert.ok(!isPlaceKey(bad), String(bad));
  }
  assert.ok(isTabId('tab_1-a'));
  assert.ok(!isTabId('') && !isTabId('a b') && !isTabId('x'.repeat(129)));
});

test('openPlaceWindow sends window_open with the request shape', async () => {
  const { bridge, calls } = stub({ reply: 'win-2' });
  const win = createNativeWindows(bridge);
  assert.equal(await win.openPlaceWindow(PLACE, 'tab-1'), 'win-2');
  assert.equal(await win.openPlaceWindow('now'), 'win-2');
  assert.deepEqual(calls, [
    { command: 'window_open', args: { request: { placeKey: PLACE, focusTab: 'tab-1' } } },
    { command: 'window_open', args: { request: { placeKey: 'now' } } },
  ]);
});

test('a handoff receipt still yields the label', async () => {
  const { bridge } = stub({ reply: { label: 'win-3', handoffId: 'h' } });
  assert.equal(await createNativeWindows(bridge).openPlaceWindow('now'), 'win-3');
});

test('bad keys are refused before the bridge is called', async () => {
  const { bridge, calls } = stub();
  const win = createNativeWindows(bridge);
  await assert.rejects(win.openPlaceWindow('now?x=1'), /not a place/);
  await assert.rejects(win.openPlaceWindow('now', 'a b'), /cannot be focused/);
  await assert.rejects(win.moveTabToNewWindow('', 'now'), /cannot be focused/);
  await assert.rejects(win.moveTabToNewWindow('t', 'zzz'), /not a place/);
  await assert.rejects(win.moveTabToNewWindow('t', 'now', { x: Infinity, y: 0 }), /outside the screen/);
  await assert.rejects(win.moveTabToNewWindow('t', 'now', { x: 2e6, y: 0 }), /outside the screen/);
  assert.equal(calls.length, 0);
});

test('moveTabToNewWindow sends tab_move_to_window, with the drop point only when given', async () => {
  const { bridge, calls } = stub({ reply: 'win-4' });
  const win = createNativeWindows(bridge);
  assert.equal(await win.moveTabToNewWindow('tab-1', PLACE, { x: 40, y: 80 }), 'win-4');
  await win.moveTabToNewWindow('tab-1', PLACE);
  assert.deepEqual(calls, [
    { command: 'tab_move_to_window', args: { request: { tabId: 'tab-1', placeKey: PLACE, at: { x: 40, y: 80 } } } },
    { command: 'tab_move_to_window', args: { request: { tabId: 'tab-1', placeKey: PLACE } } },
  ]);
});

test('focus, list and title use the native commands', async () => {
  const rows = [{ label: 'main', placeKey: 'now', focused: true, title: 'codeaf' }];
  const { bridge, calls } = stub({ reply: [...rows, { label: 1 }] });
  const win = createNativeWindows(bridge);
  assert.deepEqual(await win.listWindows(), rows);
  await win.focusWindow('main');
  await win.setWindowTitle('Marketing');
  await win.setWindowTitle();
  assert.deepEqual(calls.slice(1), [
    { command: 'window_focus', args: { label: 'main' } },
    { command: 'window_set_title', args: { title: 'Marketing' } },
    { command: 'window_set_title', args: { title: '' } },
  ]);
});

test('currentPlaceKey reads ?place= and defaults to Now', () => {
  const at = (search: string) => createNativeWindows(stub({ search: () => search }).bridge).currentPlaceKey();
  assert.equal(at(`?place=${PLACE}`), PLACE);
  assert.equal(at(''), 'now');
  assert.equal(at('?place=evil%26x'), 'now');
});

test('onFocusTab hears window://focus-tab and ignores malformed payloads', async () => {
  const { bridge, listeners } = stub();
  const heard: string[] = [];
  const off = await createNativeWindows(bridge).onFocusTab((id) => heard.push(id));
  listeners[FOCUS_TAB_EVENT]({ tabId: 'tab-9' });
  listeners[FOCUS_TAB_EVENT]({ tabId: 'bad id' });
  listeners[FOCUS_TAB_EVENT](null);
  assert.deepEqual(heard, ['tab-9']);
  off();
  assert.equal(listeners[FOCUS_TAB_EVENT], undefined);
});

test('Ctrl+N opens Now off macOS and is left to the menu on macOS', async () => {
  const { bridge, calls } = stub({ reply: 'w' });
  const win = createNativeWindows(bridge);
  const ctrlN = { key: 'n', ctrlKey: true, metaKey: false, shiftKey: false, altKey: false };
  assert.equal(win.handleNewWindowKey(ctrlN, 'MacIntel'), false);
  assert.equal(win.handleNewWindowKey({ ...ctrlN, shiftKey: true }, 'Win32'), false);
  assert.equal(win.handleNewWindowKey({ ...ctrlN, key: 't' }, 'Linux x86_64'), false);
  assert.equal(win.handleNewWindowKey(ctrlN, 'Win32'), true);
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(calls, [{ command: 'window_open', args: { request: { placeKey: 'now' } } }]);
});

test('in a browser opening a place opens a tab with ?place= and everything else is a no-op', async () => {
  const { bridge, calls, tabs, listeners } = stub({ desktop: false });
  const win = createNativeWindows(bridge);
  assert.equal(await win.openPlaceWindow(PLACE), undefined);
  assert.deepEqual(tabs, [`?place=${PLACE}`]);
  assert.deepEqual(await win.listWindows(), []);
  assert.equal(await win.moveTabToNewWindow('t', 'now'), undefined);
  await win.focusWindow('main');
  await win.setWindowTitle('x');
  await (await win.onFocusTab(() => {}))();
  assert.equal(calls.length, 0);
  assert.equal(Object.keys(listeners).length, 0);
});

test('tabMoveToWindow sends tab_move_to_window with no drop point, and only when multiwindow is reported', async () => {
  const desktop = stub({ reply: 'w-2' });
  const windows = createNativeWindows(desktop.bridge);
  assert.equal(reportsMultiwindow(windows), true);
  assert.equal(await tabMoveToWindow({ id: 'tab-1' }, PLACE, windows), true);
  assert.equal(await tabMoveToWindow({ id: 'tab-1' }, undefined, windows), true);
  assert.deepEqual(desktop.calls, [
    { command: 'tab_move_to_window', args: { request: { tabId: 'tab-1', placeKey: PLACE } } },
    { command: 'tab_move_to_window', args: { request: { tabId: 'tab-1', placeKey: 'now' } } },
  ]);
  const browser = stub({ desktop: false });
  const plain = createNativeWindows(browser.bridge);
  assert.equal(reportsMultiwindow(plain), false);
  assert.equal(await tabMoveToWindow({ id: 'tab-1' }, 'now', plain), false);
  assert.equal(await tabMoveToWindow({ id: 'a b' }, 'now', windows), false);
  assert.equal(await tabMoveToWindow({ id: 'tab-1' }, 'pl_0123456789abcdef', windows), false);
  assert.equal(browser.calls.length, 0);
  assert.equal(desktop.calls.length, 2);
});
