import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import type { MenuEntry } from '../../components/ui/index.ts';
import type { TabsApi } from '../tabs/context.ts';
import type { Tab } from '../tabs/types.ts';
import { clearTerminalTabMenu, finishedHeaderMenu, offerTerminalTabMenu, placeFinishedJobItems, terminalTabMenuItems, type FinishedJobHandlers } from './tabMenu.ts';

const label = (entry: MenuEntry) => (entry.kind === 'separator' ? '|' : entry.label);

function tab(id: string): Tab {
  return { id, kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false };
}

function api(pane: Tab): TabsApi {
  return { state: { tabs: [pane], groups: [], activeId: pane.id, closed: [], nextNumber: 2, recentIds: [] } } as TabsApi;
}

function action(items: MenuEntry[], id: string) {
  const entry = items.find(item => item.kind !== 'separator' && item.kind !== 'submenu' && item.id === id);
  if (!entry || entry.kind === 'separator' || entry.kind === 'submenu') throw new Error(`missing ${id}`);
  return entry;
}

test('finished job yields Open log, Run again, Remove job; running yields none', () => {
  const pane = tab('job');
  const openLog = () => {};
  const rerun = () => {};
  const remove = () => {};
  const handlers: FinishedJobHandlers = { onOpenLog: openLog, onRerun: rerun, onRemove: remove };
  offerTerminalTabMenu(pane.id, () => ({ finished: true, ...handlers }));
  try {
    // terminalKind.menuItems is this function. The kind module also loads the pane, so the test reads the wiring from source.
    const kind = readFileSync(new URL('../tabs/kinds/terminal.ts', import.meta.url), 'utf8');
    assert.match(kind, /menuItems:\s*terminalTabMenuItems/);
    const items = terminalTabMenuItems(pane, api(pane));
    assert.deepEqual(items.map(label), ['Open log', 'Run again', 'Remove job']);
    assert.equal(action(items, 'log').icon, 'scrollText');
    assert.equal(action(items, 'rerun').icon, 'reload');
    assert.equal(action(items, 'remove').danger, true);
    // The header menu is built from these same functions, so the two rows cannot point at different actions.
    const header = finishedHeaderMenu({ ...handlers, onClose: () => {} }, '⌘W');
    assert.equal(action(items, 'log').onSelect, action(header, 'log').onSelect);
    assert.equal(action(items, 'rerun').onSelect, action(header, 'rerun').onSelect);
    assert.equal(action(items, 'remove').onSelect, action(header, 'remove').onSelect);
    assert.deepEqual(header.map(label), ['Open log', 'Run again', '|', 'Close tab', 'Remove job']);
    const placed = placeFinishedJobItems([
      { id: 'pin', label: 'Pin tab', onSelect: () => {} },
      { kind: 'separator', id: 'close-separator' },
      { id: 'close', label: 'Close tab', onSelect: () => {} },
      { id: 'close-others', label: 'Close other tabs', onSelect: () => {} },
    ], items);
    assert.deepEqual(placed.map(label), ['Pin tab', 'Open log', 'Run again', '|', 'Close tab', 'Remove job', 'Close other tabs']);

    offerTerminalTabMenu(pane.id, () => ({ finished: false, ...handlers }));
    assert.deepEqual(terminalTabMenuItems(pane, api(pane)), []);
  } finally {
    clearTerminalTabMenu(pane.id);
  }
});
