// withUndo: structural tab actions leave a shared snapshot, 20 per window, and undo puts the strip back.
import test from 'node:test';
import assert from 'node:assert/strict';
import { workspaceReducer, type Tab, type TabGroup, type WorkspaceAction, type WorkspaceState } from '../model.ts';
import { undoAction, undoStackLimit, windowCloseAction, withUndo } from './undo.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const group = (id: string, title = id): TabGroup => ({ id, title, collapsed: false });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({
  tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id), ...over,
});
const reduce = withUndo(workspaceReducer);
const place = (s: WorkspaceState) => s.tabs.map(item => `${item.id}${item.groupId ? `@${item.groupId}` : ''}${item.pinned ? '*' : ''}`).join(' ');

test('21 closes keep 20', () => {
  const tabs = Array.from({ length: 22 }, (_, index) => tab(`t${index}`));
  let current = reduce(state(tabs), { type: 'select', id: 't0' });
  for (let index = 0; index < undoStackLimit + 1; index++) current = reduce(current, { type: 'close', id: `t${index}` });
  assert.equal(current.undo?.length, undoStackLimit);
  assert.equal(current.tabs.some(item => item.id === 't0'), false);
  assert.equal(current.tabs.some(item => item.id === 't20'), false);
  for (let step = 0; step < undoStackLimit; step++) current = reduce(current, undoAction);
  assert.equal(current.undo?.length, 0);
  assert.equal(current.tabs.some(item => item.id === 't0'), false);
  assert.equal(current.tabs.some(item => item.id === 't1'), true);
});

test('undo restores tab position and group', () => {
  const bench = group('g', 'Bench');
  const start = state([tab('a'), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('d')], { groups: [bench], activeId: 'a' });
  const closed = reduce(start, { type: 'close', id: 'c' });
  assert.equal(place(closed), 'a b@g d');
  const back = reduce(closed, undoAction);
  assert.equal(place(back), 'a b@g c@g d');
  assert.equal(back.groups.find(item => item.id === 'g')?.title, 'Bench');
  assert.equal(back.closed.some(item => item.id === 'c'), false);
});

test('draft typing is not undoable', () => {
  const start = state([tab('a'), tab('b')]);
  const typed = reduce(start, { type: 'draft', id: 'a', draft: 'hello' });
  assert.equal(typed.undo, undefined);
  assert.equal(reduce(typed, undoAction), typed);
  assert.equal(typed.tabs[0].draft, 'hello');
  const named = reduce(typed, { type: 'rename', id: 'a', title: 'Notes' });
  const viewed = reduce(named, { type: 'view', id: 'a', change: { tasksClosed: true } });
  const selected = reduce(viewed, { type: 'select', id: 'b' });
  assert.equal(selected.undo, undefined);
  const closed = reduce(selected, { type: 'close', id: 'b' });
  const later = reduce(closed, { type: 'draft', id: 'a', draft: 'hello world' });
  assert.equal(later.undo?.length, 1);
  const back = reduce(later, undoAction);
  assert.equal(place(back), 'a b');
  assert.equal(back.tabs.find(item => item.id === 'a')?.draft, 'hello world');
  assert.equal(back.tabs.find(item => item.id === 'a')?.title, 'Notes');
  assert.equal(back.tabs.find(item => item.id === 'a')?.tasksClosed, true);
  assert.equal(back.activeId, 'a');
});

test('the closing toast Undo is the same undo action', () => {
  const bench = group('g', 'Bench');
  const start = state([tab('a'), tab('b', { groupId: 'g' }), tab('c')], { groups: [bench] });
  const closed = reduce(start, { type: 'close-group', id: 'g' });
  assert.equal(place(closed), 'a c');
  assert.equal(closed.groups.some(item => item.id === 'g'), false);
  const back = reduce(closed, undoAction);
  assert.equal(place(back), 'a b@g c');
  assert.equal(back.groups[0].title, 'Bench');
});

test('a window close clears that window\'s stack and leaves the other window\'s', () => {
  const leftStart = state([tab('a'), tab('b')]);
  const rightStart = state([tab('a'), tab('b'), tab('c')]);
  const left = reduce(leftStart, { type: 'pin', id: 'b' });
  let right = reduce(rightStart, { type: 'reorder', id: 'c', targetId: 'a' });
  assert.equal(left.undo?.length, 1);
  assert.equal(right.undo?.length, 1);
  right = reduce(right, windowCloseAction);
  assert.equal(right.undo?.length, 0);
  assert.equal(place(reduce(right, undoAction)), place(right));
  assert.equal(place(reduce(left, undoAction)), 'a b');
  assert.equal(left.tabs.find(item => item.id === 'b')?.pinned, true);
});

test('titles views and selection do not push, and a no-op close does not either', () => {
  const start = state([tab('a'), tab('b', { groupId: 'g' })], { groups: [group('g')] });
  const actions: WorkspaceAction[] = [
    { type: 'draft', id: 'a', draft: 'x' },
    { type: 'title', id: 'a', title: 'From the engine', source: 'engine' },
    { type: 'view', id: 'a', change: { path: 'notes.md' } },
    { type: 'select', id: 'b' },
    { type: 'close', id: 'missing' },
  ];
  const after = actions.reduce(reduce, start);
  assert.equal(after.undo, undefined);
  assert.equal(reduce(after, undoAction), after);
});
