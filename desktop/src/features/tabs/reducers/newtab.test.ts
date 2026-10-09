import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../model.ts';

let counter = 0;
setIdSource(() => `n${++counter}`);
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);

test('a new tab is a field titled New tab, not a conversation', () => {
  const s = run(state([tab('a')]), { type: 'new' });
  assert.equal(s.tabs[1].kind, 'newtab');
  assert.equal(s.tabs[1].title, 'New tab');
});

test('the field becomes a conversation that carries its session and keeps the typed words as the title', () => {
  const s = run(state([tab('f', { kind: 'newtab', title: 'New tab' })]), { type: 'newtab-become', id: 'f', kind: 'conversation', title: 'Fix the lexer', titleSource: 'message', sessionFile: '/s/1.jsonl' });
  assert.deepEqual([s.tabs[0].kind, s.tabs[0].title, s.tabs[0].sessionFile, s.tabs[0].draft, s.tabs[0].id], ['conversation', 'Fix the lexer', '/s/1.jsonl', '', 'f']);
});

test('a failed start keeps the words as the draft', () => {
  const s = run(state([tab('f', { kind: 'newtab' })]), { type: 'newtab-become', id: 'f', kind: 'conversation', title: 'Hi', draft: 'Hi there' });
  assert.equal(s.tabs[0].draft, 'Hi there');
});

test('the field becomes a file tab with its path', () => {
  const s = run(state([tab('f', { kind: 'newtab' })]), { type: 'newtab-become', id: 'f', kind: 'file', title: 'lexer.go', path: 'internal/parse/lexer.go', sessionFile: '/s/1.jsonl' });
  assert.deepEqual([s.tabs[0].kind, s.tabs[0].path, s.tabs[0].sessionFile], ['file', 'internal/parse/lexer.go', '/s/1.jsonl']);
});

test('only a field can become something else', () => {
  const base = state([tab('a')]);
  assert.equal(run(base, { type: 'newtab-become', id: 'a', kind: 'file', title: 'x' }).tabs[0].kind, 'conversation');
});

test('reopening from the field brings the closed tab back under its own id, the field goes, and the closed list drops it', () => {
  const closed = tab('c', { title: 'Fix it in the lexer', sessionFile: '/s/9.jsonl', draft: 'half' });
  const s = run(state([tab('f', { kind: 'newtab', title: 'New tab' })], { closed: [closed] }), { type: 'newtab-reopen', id: 'f', closedId: 'c' });
  assert.deepEqual([s.tabs.length, s.tabs[0].id, s.tabs[0].kind, s.tabs[0].title, s.tabs[0].sessionFile, s.tabs[0].draft, s.closed.length, s.activeId], [1, 'c', 'conversation', 'Fix it in the lexer', '/s/9.jsonl', 'half', 0, 'c']);
  assert.ok(!s.recentIds.includes('f'));
});

test('reopening an unknown closed tab changes nothing', () => {
  const base = state([tab('f', { kind: 'newtab' })]);
  assert.equal(run(base, { type: 'newtab-reopen', id: 'f', closedId: 'zzz' }), base);
});

test('the field works inside a split', () => {
  const a = tab('a');
  const f = tab('f', { kind: 'newtab', title: 'New tab' });
  let s = run(state([a, f]), { type: 'split-merge', id: 'a', withId: 'f' });
  s = run(s, { type: 'newtab-become', id: 'f', kind: 'conversation', title: 'Q', sessionFile: '/s/2.jsonl' });
  assert.equal(s.tabs.length, 1);
  assert.equal(s.tabs.flatMap(t => t.split?.panes ?? [t]).find(p => p.id === 'f')?.kind, 'conversation');
});
