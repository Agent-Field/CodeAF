import test from 'node:test';
import assert from 'node:assert/strict';
import { suggestGroup, suggestionSetKey, workspaceFolder } from './suggest.ts';
import type { Tab, TabGroup } from './types.ts';

const tab = (id: string, chatId: string, over: Partial<Tab> = {}): Tab => ({
  id, kind: 'conversation', title: id, draft: '', pinned: false,
  sessionFile: `/tmp/chats/${chatId}/transcript.jsonl`,
  ...over,
});
const group = (id: string): TabGroup => ({ id, title: 'Existing', collapsed: false });
const rows = (workspaces: Record<string, string | undefined>) =>
  Object.fromEntries(Object.entries(workspaces).map(([id, workspace]) => [id, { workspace }]));

const bench = rows({ a: '/work/bench', b: '/work/bench', c: '/work/bench', d: '/work/bench' });

test('two tabs on one workspace root are no suggestion', () => {
  assert.equal(suggestGroup([tab('a', 'a'), tab('b', 'b')], [], bench, new Set()), undefined);
});

test('three tabs on one workspace root are one suggestion named by the folder', () => {
  const tabs = [
    tab('a', 'a', { title: 'Lexer notes' }),
    tab('b', 'b', { title: 'Something else' }),
    tab('c', 'c', { title: 'Benchmarks' }),
  ];
  assert.deepEqual(suggestGroup(tabs, [], bench, new Set()), { ids: ['a', 'b', 'c'], title: 'bench' });
});

test('the same id set after dismissal is no suggestion', () => {
  const tabs = [tab('a', 'a'), tab('b', 'b'), tab('c', 'c')];
  const offer = suggestGroup(tabs, [], bench, new Set());
  assert.ok(offer);
  assert.equal(suggestGroup(tabs, [], bench, new Set([suggestionSetKey(offer.ids)])), undefined);
  const reordered = [tabs[2], tabs[0], tabs[1]];
  assert.equal(suggestGroup(reordered, [], bench, new Set([suggestionSetKey(offer.ids)])), undefined);
});

test('tabs on mixed workspace roots are no suggestion', () => {
  const mixed = [tab('a', 'a'), tab('b', 'b'), tab('c', 'c')];
  assert.equal(suggestGroup(mixed, [], rows({ a: '/work/bench', b: '/work/other', c: '/work/third' }), new Set()), undefined);
  assert.equal(suggestGroup(mixed, [], rows({ a: '/work/bench', b: '/work/bench', c: '/work/other' }), new Set()), undefined);
});

test('a grouped member is excluded, and similar titles do not make a set', () => {
  const three = [tab('a', 'a'), tab('b', 'b', { groupId: 'g' }), tab('c', 'c')];
  assert.equal(suggestGroup(three, [group('g')], bench, new Set()), undefined);
  const four = [tab('a', 'a'), tab('b', 'b', { groupId: 'g' }), tab('c', 'c'), tab('d', 'd')];
  assert.deepEqual(suggestGroup(four, [group('g')], bench, new Set()), { ids: ['a', 'c', 'd'], title: 'bench' });
  const alike = [
    tab('a', 'a', { title: 'Benchmarks' }),
    tab('b', 'b', { title: 'Benchmarks' }),
    tab('c', 'c', { title: 'Benchmarks' }),
  ];
  assert.equal(suggestGroup(alike, [], rows({ a: '/work/one', b: '/work/two', c: '/work/three' }), new Set()), undefined);
});

test('a pinned tab is left out, a missing workspace is left out, and a trailing slash is the same root', () => {
  assert.equal(suggestGroup([tab('a', 'a'), tab('b', 'b'), tab('c', 'c', { pinned: true })], [], bench, new Set()), undefined);
  assert.equal(suggestGroup([tab('a', 'a'), tab('b', 'b'), tab('c', 'c')], [], rows({ a: '/work/bench', b: '/work/bench' }), new Set()), undefined);
  assert.equal(suggestGroup([tab('a', 'a'), tab('b', 'b'), tab('c', 'c')], [], rows({ a: '/work/bench', b: '/work/bench', c: '  ' }), new Set()), undefined);
  const slashed = rows({ a: '/work/bench/', b: '/work/bench', c: '/work/bench/' });
  assert.deepEqual(suggestGroup([tab('a', 'a'), tab('b', 'b'), tab('c', 'c')], [], slashed, new Set())?.title, 'bench');
  assert.equal(workspaceFolder('C:\\work\\bench\\')?.title, 'bench');
  assert.equal(workspaceFolder('/'), undefined);
  assert.equal(workspaceFolder('.'), undefined);
});

test('a dismissed set leaves the next workspace free to be suggested', () => {
  const tabs = [tab('a', 'a'), tab('b', 'b'), tab('c', 'c'), tab('d', 'd'), tab('e', 'e'), tab('f', 'f')];
  const roots = rows({ a: '/work/bench', b: '/work/bench', c: '/work/bench', d: '/work/other', e: '/work/other', f: '/work/other' });
  const first = suggestGroup(tabs, [], roots, new Set());
  assert.deepEqual(first, { ids: ['a', 'b', 'c'], title: 'bench' });
  assert.deepEqual(suggestGroup(tabs, [], roots, new Set([suggestionSetKey(first!.ids)])), { ids: ['d', 'e', 'f'], title: 'other' });
});

test('rows may be a map keyed by chat id, and a stale group id is not a group', () => {
  const tabs = [tab('a', 'a', { groupId: 'gone' }), tab('b', 'b'), tab('c', 'c')];
  const map = new Map([['a', { workspace: '/work/bench' }], ['b', { workspace: '/work/bench' }], ['c', { workspace: '/work/bench' }]]);
  assert.deepEqual(suggestGroup(tabs, [], map, new Set()), { ids: ['a', 'b', 'c'], title: 'bench' });
});
