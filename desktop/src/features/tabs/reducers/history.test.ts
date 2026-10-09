import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../model.ts';

let counter = 0;
setIdSource(() => `h${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);

test('history-replace swaps the tab in place, keeping its pin and group, and selects the new one', () => {
  const base = state([tab('a'), tab('h', { kind: 'history', title: 'History', groupId: 'g' }), tab('b')], { activeId: 'h', groups: [{ id: 'g', title: 'G', collapsed: false }] });
  const next = run(base, { type: 'history-replace', id: 'h', tab: tab('c', { sessionFile: '/s/c/transcript.jsonl' }) });
  assert.deepEqual(next.tabs.map(t => t.id), ['a', 'c', 'b']);
  assert.equal(next.tabs[1].groupId, 'g');
  assert.equal(next.activeId, 'c');
  assert.equal(next.tabs[1].sessionFile, '/s/c/transcript.jsonl');
  assert.deepEqual(run(base, { type: 'history-replace', id: 'zzz', tab: tab('c') }), base);
});

test('history-archive removes tabs without feeding Reopen, and moves the selection off a removed tab', () => {
  const base = state([tab('a'), tab('b'), tab('c')], { activeId: 'b' });
  const next = run(base, { type: 'history-archive', ids: ['a', 'b'] });
  assert.deepEqual(next.tabs.map(t => t.id), ['c']);
  assert.equal(next.activeId, 'c');
  assert.deepEqual(next.closed, []);
  assert.deepEqual(run(base, { type: 'history-archive', ids: ['nope'] }), base);
  // The last tab is never taken: a workspace always has one.
  assert.deepEqual(run(state([tab('a')]), { type: 'history-archive', ids: ['a'] }).tabs.map(t => t.id), ['a']);
});
