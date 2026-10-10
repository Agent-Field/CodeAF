import test from 'node:test';
import assert from 'node:assert/strict';
import { createLabel, fuzzy, paletteView, windowSlice, type PaletteData, type PaletteRow } from './paletteModel.ts';
import type { PlaceRowModel } from '../shell/contracts.ts';

const now = new Date('2026-10-10T12:00:00.000Z');
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();
const MIN = 60_000;

function place(id: string, name: string, extra: Partial<PlaceRowModel> = {}): PlaceRowModel {
  return { id, name, tint: 'tide', pinned: false, archived: false, ...extra };
}

function data(places: PlaceRowModel[], tree: Record<string, string[]>): PaletteData {
  return { places, childrenOf: new Map(Object.entries(tree)), now };
}

const places = [
  place('pl_code', 'codeaf', { meta: '2 inside', lastOpenedAt: ago(5 * MIN) }),
  place('pl_soft', 'Software', { meta: '1 inside', lastOpenedAt: ago(26 * 60 * MIN) }),
  place('pl_parse', 'Config parser', { meta: '28 chats', lastOpenedAt: ago(3 * 24 * 60 * MIN) }),
  place('pl_notes', 'Notes', { meta: '4 chats' }),
  place('pl_old', 'Old', { archived: true, lastOpenedAt: ago(MIN) }),
];
const graph = data(places, { root: ['pl_code', 'pl_notes', 'pl_old'], pl_code: ['pl_soft'], pl_soft: ['pl_parse'] });
const places_ = (rows: PaletteRow[]) => rows.flatMap(r => (r.kind === 'place' ? [r] : []));

test('recent: newest first with relative words, archived and never-visited places left out', () => {
  const recent = places_(paletteView(graph, '').rows).filter(r => r.section === 'recent');
  assert.deepEqual(recent.map(r => r.place.id), ['pl_code', 'pl_soft', 'pl_parse']);
  assert.deepEqual(recent.map(r => r.meta), ['5m ago', 'yesterday', '3d ago']);
  assert.equal(recent[2].context, 'codeaf › Software');
});

test('tree: only the top level is open by default and rows say "N inside" or "N chats"', () => {
  const tree = places_(paletteView(graph, '').rows).filter(r => r.section === 'all');
  assert.deepEqual(tree.map(r => [r.place.id, r.depth, r.expanded, r.meta]), [
    ['pl_code', 0, true, '2 inside'], ['pl_soft', 1, false, '1 inside'], ['pl_notes', 0, false, '4 chats'],
  ]);
});

test('tree: the expanded set opens a deeper place, and a place under two parents is two rows with distinct keys', () => {
  const dual = data([...places, place('pl_x', 'Shared')], { root: ['pl_code', 'pl_notes'], pl_code: ['pl_x'], pl_notes: ['pl_x'] });
  const rows = places_(paletteView(dual, '', new Map([['pl_code/pl_soft', true]])).rows).filter(r => r.section === 'all');
  assert.equal(rows.filter(r => r.place.id === 'pl_x').length, 2);
  assert.equal(new Set(rows.map(r => r.key)).size, rows.length);
  const collapsed = places_(paletteView(graph, '', new Map([['pl_code', false]])).rows).filter(r => r.section === 'all');
  assert.deepEqual(collapsed.map(r => r.place.id), ['pl_code', 'pl_notes']);
});

test('tree: a cycle in a damaged graph ends the walk', () => {
  const loop = data(places.slice(0, 2), { root: ['pl_code'], pl_code: ['pl_soft'], pl_soft: ['pl_code'] });
  assert.equal(places_(paletteView(loop, '', new Map([['pl_code/pl_soft', true]])).rows).filter(r => r.section === 'all').length, 2);
});

test('search: fuzzy on the name, with matched offsets, and a name match outranks a path-only match', () => {
  const view = paletteView(graph, 'cfg');
  assert.equal(view.searching, true);
  const rows = places_(view.rows);
  assert.equal(rows[0].place.id, 'pl_parse');
  assert.deepEqual(rows[0].matched, [0, 3, 5]);
  const byPath = places_(paletteView(graph, 'codeaf soft').rows);
  // Config parser sits under Software too, so its trail matches as well, but after the place the words name.
  assert.deepEqual(byPath.map(r => r.place.id), ['pl_soft', 'pl_parse']);
  assert.deepEqual(byPath[0].matched, []);
  assert.equal(byPath[0].context, 'codeaf');
});

test('search: a prefix beats a scattered match, and equal scores keep the graph order', () => {
  assert.ok(fuzzy('not', 'notes')!.score > fuzzy('not', 'a network tool')!.score);
  const twins = data([place('a', 'Report'), place('b', 'Report')], { root: ['b', 'a'] });
  assert.deepEqual(places_(paletteView(twins, 'report').rows).map(r => r.place.id), ['a', 'b']);
});

test('search: archived places are never found', () => {
  assert.deepEqual(paletteView(graph, 'old').rows.map(r => r.kind), ['create']);
});

test('create: nothing matching offers one Create row with the trimmed name; a match offers none', () => {
  const none = paletteView(graph, '  Quarterly plan ');
  assert.deepEqual(none.rows, [{ kind: 'create', key: 'create', name: 'Quarterly plan', label: createLabel('Quarterly plan') }]);
  assert.equal(createLabel('Q3'), 'Create “Q3”');
  assert.ok(paletteView(graph, 'notes').rows.every(r => r.kind === 'place'));
  assert.ok(paletteView(graph, '   ').rows.every(r => r.kind === 'place'));
});

test('window slice: draws the visible rows plus overscan and pads the rest', () => {
  assert.deepEqual(windowSlice(200, 0, 320, 32, 4), { start: 0, end: 14, padTop: 0, padBottom: 186 * 32 });
  const mid = windowSlice(200, 3200, 320, 32, 4);
  assert.deepEqual([mid.start, mid.end], [96, 114]);
  assert.equal(mid.padTop + (mid.end - mid.start) * 32 + mid.padBottom, 200 * 32);
  assert.deepEqual(windowSlice(200, 99999, 320, 32, 4).end, 200);
  assert.deepEqual(windowSlice(0, 0, 320, 32), { start: 0, end: 0, padTop: 0, padBottom: 0 });
});

test('a 200-place fixture filters in under 5ms', () => {
  const many = Array.from({ length: 200 }, (_, i) => place(`pl_${i}`, `Project ${i} ${i % 7 ? 'alpha' : 'config parser'}`, { meta: '3 chats' }));
  const big = data(many, { root: many.slice(0, 20).map(p => p.id), ...Object.fromEntries(many.slice(0, 20).map((p, i) => [p.id, many.slice(20 + i * 9, 29 + i * 9).map(c => c.id)])) });
  paletteView(big, 'cp'); // warm the JIT so the timing reads the filter, not compilation
  const runs = Array.from({ length: 20 }, () => {
    const t = performance.now();
    paletteView(big, 'cfg pars');
    return performance.now() - t;
  }).sort((a, b) => a - b);
  assert.ok(runs[10] < 5, `median ${runs[10]}ms`);
});
