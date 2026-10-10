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

test('closing stamps when the tab entered the closed list, and reopening drops that time', () => {
  const before = Date.now();
  const closed = run(state([tab('a'), tab('b')]), { type: 'close', id: 'b' });
  const at = closed.closed.find(t => t.id === 'b')!.closedAt!;
  assert.ok(Number.isSafeInteger(at) && at >= before && at <= Date.now());
  const back = run(closed, { type: 'reopen' });
  assert.equal('closedAt' in back.tabs.find(t => t.id === 'b')!, false);
  const again = run(back, { type: 'close', id: 'b' });
  assert.ok(again.closed.find(t => t.id === 'b')!.closedAt! >= at);
});

test('a bulk close and a pane closed out of a split each carry a close time', () => {
  const bulk = run(state([tab('a'), tab('b'), tab('c')], { activeId: 'b' }), { type: 'close-others', id: 'b' });
  assert.ok(bulk.closed.every(t => Number.isSafeInteger(t.closedAt)));
  const pane = (id: string) => ({ id, kind: 'conversation' as const, title: id, draft: '' });
  const host = tab('s', { split: { layout: '1x2', focus: 0, panes: [pane('p1'), pane('p2')] } });
  const split = run(state([host, tab('z')]), { type: 'split-close-pane', id: 's', paneId: 'p2' });
  assert.equal(split.closed[0].id, 'p2');
  assert.ok(Number.isSafeInteger(split.closed[0].closedAt));
});

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

test('bulk close preserves pinned tabs and duplicating an absent tab does nothing', () => {
  const base = state([tab('pinned', { pinned: true }), tab('a'), tab('b')], { activeId: 'a' });
  assert.deepEqual(ids(run(base, { type: 'close-others', id: 'a' })), ['pinned', 'a']);
  assert.deepEqual(ids(run(base, { type: 'duplicate', id: 'missing' })), ['pinned', 'a', 'b']);
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
  const s = state([tab('pinned', { pinned: true }), tab('a'), tab('c')], { closed: [tab('b')] });
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b', before: 'c', after: 'a' })), ['pinned', 'a', 'b', 'c']);
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b', before: 'gone', after: 'a' })), ['pinned', 'a', 'b', 'c']);
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

test('retired Inbox actions cannot create a pinned slot or change focus', () => {
  const before = state([tab('a'), tab('b')], { activeId: 'b' });
  assert.equal(run(before, { type: 'ensure-inbox' }), before);
  assert.equal(run(before, { type: 'open-inbox' }), before);
});

test('release-moved removes the tab without a closed record, and leaves split tabs and unknown ids alone', () => {
  const s = state([tab('a'), tab('b')], { activeId: 'a' });
  const moved = run(s, { type: 'release-moved', id: 'a' });
  assert.deepEqual(moved.tabs.map(t => t.id), ['b']);
  assert.equal(moved.closed.some(t => t.id === 'a'), false);
  assert.equal(moved.activeId, 'b');
  assert.equal(run(s, { type: 'release-moved', id: 'zzz' }), s);
});

test('restore-closed brings back a bulk close with its group metadata, split panes and place, even past the closed cap', () => {
  const pane = (id: string) => ({ id, kind: 'conversation' as const, title: id, draft: `draft ${id}` });
  const split: Tab = { ...tab('s', { groupId: 'g' }), split: { layout: '1x2', focus: 1, panes: [pane('s1'), pane('s2')] } };
  const s = state([tab('p', { pinned: true }), tab('a'), tab('b', { groupId: 'g' }), split, tab('c')], { groups: [{ id: 'g', title: 'Trailing commas', collapsed: true }], activeId: 's' });
  const order = s.tabs.map(t => t.id);
  const closing = run(s, { type: 'close-group', id: 'g' });
  assert.deepEqual(ids(closing), ['p', 'a', 'c']);
  assert.equal(closing.groups.length, 0);
  const back = run(closing, { type: 'restore-closed', tabs: [s.tabs[2], s.tabs[3]], order, groups: s.groups, activeId: s.activeId });
  assert.deepEqual(ids(back), order);
  assert.deepEqual(back.groups, [{ id: 'g', title: 'Trailing commas', collapsed: true }]);
  assert.equal(back.tabs.find(t => t.id === 's')!.split!.panes.length, 2);
  assert.equal(back.tabs.find(t => t.id === 's')!.split!.panes[1].draft, 'draft s2');
  assert.equal(back.activeId, 's');
  assert.deepEqual(back.closed, []);
});

test('restore-closed leaves tabs that exist again alone and keeps tabs opened since in place', () => {
  const s = state([tab('a'), tab('b'), tab('c')], { activeId: 'a' });
  const order = ['a', 'b', 'c'];
  const closing = run(s, { type: 'close-others', id: 'a' }, { type: 'new' });
  const fresh = closing.tabs.find(t => !order.includes(t.id))!;
  const back = run(closing, { type: 'restore-closed', tabs: [s.tabs[1], s.tabs[2]], order, groups: [], activeId: 'a' });
  assert.deepEqual(ids(back), ['a', 'b', 'c', fresh.id]);
  assert.equal(back.activeId, closing.activeId);
  assert.equal(run(back, { type: 'restore-closed', tabs: [s.tabs[1]], order, groups: [], activeId: 'a' }), back);
});
