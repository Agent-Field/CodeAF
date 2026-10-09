import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../model.ts';

let counter = 0;
setIdSource(() => `s${++counter}`);
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[]): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id) });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);

test('split-merge at start puts the dropped tab first and focuses it; at end puts it last', () => {
  const start = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b', layout: '1x2', at: 'start' });
  assert.deepEqual(start.tabs[0].split!.panes.map(p => p.id), ['b', 'a']);
  assert.equal(start.tabs[0].split!.focus, 0);
  const end = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b', layout: '2x1' });
  assert.deepEqual(end.tabs[0].split!.panes.map(p => p.id), ['a', 'b']);
  assert.equal(end.tabs[0].split!.focus, 1);
  assert.equal(end.tabs[0].split!.layout, '2x1');
});

test('a third and fourth pane make a 2x2; a fifth is refused', () => {
  let s = run(state([tab('a'), tab('b'), tab('c'), tab('d'), tab('e')]), { type: 'split-merge', id: 'a', withId: 'b' });
  s = run(s, { type: 'split-merge', id: s.activeId, withId: 'c' });
  assert.equal(s.tabs[0].split!.layout, '2x2');
  s = run(s, { type: 'split-merge', id: s.activeId, withId: 'd' });
  assert.equal(s.tabs[0].split!.panes.length, 4);
  assert.equal(run(s, { type: 'split-merge', id: s.activeId, withId: 'e' }), s);
});

test('split-resize clamps and persists in the model', () => {
  const s = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b', layout: '1x2' });
  const id = s.activeId;
  const wide = run(s, { type: 'split-resize', id, col: 0.7 });
  assert.deepEqual(wide.tabs[0].split!.ratios, { col: 0.7, row: 0.5 });
  const clamped = run(wide, { type: 'split-resize', id, col: 0.99, row: 0 });
  assert.deepEqual(clamped.tabs[0].split!.ratios, { col: 0.8, row: 0.2 });
  assert.equal(run(clamped, { type: 'split-resize', id, col: 0.99 }), clamped);
  assert.equal(run(state([tab('a')]), { type: 'split-resize', id: 'a', col: 0.3 }).tabs[0].split, undefined);
});

test('reorder after puts the tab behind its target', () => {
  const s = run(state([tab('a'), tab('b'), tab('c')]), { type: 'reorder', id: 'a', targetId: 'b', after: true });
  assert.deepEqual(s.tabs.map(t => t.id), ['b', 'a', 'c']);
});

test('split-swap trades two panes, keeps the focused pane focused, and refuses unknown or identical panes', () => {
  const merged = run(state([tab('a'), tab('b'), tab('c')]), { type: 'split-merge', id: 'a', withId: 'b', layout: '1x2' }, { type: 'split-resize', id: 's1', col: 0.7 });
  const id = merged.activeId;
  assert.equal(merged.tabs[0].split!.focus, 1);
  const swapped = run(merged, { type: 'split-swap', id, paneId: 'a', withPaneId: 'b' });
  assert.deepEqual(swapped.tabs[0].split!.panes.map(p => p.id), ['b', 'a']);
  assert.equal(swapped.tabs[0].split!.focus, 0);
  assert.equal(swapped.tabs[0].split!.panes[swapped.tabs[0].split!.focus].id, 'b');
  assert.deepEqual(swapped.tabs[0].split!.ratios, merged.tabs[0].split!.ratios);
  assert.equal(run(merged, { type: 'split-swap', id, paneId: 'a', withPaneId: 'a' }), merged);
  assert.equal(run(merged, { type: 'split-swap', id, paneId: 'a', withPaneId: 'zzz' }), merged);
});
