import test from 'node:test';
import assert from 'node:assert/strict';
import { homeMenu, placeMenu, type MenuContext, type MenuPlace } from './placeMenu.ts';
import type { PlaceActions } from '../place-actions.ts';
import type { MenuEntry } from '../../../components/ui/Menu.tsx';

const place = (extra: Partial<MenuPlace> = {}): MenuPlace => ({ id: 'p1', name: 'Reading', tint: 'tide', ...extra });

function wired(extra: Partial<PlaceActions> = {}): PlaceActions {
  return {
    goTo: () => undefined,
    quickLook: () => undefined,
    goToInNewWindow: () => undefined,
    rename: () => undefined,
    setTint: () => undefined,
    chooseAnotherParent: () => undefined,
    chooseMergeTarget: () => undefined,
    pin: () => undefined,
    unpin: () => undefined,
    archive: () => undefined,
    restore: () => undefined,
    remove: () => undefined,
    setDecide: () => undefined,
    ...extra,
  };
}

const editing: MenuContext = { canRename: true, startRename: () => undefined, startDelete: () => undefined, newWindowHint: '⌘↵' };

type Row = { kind: string; id?: string; label?: string; shortcut?: string; icon?: string; iconSlot?: boolean; danger?: boolean; selected?: string; swatches?: string[] };

function shape(entries: readonly MenuEntry[]): Row[] {
  return entries.map(entry => {
    if (entry.kind === 'separator') return { kind: 'separator', id: entry.id };
    if (entry.kind === 'swatches') return { kind: 'swatches', id: entry.id, label: entry.label, icon: entry.icon, selected: entry.selected, swatches: entry.options.map(option => option.id) };
    if (entry.kind === 'submenu') return { kind: 'submenu', id: entry.id, label: entry.label, icon: entry.icon, iconSlot: entry.iconSlot };
    return { kind: 'action', id: entry.id, label: entry.label, shortcut: entry.shortcut, icon: entry.icon, iconSlot: entry.iconSlot, danger: entry.danger };
  });
}

test('a tile menu follows Places 8f: two rules, five swatches, Pin to rail, danger words on Delete', () => {
  const rows = shape(placeMenu(place({ decide: { alwaysAsk: false, threshold: 90 } }), wired(), editing));
  assert.deepEqual(rows.map(row => row.kind === 'separator' ? '—' : row.label), [
    'Go to', 'Quick Look', 'Open in new window', '—',
    'Rename', 'Tint', 'Add to another place…', 'Merge into…', 'Pin to rail', '—',
    'Always ask me', 'Decision confidence…', '—',
    'Archive', 'Delete place…',
  ]);
  const go = rows[0];
  const look = rows[1];
  const window = rows[2];
  const tint = rows[5];
  const pin = rows[8];
  const deleted = rows[14];
  assert.equal(go.shortcut, '↵');
  assert.equal(go.icon, 'arrow');
  assert.equal(look.shortcut, 'Space');
  assert.equal(look.icon, 'eye');
  assert.equal(window.shortcut, '⌘↵');
  assert.equal(window.icon, 'appWindow');
  assert.deepEqual(tint.swatches, ['tide', 'rose', 'sage', 'sand', 'iris']);
  assert.equal(tint.selected, 'tide');
  assert.equal(tint.icon, 'palette');
  assert.equal(pin.label, 'Pin to rail');
  assert.equal(pin.icon, 'pin');
  assert.equal(deleted.danger, true);
  assert.equal(deleted.icon, undefined);
  assert.equal(deleted.iconSlot, true);
  assert.equal(rows.filter(row => row.label === 'Graphite').length, 0);
});

test('a pinned place says Unpin, and a graphite place checks no swatch', () => {
  const pinned = shape(placeMenu(place({ pinned: true }), wired(), editing));
  assert.equal(pinned.find(row => row.id === 'unpin')?.label, 'Unpin');
  assert.equal(pinned.find(row => row.id === 'pin'), undefined);
  const graphite = shape(placeMenu(place({ tint: 'graphite' }), wired(), editing));
  assert.equal(graphite.find(row => row.id === 'tint')?.selected, undefined);
});

