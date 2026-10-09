import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../model.ts';

let counter = 0;
setIdSource(() => `copy${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);
const ids = (s: WorkspaceState) => s.tabs.map(t => t.id);

test('close-others keeps the tab and the pinned tabs, and every closed tab can be reopened', () => {
  const s = run(state([tab('p', { pinned: true }), tab('a'), tab('b'), tab('c')], { activeId: 'a' }), { type: 'close-others', id: 'b' });
  assert.deepEqual(ids(s), ['p', 'b']);
  assert.equal(s.activeId, 'b');
  assert.deepEqual(s.closed.map(t => t.id).sort(), ['a', 'c']);
});

test('close-right closes the unpinned tabs after this one in reading order', () => {
  const s = run(state([tab('a'), tab('b'), tab('c')]), { type: 'close-right', id: 'a' });
  assert.deepEqual(ids(s), ['a']);
  assert.deepEqual(run(state([tab('a'), tab('b')]), { type: 'close-right', id: 'b' }).tabs.length, 2);
});

test('the Inbox is never closed by a bulk close and cannot be duplicated', () => {
  const base = state([tab('inbox', { kind: 'inbox', pinned: true }), tab('a'), tab('b')], { activeId: 'a' });
  assert.deepEqual(ids(run(base, { type: 'close-others', id: 'a' })), ['inbox', 'a']);
  assert.deepEqual(ids(run(base, { type: 'duplicate', id: 'inbox' })), ['inbox', 'a', 'b']);
});

test('close-group closes every member and the group goes with them', () => {
  const s = run(state([tab('a'), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: false }] }), { type: 'close-group', id: 'g' });
  assert.deepEqual(ids(s), ['a']);
  assert.deepEqual(s.groups, []);
});

test('reopen-id puts the chosen tab back where it was, in its group, and makes it active', () => {
  const group = { id: 'g', title: 'G', collapsed: false };
  const closed = run(state([tab('a'), tab('b', { groupId: 'g' }), tab('c')], { groups: [group], activeId: 'b' }), { type: 'close', id: 'b' });
  assert.deepEqual(closed.groups, []);
  const back = run(closed, { type: 'reopen-id', id: 'b', before: 'c', after: 'a', group });
  assert.deepEqual(ids(back), ['a', 'b', 'c']);
  assert.equal(back.activeId, 'b');
  assert.equal(back.tabs[1].groupId, 'g');
  assert.deepEqual(back.groups.map(g => g.id), ['g']);
  assert.deepEqual(back.closed, []);
});

test('reopen-id follows its neighbours when the strip changed, and falls back to the tab before it', () => {
  const s = state([tab('inbox', { kind: 'inbox', pinned: true }), tab('a'), tab('c')], { closed: [tab('b')] });
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b', before: 'c', after: 'a' })), ['inbox', 'a', 'b', 'c']);
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b', before: 'gone', after: 'a' })), ['inbox', 'a', 'b', 'c']);
});

test('reopen-id of a tab that is not closed changes nothing, and a missing index appends', () => {
  const s = state([tab('a')], { closed: [tab('z')] });
  assert.equal(run(s, { type: 'reopen-id', id: 'nope' }), s);
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'z' })), ['a', 'z']);
});

test('duplicate adds a new active copy right after the tab with the same session and a fresh id', () => {
  const s = run(state([tab('a', { sessionFile: '/s/a.jsonl', draft: 'hi' }), tab('b')]), { type: 'duplicate', id: 'a' });
  assert.equal(s.tabs.length, 3);
  assert.equal(s.tabs[1].sessionFile, '/s/a.jsonl');
  assert.equal(s.tabs[1].draft, 'hi');
  assert.notEqual(s.tabs[1].id, 'a');
  assert.equal(s.activeId, s.tabs[1].id);
});

test('duplicate of a split gives every pane a new id', () => {
  const pane = (id: string) => ({ id, kind: 'conversation' as const, title: id, draft: '' });
  const s = run(state([tab('s', { split: { layout: '1x2', focus: 0, panes: [pane('p1'), pane('p2')] } })]), { type: 'duplicate', id: 's' });
  const copy = s.tabs[1].split!;
  assert.equal(copy.panes.length, 2);
  assert.ok(copy.panes.every(p => p.id !== 'p1' && p.id !== 'p2'));
});

test('ensure-inbox adds one pinned Inbox first in the strip, once, without taking focus', () => {
  const once = run(state([tab('a'), tab('b')], { activeId: 'b' }), { type: 'ensure-inbox' });
  assert.deepEqual(ids(once), ['inbox', 'a', 'b']);
  assert.equal(once.tabs[0].pinned, true);
  assert.equal(once.tabs[0].kind, 'inbox');
  assert.equal(once.activeId, 'b');
  assert.equal(run(once, { type: 'ensure-inbox' }), once);
});

test('open-inbox creates the Inbox when absent and focuses it; with it present it only focuses', () => {
  const opened = run(state([tab('a'), tab('b')], { activeId: 'b' }), { type: 'open-inbox' });
  assert.deepEqual(ids(opened), ['inbox', 'a', 'b']);
  assert.equal(opened.activeId, 'inbox');
  const again = run({ ...opened, activeId: 'a' }, { type: 'open-inbox' });
  assert.deepEqual(ids(again), ['inbox', 'a', 'b']);
  assert.equal(again.activeId, 'inbox');
});
