import assert from 'node:assert/strict';
import { test } from 'node:test';
import { addToPlaceEntry, placesToOffer } from './addToPlace.ts';
import type { PlaceView } from '../places/wire.ts';
import type { HistoryItem } from './types.ts';

const item = { id: 'chat1', sessionFile: '/h/chat1/transcript.jsonl' } as HistoryItem;
const place = (id: string, extra: Partial<PlaceView> = {}) => ({ id, name: id.toUpperCase(), archived: false, pinned: false, ...extra }) as PlaceView;

test('pinned places come first, then recent, at most ten, archived never', () => {
  const nodes = [place('old', { lastOpenedAt: '2026-01-01' }), place('new', { lastOpenedAt: '2026-02-01' }), place('pin', { pinned: true }), place('gone', { archived: true })];
  assert.deepEqual(placesToOffer(nodes).map(p => p.id), ['pin', 'new', 'old']);
  assert.equal(placesToOffer(Array.from({ length: 15 }, (_, i) => place(`p${i}`))).length, 10);
});

test('Add to place lists places and posts the membership', () => {
  const calls: unknown[] = [];
  const entry = addToPlaceEntry(item, [place('a'), place('b')], (p, c) => calls.push(['add', p, c]), c => calls.push(['all', c]));
  assert.ok(entry && entry.kind === 'submenu');
  const labels = entry.items.map(e => e.kind === 'separator' ? '-' : (e as { label: string }).label);
  assert.deepEqual(labels, ['A', 'B', '-', 'All places…']);
  for (const e of entry.items) if (e.kind !== 'separator' && e.kind !== 'submenu' && e.kind !== 'swatches') e.onSelect();
  assert.deepEqual(calls, [['add', 'a', 'chat1'], ['add', 'b', 'chat1'], ['all', 'chat1']]);
});

test('the item is absent while there are no places', () => {
  assert.equal(addToPlaceEntry(item, [], () => {}, () => {}), undefined);
});
