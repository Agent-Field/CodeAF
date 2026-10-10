import test from 'node:test';
import assert from 'node:assert/strict';
import type { MenuEntry } from '../../../components/ui/Menu.tsx';
import type { PlaceRailProps } from '../../shell/PlaceRail.tsx';
import type { PlaceRowModel } from './contracts.ts';
import type { PlacesShell } from './PlacesShell.tsx';
import { placeSwitcher } from './placeSwitcher.ts';

const place = (id: string, name: string, over: Partial<PlaceRowModel> = {}): PlaceRowModel => ({
  id, name, tint: 'tide', pinned: false, archived: false, ...over,
});

function labels(items: MenuEntry[]): string[] {
  return items.flatMap(item => (item.kind === 'separator' || item.kind === 'submenu' || item.kind === 'swatches' ? [] : [item.label]));
}

function rail(over: Partial<PlaceRailProps> = {}): PlaceRailProps {
  return {
    inert: false, peeking: false, onToggle: () => {},
    now: { active: true, shortcut: '⌃0', onGo: () => {} },
    actions: { go: () => {}, close: () => {}, closeAll: () => {}, closeOthers: () => {} },
    slotShortcut: index => `⌃${index}`, closeShortcut: '⌘⇧W', newWindowShortcut: '⌘↵',
    allPlaces: { active: false, shortcut: '⌘⇧P', onOpen: () => {} },
    ...over,
  };
}

test('the collapsed switcher lists Now, the rail places and All places, and never Inbox', () => {
  const { items } = placeSwitcher({ place: 'now' } as PlacesShell, rail({
    sections: {
      pinned: [place('pl_codeaf', 'codeaf', { pinned: true })],
      open: [place('pl_config', 'Config parser', { parentName: 'codeaf', status: 'waiting', statusLabel: '2 need you in Config parser' })],
    },
  }));
  assert.deepEqual(labels(items), ['Now', 'codeaf', 'Config parser · codeaf', 'All places']);
  assert.equal(items.some(item => item.id === 'inbox' || ('label' in item && item.label.includes('Inbox'))), false);
  const now = items.find(item => item.id === 'now');
  const config = items.find(item => item.id === 'pl_config');
  assert.equal(now && 'checked' in now ? now.checked : undefined, true);
  assert.equal(config && 'checked' in config ? config.checked : undefined, false);
});

test('with no places the switcher is Now and All places', () => {
  const { items, alert } = placeSwitcher({ place: 'now' } as PlacesShell, rail({ sections: { pinned: [], open: [] } }));
  assert.deepEqual(labels(items), ['Now', 'All places']);
  assert.equal(alert, undefined);
});

test('a waiting place that is not the current one still names itself on the switcher', () => {
  const { alert } = placeSwitcher({ place: 'pl_codeaf' } as PlacesShell, rail({
    sections: {
      pinned: [place('pl_codeaf', 'codeaf', { pinned: true })],
      open: [place('pl_config', 'Config parser', { status: 'waiting', statusLabel: '2 need you in Config parser' })],
    },
  }));
  assert.equal(alert, '2 need you in Config parser');
});

test('the switcher keeps distinct nonempty section headings and numbers pinned before open', () => {
  const { items } = placeSwitcher({ place: 'now' } as PlacesShell, rail({ sections: {
    pinned: [place('a', 'A'), place('b', 'B')], open: [place('c', 'C')],
  } }));
  assert.deepEqual(items.filter(item => item.kind === 'separator' && item.label).map(item => item.kind === 'separator' ? item.label : ''), ['Pinned', 'Open']);
  const shortcuts = items.filter(item => item.kind !== 'separator' && 'checked' in item).map(item => 'shortcut' in item ? item.shortcut : '');
  assert.equal(shortcuts.length, 4);
  assert.ok(shortcuts.every(Boolean));
});

test('place decoration preserves its real navigation callback', () => {
  const called: string[] = [];
  const props = rail({ sections: { pinned: [], open: [place('a', 'A')] } });
  props.actions.go = id => called.push(id);
  const { items } = placeSwitcher({ place: 'now' } as PlacesShell, props, () => ({ label: 'A decorated', onSelect: () => called.push('wrong') }));
  const item = items.find(item => item.id === 'a');
  assert.ok(item && 'onSelect' in item);
  if (item && 'onSelect' in item) item.onSelect();
  assert.deepEqual(called, ['a']);
  assert.equal(item && 'label' in item ? item.label : undefined, 'A decorated');
});
