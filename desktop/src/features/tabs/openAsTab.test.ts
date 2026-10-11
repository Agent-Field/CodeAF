// Opening the view a pane is drilled into hands that view over. Opening any other task does not.
import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from './helpers.ts';
import { panesOf, workspaceReducer, type Tab, type WorkspaceState } from './model.ts';
import { openAsTab, releaseRoute } from './openAsTab.ts';
import type { TabRoute } from './view-state.ts';

let minted = 0;
setIdSource(() => `handed-${++minted}`);

const route = (taskId: string, back: string[] = [''], forward: string[] = []): TabRoute => ({ taskId, back, forward });
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[]): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id) });

function taskOf(current: WorkspaceState, id: string): string | undefined {
  for (const item of current.tabs) {
    const pane = panesOf(item).find(candidate => candidate.id === id);
    if (pane) return pane.route?.taskId;
  }
  throw new Error(`no pane ${id}`);
}

test('opening the task a chat is drilled into hands the route over and pops the chat', () => {
  const chat = tab('chat', { title: 'Security review', sessionFile: 'chats/security/session.jsonl', route: route('audit', ['', 'parent'], ['later']) });
  const placed = openAsTab(chat, 'audit');
  assert.deepEqual(placed.route, { taskId: 'audit', back: ['parent'], forward: ['later'] });
  assert.deepEqual(placed.release, { taskId: 'parent', back: [''], forward: [] });
  assert.equal(releaseRoute({ back: [], forward: [] }), undefined);

  const opened = tab('task', { kind: 'task', title: 'Security audit', sessionFile: chat.sessionFile, route: route('audit') });
  const next = workspaceReducer(state([chat]), { type: 'open-task', tab: opened, background: true, from: 'chat' });
  assert.equal(next.activeId, 'chat');
  assert.equal(taskOf(next, 'chat'), 'parent');
  assert.deepEqual(panesOf(next.tabs.find(item => item.id === 'task')!)[0].route, placed.route);
});

test('a one-step drill pops the chat to the conversation, and Back on the new tab has nowhere to go', () => {
  const chat = tab('chat', { route: route('audit') });
  const opened = tab('task', { kind: 'task', title: 'Audit', route: route('audit') });
  const next = workspaceReducer(state([chat]), { type: 'open-task', tab: opened, background: true, from: 'chat' });
  assert.equal(taskOf(next, 'chat'), undefined);
  assert.equal(panesOf(next.tabs.find(item => item.id === 'chat')!)[0].route, undefined);
  const taskRoute = panesOf(next.tabs.find(item => item.id === 'task')!)[0].route;
  assert.deepEqual(taskRoute, { taskId: 'audit', back: [], forward: [] });
});

test('opening a different task leaves the source route where it was', () => {
  const chat = tab('chat', { route: route('already-open') });
  const opened = tab('task', { kind: 'task', title: 'Other', route: route('other') });
  const next = workspaceReducer(state([chat]), { type: 'open-task', tab: opened, background: true, from: 'chat' });
  assert.equal(taskOf(next, 'chat'), 'already-open');
  assert.equal(taskOf(next, 'task'), 'other');
  assert.deepEqual(panesOf(next.tabs.find(item => item.id === 'task')!)[0].route, route('other'));
});

test('a task tab drilled into a child hands the child over and stays on its own task', () => {
  const task = tab('task', { kind: 'task', title: 'Audit', route: route('child', ['', 'audit']) });
  const opened = tab('child', { kind: 'task', title: 'Child', route: route('child') });
  const next = workspaceReducer(state([task]), { type: 'open-task', tab: opened, background: true, from: 'task' });
  assert.deepEqual(panesOf(next.tabs.find(item => item.id === 'task')!)[0].route, route('audit'));
  assert.deepEqual(panesOf(next.tabs.find(item => item.id === 'child')!)[0].route, { taskId: 'child', back: ['audit'], forward: [] });
});

test('a task tab that already is that task is not popped back to the conversation', () => {
  const task = tab('task', { kind: 'task', title: 'Audit', route: route('audit') });
  const again = tab('again', { kind: 'task', title: 'Audit', route: route('audit') });
  const next = workspaceReducer(state([task]), { type: 'open-task', tab: again, background: true, from: 'task' });
  assert.equal(taskOf(next, 'task'), 'audit');
  assert.deepEqual(panesOf(next.tabs.find(item => item.id === 'again')!)[0].route, route('audit'));
});

test('dropping a drilled chat into a split pops the chat and adds the task pane', () => {
  const chat = tab('chat', { title: 'Security review', draft: 'keep', sessionFile: 'chats/security/session.jsonl', route: route('audit') });
  const host = tab('host', { kind: 'newtab', title: 'New tab' });
  const next = workspaceReducer(state([host, chat]), { type: 'split-merge', id: 'host', withId: 'chat', layout: '1x2' });
  const panes = next.tabs[0].split?.panes ?? [];
  assert.deepEqual(panes.map(pane => pane.kind), ['newtab', 'conversation', 'task']);
  assert.equal(panes[1].route, undefined);
  assert.equal(panes[1].draft, 'keep');
  assert.deepEqual(panes[2].route, { taskId: 'audit', back: [], forward: [] });
  assert.equal(panes[2].sessionFile, chat.sessionFile);
  assert.equal(panes[2].title, '');
  assert.equal(next.tabs[0].split?.layout, '2x2');
});

test('dropping a chat that is not drilled into a task does not add a pane', () => {
  const chat = tab('chat', { route: route('audit'), kind: 'task' });
  const host = tab('host');
  const next = workspaceReducer(state([host, chat]), { type: 'split-merge', id: 'host', withId: 'chat', layout: '1x2' });
  assert.deepEqual(next.tabs[0].split?.panes.map(pane => pane.id), ['host', 'chat']);
  assert.equal(next.tabs[0].split?.layout, '1x2');
});
