import test from 'node:test';
import assert from 'node:assert/strict';
import { blockNeighbour } from './groups.ts';
import { workspaceReducer, type Tab, type TabGroup, type WorkspaceState } from '../model.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const group = (id: string): TabGroup => ({ id, title: id, collapsed: false });
const ids = (s: WorkspaceState) => s.tabs.map(t => t.id);
// P pinned, then a, group G (g1 g2 g3), b, group H (h1 h2).
const base = (): WorkspaceState => ({
  tabs: [tab('P', { pinned: true }), tab('a'), tab('g1', { groupId: 'G' }), tab('g2', { groupId: 'G' }), tab('g3', { groupId: 'G' }), tab('b'), tab('h1', { groupId: 'H' }), tab('h2', { groupId: 'H' })],
  groups: [group('G'), group('H')], activeId: 'a', closed: [], nextNumber: 9, recentIds: [],
});
const move = (s: WorkspaceState, id: string, targetId: string, after?: boolean) => workspaceReducer(s, { type: 'move-group-block', id, targetId, after });

test('the block keeps its member order when dropped after a tab', () => {
  assert.deepEqual(ids(move(base(), 'G', 'b', true)), ['P', 'a', 'b', 'g1', 'g2', 'g3', 'h1', 'h2']);
});

test('dropping before another group moves the block ahead of that whole group', () => {
  assert.deepEqual(ids(move(base(), 'H', 'G')), ['P', 'a', 'h1', 'h2', 'g1', 'g2', 'g3', 'b']);
});

test('dropping on a member of another group lands beside the group, never inside it', () => {
  assert.deepEqual(ids(move(base(), 'G', 'h1', true)), ['P', 'a', 'b', 'h1', 'h2', 'g1', 'g2', 'g3']);
});

test('pinned tabs never move: dropping before a pin lands after the pins, and no pin joins the group', () => {
  const s = move(base(), 'G', 'P');
  assert.deepEqual(ids(s), ['P', 'g1', 'g2', 'g3', 'a', 'b', 'h1', 'h2']);
  assert.equal(s.tabs[0].pinned, true);
  assert.equal(s.tabs[0].groupId, undefined);
});

test('moving a block to its own position is a no-op', () => {
  const s = base();
  assert.equal(move(s, 'G', 'a', true), s);
  assert.equal(move(s, 'G', 'b'), s);
  assert.equal(move(s, 'G', 'g2'), s);
  assert.equal(move(s, 'G', 'G'), s);
});

test('Alt+Shift neighbours are the adjacent loose tab or whole group', () => {
  const s = base();
  assert.equal(blockNeighbour(s.tabs, 'G', -1), 'a');
  assert.equal(blockNeighbour(s.tabs, 'G', 1), 'b');
  assert.equal(blockNeighbour(s.tabs, 'H', 1), undefined);
  assert.equal(blockNeighbour(s.tabs, 'H', -1), 'b');
  assert.deepEqual(ids(move(s, 'G', blockNeighbour(s.tabs, 'G', 1)!, true)), ['P', 'a', 'b', 'g1', 'g2', 'g3', 'h1', 'h2']);
});
