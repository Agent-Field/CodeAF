import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mergeWindowLocal, type SharedWorkspace, type WindowLocal } from './windowLocal.ts';
import type { Tab } from './types.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const shared = (...ids: string[]): SharedWorkspace => ({ tabs: ids.map(id => tab(id)), groups: [], closed: [], nextNumber: 9 });
const local = (activeId: string, over: Partial<WindowLocal> = {}): WindowLocal => ({
  activeId, recentIds: [activeId], selection: [], overviewOpen: false, undo: [], railCollapsed: false, focusMode: false, ...over,
});
const was = ['a', 'b', 'c', 'd'];

test('a remote close of the local active tab picks its right neighbour', () => {
  const merged = mergeWindowLocal(shared('a', 'c', 'd'), local('b'), was);
  assert.equal(merged.activeId, 'c');
});

test('with no right neighbour left the left one is taken, and with no order the first tab', () => {
  assert.equal(mergeWindowLocal(shared('a', 'b'), local('d'), was).activeId, 'b');
  assert.equal(mergeWindowLocal(shared('x', 'y'), local('b')).activeId, 'x');
});

test('an active tab that survived stays put', () => {
  assert.equal(mergeWindowLocal(shared('a', 'b', 'd'), local('b'), was).activeId, 'b');
});

test('selection and recents drop vanished ids, and panes of splits still count', () => {
  const split = { ...tab('s'), split: { layout: '1x2' as const, focus: 0, panes: [tab('p1'), tab('p2')] } };
  const s: SharedWorkspace = { ...shared('a'), tabs: [tab('a'), split] };
  const merged = mergeWindowLocal(s, local('a', { selection: ['a', 'gone', 'p2'], recentIds: ['a', 'gone', 's'] }), was);
  assert.deepEqual(merged.selection, ['a', 'p2']);
  assert.deepEqual(merged.recentIds, ['a', 's']);
});

test('two locals on one shared state stay independent and nothing is mutated', () => {
  const s = shared('a', 'c');
  const one = local('b', { selection: ['b', 'c'], focusMode: true });
  const two = local('c', { overviewOpen: true });
  const frozen = JSON.stringify([s, one, two]);
  const m1 = mergeWindowLocal(s, one, was);
  const m2 = mergeWindowLocal(s, two, was);
  assert.equal(m1.activeId, 'c');
  assert.equal(m2.activeId, 'c');
  assert.deepEqual([m1.focusMode, m1.overviewOpen, m1.selection], [true, false, ['c']]);
  assert.deepEqual([m2.focusMode, m2.overviewOpen, m2.selection], [false, true, []]);
  assert.equal(JSON.stringify([s, one, two]), frozen);
});
