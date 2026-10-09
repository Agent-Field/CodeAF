import assert from 'node:assert/strict';
import { test } from 'node:test';
import { moveCursor, overviewOrder, overviewSections, searchPlaceholder, sectionPosition } from './overview-model.ts';
import type { Tab } from './types.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const groups = [{ id: 'g', title: 'Trailing commas', collapsed: false }, { id: 'h', title: 'Empty', collapsed: false }];
const tabs = [tab('a'), tab('p', { pinned: true }), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('d')];
const hay = (t: Tab) => t.title;

test('sections run pinned, groups, other; empty sections drop', () => {
  const s = overviewSections(tabs, groups, '', hay);
  assert.deepEqual(s.map(x => [x.title, x.tabs.map(t => t.id)]), [['Pinned', ['p']], ['Trailing commas', ['b', 'c']], ['Other tabs', ['a', 'd']]]);
  assert.deepEqual(overviewOrder(s).map(t => t.id), ['p', 'b', 'c', 'a', 'd']);
});

test('the filter keeps matching tabs and drops sections left empty', () => {
  const s = overviewSections(tabs, groups, ' C ', hay);
  assert.deepEqual(s.map(x => [x.title, x.tabs.map(t => t.id)]), [['Trailing commas', ['c']]]);
  assert.deepEqual(overviewSections(tabs, groups, 'zzz', hay), []);
});

test('placeholder, cursor and position', () => {
  assert.equal(searchPlaceholder(18), 'Search 18 tabs');
  assert.equal(searchPlaceholder(1), 'Search 1 tab');
  const ids = ['a', 'b', 'c'];
  assert.equal(moveCursor(ids, 'c', 1), 'a');
  assert.equal(moveCursor(ids, 'a', -1), 'c');
  assert.equal(moveCursor(ids, 'gone', 1), 'a');
  assert.equal(moveCursor([], 'a', 1), undefined);
  const s = overviewSections(tabs, groups, '', hay);
  assert.equal(sectionPosition(s, 'c'), 'Trailing commas · 2 of 2');
  assert.equal(sectionPosition(s, 'zz'), '');
});
