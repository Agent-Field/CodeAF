import test from 'node:test';
import assert from 'node:assert/strict';

import { deliverDesktopAction, shellShortcutOf, isDesktopTabAction } from './desktopMenuRoute.ts';

test('native menu shell commands run the same shortcut ids as their keys', () => {
  const ran: string[] = [];
  const tabs: string[] = [];
  for (const [payload, id] of [['settings', 'settings'], ['focus', 'focus'], ['sidebar', 'rail'], ['history', 'history'], ['new-window', 'new-window']] as const) {
    assert.equal(deliverDesktopAction(payload, action => tabs.push(action), shortcut => { ran.push(shortcut.id); return true; }), true);
    assert.deepEqual(shellShortcutOf(payload), { id });
  }
  assert.deepEqual(ran, ['settings', 'focus', 'rail', 'history', 'new-window']);
  assert.deepEqual(tabs, []);
});

test('tab commands, including close-stop, go to the workspace and not the shortcut registry', () => {
  const tabs: string[] = [];
  for (const action of ['new', 'close', 'close-stop', 'reopen', 'overview', 'next', 'previous']) {
    assert.equal(isDesktopTabAction(action), true);
    deliverDesktopAction(action, a => tabs.push(a), () => assert.fail('registry must not run'));
  }
  assert.equal(tabs.length, 7);
});

test('unknown or inherited payloads are refused', () => {
  for (const payload of ['toString', '__proto__', 'quit', 7, null, {}]) {
    assert.equal(deliverDesktopAction(payload, () => assert.fail('dispatch'), () => assert.fail('run')), false);
  }
});

test('isDesktopTabAction accepts exactly the tab commands and no shell chord', () => {
  const tab = ['new', 'close', 'close-stop', 'reopen', 'overview', 'next', 'previous'];
  for (const action of tab) assert.equal(isDesktopTabAction(action), true);
  for (const other of ['settings', 'focus', 'sidebar', 'history', 'new-window', 'Close', '', 'constructor']) assert.equal(isDesktopTabAction(other), false);
});
