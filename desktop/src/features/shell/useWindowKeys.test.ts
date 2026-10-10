import assert from 'node:assert/strict';
import test from 'node:test';
import { mockIPC, clearMocks } from '@tauri-apps/api/mocks';
import { toasts } from '../../design/toasts.ts';
import { goToHasTheKey, takeNewWindow, windowOpen } from './useWindowKeys.ts';

function asDesktop() {
  const globals = globalThis as unknown as { isTauri?: boolean; window?: object };
  globals.isTauri = true;
  globals.window = globals.window ?? {};
}

function leaveDesktop() {
  clearMocks();
  const globals = globalThis as unknown as { isTauri?: boolean; window?: object };
  delete globals.isTauri;
}

test('registry id new-window calls windowOpen on Now', async () => {
  asDesktop();
  const calls: unknown[][] = [];
  mockIPC((command, args) => { calls.push([command, args]); return 'w-2'; });
  try {
    // The menu event arrives as this same id, with no focused element, through runShortcut.
    assert.equal(takeNewWindow({ id: 'new-window' }, true, undefined), true);
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(calls.filter(([command]) => command === 'window_open'), [
      ['window_open', { request: { placeKey: 'now' } }],
    ]);
  } finally {
    leaveDesktop();
  }
});

test('a plain browser leaves the new-window key alone', async () => {
  leaveDesktop();
  let opened = 0;
  assert.equal(takeNewWindow({ id: 'new-window' }, false, undefined, () => { opened += 1; }), false);
  assert.equal(opened, 0);
  assert.equal(await windowOpen({ placeKey: 'now' }), undefined);
});

test('Go to keeps ⌘N so the sheet can create a place', () => {
  const target = { closest: (selector: string) => (selector === '.goto-chooser' ? {} : null) } as unknown as EventTarget;
  let opened = 0;
  assert.equal(goToHasTheKey(target), true);
  assert.equal(takeNewWindow({ id: 'new-window' }, true, { target }, () => { opened += 1; }), false);
  assert.equal(opened, 0);
});

test('another registry id does not open a window', () => {
  let opened = 0;
  assert.equal(takeNewWindow({ id: 'new' }, true, undefined, () => { opened += 1; }), false);
  assert.equal(opened, 0);
});

test('a refused open says so, and uses a sentence when the shell sent none', async () => {
  asDesktop();
  mockIPC(() => Promise.reject(new Error('refused')));
  try {
    assert.equal(takeNewWindow({ id: 'new-window' }, true, undefined), true);
    await new Promise(resolve => setImmediate(resolve));
    const toast = toasts.getToast();
    assert.equal(toast?.tone, 'warning');
    assert.deepEqual(toast?.message, ['refused']);
    if (toast) toasts.dismiss(toast.id);
    mockIPC(() => Promise.reject('nope'));
    assert.equal(takeNewWindow({ id: 'new-window' }, true, undefined), true);
    await new Promise(resolve => setImmediate(resolve));
    const bare = toasts.getToast();
    assert.deepEqual(bare?.message, ['Could not open a window']);
    if (bare) toasts.dismiss(bare.id);
  } finally {
    leaveDesktop();
  }
});
