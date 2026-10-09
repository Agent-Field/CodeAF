import test from 'node:test';
import assert from 'node:assert/strict';
import { chooserTitle, chooserView, countLabel, excludedIds, firstChoosable, parentRowKey, relativeWhen, step, type ChooserData } from './chooserModel.ts';
import type { PlaceRowModel } from './contracts.ts';

// Times are built in local time so "yesterday" means the same thing on every machine's clock.
const now = new Date(2026, 9, 9, 9, 0, 0);
const ago = (minutes: number) => new Date(now.getTime() - minutes * 60_000).toISOString();

const place = (id: string, name: string, extra: Partial<PlaceRowModel> = {}): PlaceRowModel =>
  ({ id, name, tint: 'tide', pinned: false, archived: false, ...extra });

// The 6c graph, trimmed: codeaf holds Software, Marketing and Config parser; Reports holds two reports; Marketing is also
// under Personal, so it is drawn twice in the tree.
const places: PlaceRowModel[] = [
  place('pl_codeaf', 'codeaf', { meta: '4 inside' }),
  place('pl_software', 'Software', { meta: '3 inside', parentName: 'codeaf' }),
  place('pl_marketing', 'Marketing', { meta: '2 inside', parentName: 'codeaf', lastOpenedAt: ago(60 * 24), parents: ['pl_codeaf', 'pl_personal'] }),
  place('pl_config', 'Config parser', { parentName: 'codeaf', lastOpenedAt: ago(2), status: 'waiting' }),
  place('pl_lexer', 'Lexer', { parentName: 'Software' }),
  place('pl_reports', 'Reports', { meta: '47 inside' }),
  place('pl_q3', 'Q3 report', { meta: '12 chats', parentName: 'Reports', lastOpenedAt: ago(60 * 24 * 3) }),
  place('pl_q2', 'Q2 report', { meta: '9 chats', parentName: 'Reports', lastOpenedAt: ago(60 * 24 * 10) }),
  place('pl_personal', 'Personal', { meta: '8 chats' }),
  place('pl_old', 'Old', { archived: true, lastOpenedAt: ago(1) }),
];
const childrenOf = new Map<string, readonly string[]>([
  ['root', ['pl_codeaf', 'pl_reports', 'pl_personal']],
  ['pl_codeaf', ['pl_software', 'pl_marketing', 'pl_config']],
  ['pl_software', ['pl_lexer']],
  ['pl_reports', ['pl_q3', 'pl_q2']],
  ['pl_personal', ['pl_marketing']],
]);
const data: ChooserData = { places, childrenOf, now };
const go = { kind: 'go' } as const;
const names = (rows: { place: PlaceRowModel }[]) => rows.map(row => row.place.name);

test('Recent is the three newest, with their parent and a relative time; archived places never appear', () => {
  const view = chooserView(data, '', go);
  assert.equal(view.searching, false);
  assert.deepEqual(names(view.recent), ['Config parser', 'Marketing', 'Q3 report']);
  assert.deepEqual(view.recent.map(row => row.meta), ['2m ago', 'yesterday', '3d ago']);
  assert.deepEqual(view.recent.map(row => row.context), ['codeaf', 'codeaf', 'Reports']);
});

test('the All tree opens the top level by default and keys expansion by path', () => {
  const view = chooserView(data, '', go);
  assert.deepEqual(view.tree.map(row => `${row.depth}:${row.place.name}`),
    ['0:codeaf', '1:Software', '1:Marketing', '1:Config parser', '0:Reports', '1:Q3 report', '1:Q2 report', '0:Personal', '1:Marketing']);
  const software = view.tree.find(row => row.place.id === 'pl_software')!;
  assert.equal(software.hasChildren, true);
  assert.equal(software.expanded, false);
  assert.equal(view.tree.find(row => row.place.id === 'pl_config')!.hasChildren, false);
  assert.equal(view.tree[0].meta, '4 inside');
  assert.equal(view.tree[0].context, '');

  const opened = chooserView(data, '', go, new Map([['pl_codeaf/pl_software', true], ['pl_reports', false]]));
  assert.deepEqual(names(opened.tree), ['codeaf', 'Software', 'Lexer', 'Marketing', 'Config parser', 'Reports', 'Personal', 'Marketing']);
  assert.equal(opened.tree.find(row => row.place.id === 'pl_lexer')!.depth, 2);
  // The two Marketing rows are two keys, so a list can tell them apart.
  const keys = opened.tree.map(row => row.key);
  assert.equal(new Set(keys).size, keys.length);
  assert.deepEqual(opened.rows.slice(0, 3).map(row => row.section), ['recent', 'recent', 'recent']);
});

test('search is flat, case-insensitive, matches ancestors, and puts name prefixes first', () => {
  const view = chooserView(data, '  REP ', go);
  assert.equal(view.searching, true);
  assert.deepEqual(view.recent, []);
  assert.deepEqual(view.tree, []);
  assert.deepEqual(names(view.results), ['Reports', 'Q3 report', 'Q2 report']);
  assert.equal(view.results[1].context, 'Reports');

  // "soft" names Software, and Lexer only through its parent.
  assert.deepEqual(names(chooserView(data, 'soft', go).results), ['Software', 'Lexer']);
  // A place under two parents is one result.
  assert.deepEqual(names(chooserView(data, 'market', go).results), ['Marketing']);
  assert.deepEqual(chooserView(data, 'nothing like it', go).results, []);
});

