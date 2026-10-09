import test from 'node:test';
import assert from 'node:assert/strict';

// keyboard.ts reads navigator.platform at import; the matcher takes the platform as an argument.
Object.defineProperty(globalThis, 'navigator', { value: { platform: 'Linux x86_64' }, configurable: true });
const { shortcutOf } = await import('./keyboard.ts');

const key = (key: string, over: Record<string, unknown> = {}) => ({ key, code: '', metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...over });
const mac = (event: ReturnType<typeof key>) => shortcutOf(event, true);
const linux = (event: ReturnType<typeof key>) => shortcutOf(event, false);

test('⌘1–9 jump to tabs, ⌥⌘1–3 pick pinned models (design Interactions)', () => {
  assert.deepEqual(mac(key('2', { metaKey: true })), { id: 'jump', index: 2 });
  assert.deepEqual(mac(key('¡', { code: 'Digit1', metaKey: true, altKey: true })), { id: 'model-pin', index: 1 });
  assert.deepEqual(mac(key('£', { code: 'Digit3', metaKey: true, altKey: true })), { id: 'model-pin', index: 3 });
  assert.equal(mac(key('4', { code: 'Digit4', metaKey: true, altKey: true })), undefined);
  assert.deepEqual(linux(key('2', { code: 'Digit2', ctrlKey: true, altKey: true })), { id: 'model-pin', index: 2 });
  assert.equal(mac(key('2', { code: 'Digit2', ctrlKey: true, altKey: true })), undefined);
});

test('⌘⇧\\ is the overview on a Mac, Ctrl Shift A elsewhere; ⌘↑ and ⌘↓ only step between messages', () => {
  assert.deepEqual(mac(key('|', { code: 'Backslash', metaKey: true, shiftKey: true })), { id: 'overview' });
  assert.deepEqual(linux(key('A', { ctrlKey: true, shiftKey: true })), { id: 'overview' });
  assert.deepEqual(mac(key('ArrowUp', { metaKey: true })), { id: 'turn-previous' });
  assert.deepEqual(mac(key('ArrowDown', { metaKey: true })), { id: 'turn-next' });
  assert.equal(mac(key('ArrowUp', { metaKey: true, target: { tagName: 'TEXTAREA', value: 'words' } } as never)), undefined);
});

test('rail, focus, tasks, history, settings, models and palette keys', () => {
  const ids = [['s', {}, 'rail'], ['b', {}, 'rail'], ['f', { shiftKey: true }, 'focus'], ['k', { shiftKey: true }, 'tasks'], ['y', {}, 'history'], [',', {}, 'settings'], ['/', {}, 'models'], ['k', {}, 'palette']] as const;
  for (const [k, over, id] of ids) assert.deepEqual(mac(key(k, { metaKey: true, ...over })), { id });
});

test('⌃Tab switches recent tabs on both platforms and ⌘Tab is left to macOS', () => {
  assert.deepEqual(mac(key('Tab', { ctrlKey: true })), { id: 'switch' });
  assert.deepEqual(mac(key('Tab', { ctrlKey: true, shiftKey: true })), { id: 'switch-back' });
  assert.equal(mac(key('Tab', { metaKey: true })), undefined);
});
