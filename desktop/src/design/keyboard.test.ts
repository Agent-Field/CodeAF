import test from 'node:test';
import assert from 'node:assert/strict';

// keyboard.ts reads navigator.platform at import; the matcher takes the platform as an argument.
Object.defineProperty(globalThis, 'navigator', { value: { platform: 'Linux x86_64' }, configurable: true });
const { spellShortcut, shortcutOf, formatShortcut, shellShortcuts, newTerminalShortcut, isNewTerminalShortcut } = await import('./keyboard.ts');

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
  const ids = [['s', {}, 'rail'], ['b', {}, 'rail'], ['f', { shiftKey: true }, 'focus'], ['k', { shiftKey: true }, 'tasks-panel'], ['y', {}, 'history'], [',', {}, 'settings'], ['/', {}, 'models'], ['k', {}, 'palette']] as const;
  for (const [k, over, id] of ids) assert.deepEqual(mac(key(k, { metaKey: true, ...over })), { id });
});

test('Tasks panel uses ⌘⇧K or Ctrl Shift K without claiming the plain palette chord', () => {
  for (const [matcher, primary] of [[mac, { metaKey: true }], [linux, { ctrlKey: true }]] as const) {
    assert.deepEqual(matcher(key('K', { ...primary, shiftKey: true })), { id: 'tasks-panel' });
    assert.deepEqual(matcher(key('k', primary)), { id: 'palette' });
    assert.equal(matcher(key('K', { ...primary, shiftKey: true, altKey: true })), undefined);
  }
  assert.equal(mac(key('K', { ctrlKey: true, shiftKey: true })), undefined);
  assert.equal(linux(key('K', { metaKey: true, shiftKey: true })), undefined);
  assert.equal(shellShortcuts.tasks, formatShortcut('⌘/Ctrl ⇧ K'));
  assert.equal(shellShortcuts.tasks, 'Ctrl Shift K');
});