test('merge refuses the place and everything under it', () => {
  const mode = { kind: 'merge', placeId: 'pl_codeaf', placeName: 'codeaf' } as const;
  assert.deepEqual([...excludedIds(mode, data)].sort(), ['pl_codeaf', 'pl_config', 'pl_lexer', 'pl_marketing', 'pl_software']);
  const view = chooserView(data, '', mode);
  assert.deepEqual(names(view.recent), ['Q3 report', 'Q2 report']);
  assert.deepEqual(names(view.tree), ['Reports', 'Q3 report', 'Q2 report', 'Personal']);
  assert.equal(view.tree.find(row => row.place.id === 'pl_personal')!.hasChildren, false);
});

test('parent refuses the place, its descendants and its current parents', () => {
  const mode = { kind: 'parent', placeId: 'pl_software', placeName: 'Software' } as const;
  assert.deepEqual([...excludedIds(mode, data)].sort(), ['pl_codeaf', 'pl_lexer', 'pl_software']);
  // codeaf stays in the tree, disabled, because Marketing and Config parser under it are still offered.
  const view = chooserView(data, '', mode);
  const codeaf = view.tree.find(row => row.place.id === 'pl_codeaf')!;
  assert.equal(codeaf.disabled, true);
  assert.deepEqual(names(view.tree).slice(0, 3), ['codeaf', 'Marketing', 'Config parser']);
  assert.deepEqual(names(chooserView(data, 'codeaf', mode).results), ['Marketing', 'Config parser']);
  // The parents field counts even when childrenOf has not caught up.
  const marketing = { kind: 'parent', placeId: 'pl_marketing', placeName: 'Marketing' } as const;
  assert.ok(excludedIds(marketing, { places, childrenOf: new Map() }).has('pl_personal'));
});

test('file refuses only the places the chats are already in', () => {
  const mode = { kind: 'file', chatIds: ['c1'], chatTitle: 'Launch plan', exclude: ['pl_reports'] } as const;
  const view = chooserView(data, '', mode);
  assert.equal(view.tree.find(row => row.place.id === 'pl_reports')!.disabled, true);
  assert.equal(view.tree.find(row => row.place.id === 'pl_q3')!.disabled, false);
  assert.ok(!names(chooserView(data, 'reports', mode).results).includes('Reports'));
});

test('arrow steps skip refused rows that cannot open, and the first choice is never a refused one', () => {
  const mode = { kind: 'parent', placeId: 'pl_software', placeName: 'Software' } as const;
  const { rows } = chooserView(data, '', { kind: 'merge', placeId: 'pl_reports', placeName: 'Reports' });
  assert.equal(step(rows, undefined, 1), rows[0].key);
  assert.equal(step(rows, undefined, -1), rows[rows.length - 1].key);
  assert.equal(step(rows, rows[rows.length - 1].key, 1), rows[rows.length - 1].key);
  const tree = chooserView(data, '', mode).tree;
  assert.equal(firstChoosable(tree), 'all:pl_codeaf/pl_marketing');
  assert.equal(parentRowKey(tree.find(row => row.key === 'all:pl_codeaf/pl_marketing')!), 'all:pl_codeaf');
  assert.equal(parentRowKey(tree[0]), undefined);
});

test('relativeWhen follows the calendar and draws nothing for an unknown time', () => {
  assert.equal(relativeWhen(undefined, now), '');
  assert.equal(relativeWhen('not a time', now), '');
  assert.equal(relativeWhen(ago(0), now), 'just now');
  assert.equal(relativeWhen(ago(2), now), '2m ago');
  assert.equal(relativeWhen(ago(90), now), '1h ago');
  assert.equal(relativeWhen(new Date(2026, 9, 8, 23, 0).toISOString(), now), 'yesterday');
  assert.equal(relativeWhen(new Date(2026, 9, 6, 12, 0).toISOString(), now), '3d ago');
  assert.equal(relativeWhen(new Date(2026, 8, 14, 12, 0).toISOString(), now), 'Sep 14');
});

test('titles and the count speak the mode', () => {
  assert.equal(chooserTitle(go), 'Go to a place, or create one');
  assert.equal(chooserTitle({ kind: 'merge', placeId: 'x', placeName: 'Q2 report' }), 'Merge “Q2 report” into…');
  assert.equal(chooserTitle({ kind: 'parent', placeId: 'x', placeName: 'Q2 report' }), 'Add “Q2 report” to another place…');
  assert.equal(chooserTitle({ kind: 'file', chatIds: [], chatTitle: 'Launch plan', exclude: [] }), 'Add “Launch plan” to a place…');
  assert.equal(countLabel(58), '58 places');
  assert.equal(countLabel(1), '1 place');
  assert.equal(countLabel(0), '');
});
