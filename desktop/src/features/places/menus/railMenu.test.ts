import assert from 'node:assert/strict';
import test from 'node:test';
import type { MenuEntry } from '../../../components/ui/index.ts';
import { opensRailMenu, railMenu, type RailMenuActions } from './railMenu.ts';

const label = (entry: MenuEntry) => (entry.kind === 'separator' ? '|' : entry.label);
const calls: string[] = [];
const all: RailMenuActions = {
  goTo: id => calls.push(`go ${id}`), quickLook: id => calls.push(`ql ${id}`), pin: id => calls.push(`pin ${id}`),
  unpin: id => calls.push(`unpin ${id}`), startRename: id => calls.push(`rename ${id}`),
  setTint: (id, tint) => calls.push(`tint ${id} ${tint}`), close: id => calls.push(`close ${id}`), closeOthers: id => calls.push(`others ${id}`),
};

test('the menu lists the design rows in order and every row acts on its own place', () => {
  const items = railMenu({ id: 'p1', tint: 'sage', pinned: false }, all);
  assert.deepEqual(items.map(label), ['Go to', 'Quick Look', '|', 'Pin', 'Rename', 'Tint', '|', 'Close', 'Close all others']);
  for (const entry of items) {
    if (entry.kind === 'separator' || entry.kind === 'submenu') continue;
    if (entry.kind === 'swatches') { assert.equal(entry.selected, 'sage'); entry.onSelect('rose'); } else entry.onSelect();
  }
  assert.deepEqual(calls, ['go p1', 'ql p1', 'pin p1', 'rename p1', 'tint p1 rose', 'close p1', 'others p1']);
});

test('the pin row flips its label and verb for a pinned place', () => {
  calls.length = 0;
  const pin = railMenu({ id: 'p2', tint: 'tide', pinned: true }, all).find(e => e.kind !== 'separator' && e.id === 'unpin');
  assert.ok(pin && pin.kind !== 'swatches' && pin.kind !== 'submenu');
  assert.equal(pin.label, 'Unpin');
  pin.onSelect();
  assert.deepEqual(calls, ['unpin p2']);
});

test('a verb without a handler is absent and no separator is left dangling', () => {
  assert.deepEqual(railMenu({ id: 'p', tint: 'graphite', pinned: false }, {}), []);
  const items = railMenu({ id: 'p', tint: 'graphite', pinned: false }, { close: () => {} });
  assert.deepEqual(items.map(label), ['Close']);
  const tint = railMenu({ id: 'p', tint: 'graphite', pinned: false }, { setTint: () => {} })[0];
  assert.equal(tint.kind === 'swatches' && tint.selected, undefined);
});

test('Shift+F10 and the context-menu key open the menu; plain F10 does not', () => {
  assert.ok(opensRailMenu({ key: 'ContextMenu', shiftKey: false }));
  assert.ok(opensRailMenu({ key: 'F10', shiftKey: true }));
  assert.ok(!opensRailMenu({ key: 'F10', shiftKey: false }));
});

test('wired window, reorder and close hints use the same rail menu builder', () => {
  const called: string[] = [];
  const items = railMenu({ id: 'a', tint: 'tide', pinned: true }, {
    newWindow: id => called.push(`window:${id}`), newWindowShortcut: '⌘↵',
    moveUp: id => called.push(`up:${id}`), close: id => called.push(`close:${id}`), closeShortcut: '⌘⇧W',
    closeOthers: () => {}, closeOthersDisabled: true,
  });
  for (const id of ['new-window', 'up', 'close']) {
    const item = items.find(item => item.id === id);
    assert.ok(item && 'onSelect' in item);
    if (item && 'onSelect' in item) item.onSelect();
  }
  assert.deepEqual(called, ['window:a', 'up:a', 'close:a']);
  const close = items.find(item => item.id === 'close');
  assert.equal(close && 'shortcut' in close ? close.shortcut : undefined, '⌘⇧W');
  const others = items.find(item => item.id === 'close-others');
  assert.equal(others && 'disabled' in others ? others.disabled : undefined, true);
});