test('Tasks panel tooltip formats the chord for a Mac', async () => {
  const original = globalThis.navigator;
  Object.defineProperty(globalThis, 'navigator', { value: { platform: 'MacIntel' }, configurable: true });
  try {
    // A fresh module reads the Mac platform without changing the Linux matcher's defaults.
    const macKeyboard = await import(new URL('./keyboard.ts?mac-shortcuts', import.meta.url).href);
    assert.equal(macKeyboard.shellShortcuts.tasks, macKeyboard.formatShortcut('⌘/Ctrl ⇧ K'));
    assert.equal(macKeyboard.shellShortcuts.tasks, '⌘ ⇧ K');
    assert.equal(macKeyboard.newTerminalShortcut, '⌃`');
    assert.equal(macKeyboard.shellShortcuts.openFile, '⌘O');
  } finally {
    Object.defineProperty(globalThis, 'navigator', { value: original, configurable: true });
  }
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
  assert.deepEqual(inTerminal(key('`', { code: 'Backquote', ctrlKey: true })), { id: 'terminal-new' });
  assert.deepEqual(inTerminal(key('Tab', { ctrlKey: true })), { id: 'switch' });
  assert.deepEqual(inTerminal(key('Tab', { ctrlKey: true, shiftKey: true })), { id: 'switch-back' });
  assert.deepEqual(inTerminal(key('T', { ctrlKey: true, shiftKey: true })), { id: 'new' });
  assert.deepEqual(inTerminal(key('W', { ctrlKey: true, shiftKey: true })), { id: 'close' });
  assert.deepEqual(inTerminal(key('A', { ctrlKey: true, shiftKey: true })), { id: 'overview' });
  assert.deepEqual(inTerminal(key('K', { ctrlKey: true, shiftKey: true })), { id: 'tasks-panel' });
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

const writing = { tagName: 'TEXTAREA', value: 'words' };
const commandField = { tagName: 'INPUT', value: 'fix', getAttribute: (name: string) => (name === 'role' ? 'combobox' : null) };
const shellField = { tagName: 'TEXTAREA', value: 'ls', closest: () => ({}) };

test('⌃` is terminal-new on every platform and ⌘O is open-file; a prose field with words keeps both', async () => {
  for (const matcher of [mac, linux]) {
    assert.deepEqual(matcher(key('`', { code: 'Backquote', ctrlKey: true })), { id: 'terminal-new' });
    assert.equal(matcher(key('`', { code: 'Backquote', ctrlKey: true, target: writing })), undefined);
    assert.equal(matcher(key('`', { code: 'Backquote', metaKey: true })), undefined);
    assert.equal(matcher(key('`', { code: 'Backquote', ctrlKey: true, shiftKey: true })), undefined);
  }
  assert.equal(isNewTerminalShortcut(key('`', { code: 'Backquote', ctrlKey: true })), true);
  assert.equal(isNewTerminalShortcut(key('`', { code: 'Backquote', ctrlKey: true, target: writing })), false);
  assert.equal(mac(key('o', { metaKey: true, target: writing })), undefined);
  assert.equal(linux(key('o', { ctrlKey: true, target: writing })), undefined);
  // An empty field is not typing, so the chords still belong to the shell.
  assert.deepEqual(mac(key('o', { metaKey: true, target: { tagName: 'INPUT', value: '' } })), { id: 'open-file' });
  assert.deepEqual(linux(key('`', { code: 'Backquote', ctrlKey: true, target: { tagName: 'TEXTAREA', value: '' } })), { id: 'terminal-new' });
  // Shell 3f draws both hints beside a typed query, so the command field keeps them.
  assert.deepEqual(mac(key('o', { metaKey: true, target: commandField })), { id: 'open-file' });
  assert.deepEqual(linux(key('`', { code: 'Backquote', ctrlKey: true, target: commandField })), { id: 'terminal-new' });
  // Inside a terminal the hidden textarea keeps ⌃`, even when it holds the current line.
  assert.deepEqual(inTerminal(key('`', { code: 'Backquote', ctrlKey: true, target: shellField })), { id: 'terminal-new' });
  assert.equal(inTerminal(key('o', { ctrlKey: true, target: shellField })), undefined);

  const { buildSections, flatRows } = await import('../features/tabs/kinds/newtab/rows.ts');
  const start = flatRows(buildSections({ query: 'fix', tabs: [], closed: [], files: [], terminal: true, terminalShortcut: newTerminalShortcut, fileShortcut: shellShortcuts.openFile }));
  const terminal = start.find(row => row.kind === 'terminal');
  const file = start.find(row => row.kind === 'openfile');
  assert.equal(terminal?.label, 'New terminal');
  assert.equal(terminal?.hint, newTerminalShortcut);
  assert.equal(file?.label, 'Open file…');
  assert.equal(file?.hint, shellShortcuts.openFile);
  assert.equal(newTerminalShortcut, 'Ctrl `');
  assert.equal(shellShortcuts.openFile, 'Ctrl O');
});

test('⌘Z (Ctrl Z on Linux) is the structural Undo; ⌘⇧Z and ⌥⌘Z are not', () => {
  assert.deepEqual(mac(key('z', { metaKey: true })), { id: 'undo' });
  assert.deepEqual(linux(key('z', { ctrlKey: true })), { id: 'undo' });
  assert.equal(mac(key('Z', { metaKey: true, shiftKey: true })), undefined);
  assert.equal(mac(key('Ω', { code: 'KeyZ', metaKey: true, altKey: true })), undefined);
  assert.equal(mac(key('z', { ctrlKey: true })), undefined);
});

test('Iteration 2 chords have distinct registry ids on Mac and Linux (P-5)', () => {
  const cases = [
    [true, 'j', { metaKey: true }, 'next-up'],
    [false, 'j', { ctrlKey: true }, 'next-up'],
    [true, '[', { metaKey: true, code: 'BracketLeft' }, 'back'],
    [true, ']', { metaKey: true, code: 'BracketRight' }, 'forward'],
    [false, 'ArrowLeft', { altKey: true }, 'back'],
    [false, 'ArrowRight', { altKey: true }, 'forward'],
    [false, '[', { ctrlKey: true, code: 'BracketLeft' }, undefined],
    [false, ']', { ctrlKey: true, code: 'BracketRight' }, undefined],
    [true, 'i', { metaKey: true }, undefined],
    [false, 'i', { ctrlKey: true }, undefined],
  ] as const;
  for (const [mac, chord, modifiers, id] of cases) {
    assert.deepEqual(shortcutOf(key(chord, modifiers), mac), id ? { id } : undefined, `${mac ? 'Mac' : 'Linux'} ${chord}`);
    assert.equal(shortcutOf(key(chord, { ...modifiers, shiftKey: true }), mac)?.id === id && !!id, false);
  }
});

test('Home Up is up-level; chat arrows keep turn navigation and composers keep caret keys', () => {
  for (const mac of [true, false]) {
    const primary = mac ? { metaKey: true } : { ctrlKey: true };
    for (const [chord, id] of [['ArrowUp', 'turn-previous'], ['ArrowDown', 'turn-next']] as const) {
      assert.deepEqual(shortcutOf(key(chord, primary), { mac }), { id });
    }
    assert.deepEqual(shortcutOf(key('ArrowUp', primary), { mac, home: true }), { id: 'up-level' });
    assert.equal(shortcutOf(key('ArrowDown', primary), { mac, home: true }), undefined);
    for (const value of ['', 'draft']) {
      assert.equal(shortcutOf(key('ArrowUp', { ...primary, target: { tagName: 'TEXTAREA', value } }), { mac, home: true }), undefined);
    }
    assert.equal(shortcutOf(key('j', { ...primary, altKey: true }), { mac }), undefined);
  }
  assert.equal(shortcutOf(key('j', { ctrlKey: true }), { mac: false, terminal: true }), undefined);
});

test('spellShortcut: glyphs run together on a Mac, words join with + elsewhere (C-CTRL-14)', () => {
  const original = globalThis.navigator;
  try {
    Object.defineProperty(globalThis, 'navigator', { value: { platform: 'MacIntel', userAgent: 'Mac' }, configurable: true });
    assert.equal(spellShortcut('⌘/Ctrl ⇧ C'), '⌘⇧C');
    Object.defineProperty(globalThis, 'navigator', { value: { platform: 'Linux x86_64', userAgent: 'X11' }, configurable: true });
    assert.equal(spellShortcut('⌘/Ctrl ⇧ C'), 'Ctrl+Shift+C');
    assert.equal(spellShortcut('↵'), '↵');
  } finally {
    Object.defineProperty(globalThis, 'navigator', { value: original, configurable: true });
  }
});

test('shell recognizers use the host primary modifier on both platforms', () => {
  for (const [matcher, primary] of [[mac, { metaKey: true }], [linux, { ctrlKey: true }]] as const) {
    for (const [chord, id] of [['g', 'group'], ['z', 'undo'], ['n', 'new-window'], ['o', 'open-file'], ['0', 'place-home'], ['p', 'goto']] as const) {
      assert.deepEqual(matcher(key(chord, primary)), { id });
      assert.equal(matcher(key(chord, { ...primary, altKey: true })), undefined);
    }
    assert.deepEqual(matcher(key('P', { ...primary, shiftKey: true })), { id: 'all-places' });
    assert.deepEqual(matcher(key('C', { ...primary, shiftKey: true })), { id: 'copy-link' });
    assert.equal(matcher(key('c', primary)), undefined);
    assert.equal(matcher(key('C', { ...primary, shiftKey: true, altKey: true })), undefined);
    for (const target of [{ tagName: 'TEXTAREA', value: '' }, { tagName: 'INPUT', value: 'draft' }, { isContentEditable: true }, { closest: () => ({}) }]) {
      assert.equal(matcher(key('z', { ...primary, target })), undefined);
    }
  }
});

test('all nine place slots preserve tab jumps and use layout-independent digits', () => {
  for (let index = 1; index <= 9; index++) {
    for (const code of [`Digit${index}`, `Numpad${index}`]) {
      assert.deepEqual(mac(key('symbol', { code, ctrlKey: true })), { id: 'place-jump', index });
      assert.deepEqual(linux(key('symbol', { code, altKey: true })), { id: 'place-jump', index });
      assert.equal(mac(key('symbol', { code, ctrlKey: true, shiftKey: true })), undefined);
      assert.equal(linux(key('symbol', { code, altKey: true, shiftKey: true })), undefined);
    }
    assert.deepEqual(mac(key(String(index), { metaKey: true })), { id: 'jump', index });
    assert.deepEqual(linux(key(String(index), { ctrlKey: true })), { id: 'jump', index });
  }
});

test('Quick Look is bare Space outside writing fields and buttons on both platforms', () => {
  for (const matcher of [mac, linux]) {
    assert.deepEqual(matcher(key(' ', { code: 'Space' })), { id: 'quick-look' });
    for (const target of [{ tagName: 'INPUT', value: '' }, { tagName: 'TEXTAREA', value: '' }, { tagName: 'BUTTON' }, { tagName: 'SELECT' }, { isContentEditable: true }, { tagName: 'SPAN', closest: () => ({}) }]) {
      assert.equal(matcher(key(' ', { code: 'Space', target })), undefined);
    }
    for (const modifier of ['metaKey', 'ctrlKey', 'altKey', 'shiftKey']) {
      assert.equal(matcher(key(' ', { code: 'Space', [modifier]: true })), undefined);
    }
  }
});

test('Copy link leaves terminal copy to the shell and file copy to its surface handler', () => {
  for (const mac of [true, false]) {
    assert.equal(shortcutOf(key(' ', { code: 'Space' }), { mac, terminal: true }), undefined);
    const primary = mac ? { metaKey: true } : { ctrlKey: true };
    assert.equal(shortcutOf(key('C', { ...primary, shiftKey: true }), { mac, terminal: true }), undefined);
    assert.equal(shortcutOf(key('C', { ...primary, shiftKey: true, target: shellField }), { mac }), undefined);
  }
});

test('Iteration 2 keeps brackets as focus history even on Home; Home Up uses the arrow', () => {
  assert.deepEqual(shortcutOf(key('[', { code: 'BracketLeft', metaKey: true }), { mac: true, home: true }), { id: 'back' });
  assert.deepEqual(shortcutOf(key('ArrowUp', { metaKey: true }), { mac: true, home: true }), { id: 'up-level' });
  assert.deepEqual(shortcutOf(key('ArrowLeft', { altKey: true }), { mac: false, home: true }), { id: 'back' });
  assert.deepEqual(shortcutOf(key('ArrowUp', { ctrlKey: true }), { mac: false, home: true }), { id: 'up-level' });
});