test('the Home heading menu keeps Open in new window and drops Go to and Quick Look', () => {
  const rows = shape(homeMenu(place(), wired(), editing));
  const labels = rows.filter(row => row.kind !== 'separator').map(row => row.label);
  assert.equal(labels[0], 'Open in new window');
  assert.equal(labels.includes('Go to'), false);
  assert.equal(labels.includes('Quick Look'), false);
  assert.equal(labels.at(-2), 'Archive');
  assert.equal(labels.at(-1), 'Delete place…');
});

test('the Home tab menu is the same list without Archive or Delete, which the caller does not wire', () => {
  const { archive, restore, remove, ...tab } = wired();
  void archive; void restore; void remove;
  const labels = shape(homeMenu(place(), tab, { ...editing, startDelete: undefined })).filter(row => row.kind !== 'separator').map(row => row.label);
  assert.deepEqual(labels, ['Open in new window', 'Rename', 'Tint', 'Add to another place…', 'Merge into…', 'Pin to rail']);
});

test('an archived tile offers Restore instead of Pin and Archive, and no decision rows', () => {
  const labels = shape(placeMenu(place({ archived: true, decide: { alwaysAsk: true, threshold: 90 } }), wired(), editing))
    .filter(row => row.kind !== 'separator').map(row => row.label);
  assert.equal(labels.includes('Pin to rail'), false);
  assert.equal(labels.includes('Unpin'), false);
  assert.equal(labels.includes('Archive'), false);
  assert.equal(labels.includes('Always ask me'), false);
  assert.deepEqual(labels.slice(-2), ['Restore', 'Delete place…']);
});

test('a verb with no handler is absent, and a separator never leads or doubles', () => {
  const rows = shape(placeMenu(place(), { goTo: () => undefined }, {}));
  assert.deepEqual(rows, [{ kind: 'action', id: 'go', label: 'Go to', shortcut: '↵', icon: 'arrow', iconSlot: undefined, danger: undefined }]);
});

test('read-only keeps navigation and drops every write, including Delete', () => {
  const labels = shape(placeMenu(place(), wired(), { ...editing, readOnly: true })).map(row => row.label ?? '—');
  assert.deepEqual(labels, ['Go to', 'Quick Look', 'Open in new window']);
});

test('choosing an item calls only that verb', () => {
  const calls: string[] = [];
  const entries = placeMenu(place(), {
    goTo: id => calls.push(`go:${id}`),
    goToInNewWindow: () => undefined,
    chooseAnotherParent: id => calls.push(`add:${id}`),
    pin: id => calls.push(`pin:${id}`),
    archive: id => calls.push(`archive:${id}`),
    remove: () => undefined,
    setTint: (id, tint) => calls.push(`tint:${id}:${tint}`),
  }, { canRename: true, startRename: id => calls.push(`rename:${id}`), startDelete: id => calls.push(`delete:${id}`), newWindowHint: 'Ctrl ↵' });
  const pick = (id: string) => {
    const entry = entries.find(item => item.kind !== 'separator' && item.id === id);
    assert.ok(entry && entry.kind !== 'separator');
    if (entry.kind === 'swatches') entry.onSelect('rose');
    else if (entry.kind !== 'submenu') entry.onSelect();
  };
  pick('go');
  pick('rename');
  pick('tint');
  pick('another-parent');
  pick('pin');
  pick('archive');
  pick('delete');
  assert.deepEqual(calls, ['go:p1', 'rename:p1', 'tint:p1:rose', 'add:p1', 'pin:p1', 'archive:p1', 'delete:p1']);
  const opened = entries.find(item => item.kind !== 'separator' && item.id === 'new-window');
  assert.ok(opened && opened.kind !== 'separator' && opened.kind !== 'swatches' && opened.kind !== 'submenu');
  assert.equal(opened.shortcut, 'Ctrl ↵');
});
