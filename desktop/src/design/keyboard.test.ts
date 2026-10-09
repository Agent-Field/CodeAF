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

const inTerminal = (event: ReturnType<typeof key>) => shortcutOf(event, { terminal: true });

test('Linux: a terminal field keeps every plain Ctrl editing chord for the shell', () => {
  for (const k of ['w', 't', 'k', 's', 'b', 'y', '1', '5', '9', ',', '/', 'ArrowUp', 'ArrowDown']) {
    assert.equal(inTerminal(key(k, { ctrlKey: true })), undefined, `Ctrl+${k}`);
  }
  // Outside a terminal the same chords still belong to the app.
  assert.deepEqual(linux(key('w', { ctrlKey: true })), { id: 'close' });
  assert.deepEqual(linux(key('y', { ctrlKey: true })), { id: 'history' });
});

test('Linux: a terminal field still hands Ctrl+`, Ctrl+Tab and Ctrl+Shift chords to the workspace', () => {
  assert.deepEqual(inTerminal(key('`', { code: 'Backquote', ctrlKey: true })), { id: 'terminal' });
  assert.deepEqual(inTerminal(key('Tab', { ctrlKey: true })), { id: 'switch' });
  assert.deepEqual(inTerminal(key('Tab', { ctrlKey: true, shiftKey: true })), { id: 'switch-back' });
  assert.deepEqual(inTerminal(key('T', { ctrlKey: true, shiftKey: true })), { id: 'new' });
  assert.deepEqual(inTerminal(key('W', { ctrlKey: true, shiftKey: true })), { id: 'close' });
  assert.deepEqual(inTerminal(key('A', { ctrlKey: true, shiftKey: true })), { id: 'overview' });
  assert.deepEqual(inTerminal(key('K', { ctrlKey: true, shiftKey: true })), { id: 'tasks' });
});

test('Mac: ⌘ chords work in a terminal field and Control chords are not app chords', () => {
  const macTerminal = (event: ReturnType<typeof key>) => shortcutOf(event, { mac: true, terminal: true });
  assert.deepEqual(macTerminal(key('w', { metaKey: true })), { id: 'close' });
  assert.deepEqual(macTerminal(key('t', { metaKey: true, shiftKey: true })), { id: 'reopen' });
  assert.equal(macTerminal(key('w', { ctrlKey: true })), undefined);
});

test('Places keys: ⌘P, ⌘⇧P, ⌘0, ⌘⇧W, ⌘N, ⌘Z outside fields, and the rail slots on ⌃ (Mac) or Alt (elsewhere)', () => {
  assert.deepEqual(mac(key('p', { metaKey: true })), { id: 'goto' });
  assert.deepEqual(mac(key('P', { metaKey: true, shiftKey: true })), { id: 'all-places' });
  assert.deepEqual(linux(key('P', { ctrlKey: true, shiftKey: true })), { id: 'all-places' });
  assert.deepEqual(mac(key('0', { metaKey: true })), { id: 'place-home' });
  assert.deepEqual(mac(key('W', { metaKey: true, shiftKey: true })), { id: 'close-place' });
  assert.deepEqual(mac(key('n', { metaKey: true })), { id: 'new-window' });
  assert.deepEqual(mac(key('z', { metaKey: true })), { id: 'undo' });
  assert.equal(mac({ ...key('z', { metaKey: true }), target: { tagName: 'TEXTAREA' } as unknown as EventTarget }), undefined);
  assert.deepEqual(mac(key('3', { code: 'Digit3', ctrlKey: true })), { id: 'place-jump', index: 3 });
  assert.deepEqual(mac(key('0', { code: 'Digit0', ctrlKey: true })), { id: 'place-jump', index: 0 });
  assert.deepEqual(linux(key('3', { code: 'Digit3', altKey: true })), { id: 'place-jump', index: 3 });
  // Ctrl+digit stays the tab jump on Linux, and ⌘digit on a Mac.
  assert.deepEqual(linux(key('3', { code: 'Digit3', ctrlKey: true })), { id: 'jump', index: 3 });
  assert.deepEqual(mac(key('3', { code: 'Digit3', metaKey: true })), { id: 'jump', index: 3 });
});

test('⌘G (Ctrl G on Linux) groups the selected tabs; ⌘⇧G and ⌥⌘G mean nothing here', () => {
  assert.deepEqual(mac(key('g', { metaKey: true })), { id: 'group' });
  assert.deepEqual(linux(key('g', { ctrlKey: true })), { id: 'group' });
  assert.equal(mac(key('g', { ctrlKey: true })), undefined);
  assert.equal(mac(key('G', { metaKey: true, shiftKey: true })), undefined);
  assert.equal(mac(key('©', { code: 'KeyG', metaKey: true, altKey: true })), undefined);
});

test('⌘O and Ctrl O are the new-tab field\'s Open file…; Shift or Alt makes them something else', () => {
  assert.deepEqual(mac(key('o', { metaKey: true })), { id: 'open-file' });
  assert.deepEqual(linux(key('o', { ctrlKey: true })), { id: 'open-file' });
  assert.equal(mac(key('o', { ctrlKey: true })), undefined);
  assert.equal(linux(key('o', { metaKey: true })), undefined);
  assert.equal(linux(key('O', { ctrlKey: true, shiftKey: true })), undefined);
  assert.equal(linux(key('o', { ctrlKey: true, altKey: true, code: 'KeyO' })), undefined);
});
