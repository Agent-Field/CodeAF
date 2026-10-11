// A chat tab and a task tab opened from it share a session file and nothing else about where the reader is.
// The route lives on the pane id. A view action names that id. The session file is how a draft is mirrored, not how a drill is.
import test from 'node:test';
import assert from 'node:assert/strict';
import { panesOf, workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from './model.ts';
import { goBack, type TabRoute } from './view-state.ts';
import { createWorkspaceController } from '../workspace-sync/controller.ts';
import { createManualClock, createTestEngine } from '../workspace-sync/testEngine.ts';

const session = 'chats/security/session.jsonl';
const route = (taskId: string, back: string[] = ['']): TabRoute => ({ taskId, back, forward: [] });
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[]): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id) });
const run = (current: WorkspaceState, action: WorkspaceAction) => workspaceReducer(current, action);

function taskOf(current: WorkspaceState, id: string): string | undefined {
  for (const item of current.tabs) {
    const pane = panesOf(item).find(candidate => candidate.id === id);
    if (pane) return pane.route?.taskId;
  }
  throw new Error(`no pane ${id}`);
}

test('opening a task tab leaves the source route where it was and gives the new tab its own', () => {
  const chat = tab('chat', { title: 'Security review', sessionFile: session, route: route('already-open') });
  const task = tab('task', { kind: 'task', title: 'Security audit', titleSource: 'manual', sessionFile: session, route: route('audit') });
  const next = run(state([chat]), { type: 'open-task', tab: task, background: true, from: 'chat' });
  assert.equal(next.activeId, 'chat');
  assert.equal(taskOf(next, 'chat'), 'already-open');
  assert.equal(taskOf(next, 'task'), 'audit');
  assert.notEqual(panesOf(next.tabs.find(item => item.id === 'chat')!)[0].route, panesOf(next.tabs.find(item => item.id === 'task')!)[0].route);
});

test('a view route is stored per pane id, including two panes of one session in a split', () => {
  const chat = tab('chat', { sessionFile: session });
  const task = tab('task', { kind: 'task', sessionFile: session, route: route('audit') });
  let next = run(state([chat, task]), { type: 'split-merge', id: 'chat', withId: 'task', layout: '1x2', mint: ['split'] });
  const split = next.tabs.find(item => item.split);
  assert.equal(split?.id, 'split');
  assert.deepEqual(split?.split?.panes.map(pane => pane.id), ['chat', 'task']);
  assert.equal(taskOf(next, 'chat'), undefined);
  assert.equal(taskOf(next, 'task'), 'audit');

  const child = route('child', ['audit']);
  next = run(next, { type: 'view', id: 'task', change: { route: child } });
  assert.equal(taskOf(next, 'chat'), undefined);
  assert.equal(taskOf(next, 'task'), 'child');

  const taskPane = panesOf(next.tabs.find(item => item.id === 'split')!).find(pane => pane.id === 'task')!;
  next = run(next, { type: 'view', id: 'task', change: { route: goBack(taskPane.route!) } });
  next = run(next, { type: 'view', id: 'chat', change: { route: route('local') } });
  assert.equal(taskOf(next, 'task'), 'audit');
  assert.equal(taskOf(next, 'chat'), 'local');

  // The session file is not an address. A view aimed at it changes no pane.
  const missed = run(next, { type: 'view', id: session, change: { route: undefined } });
  assert.equal(taskOf(missed, 'chat'), 'local');
  assert.equal(taskOf(missed, 'task'), 'audit');
});

test('sync mirrors a draft across one session file and does not mirror a route', async () => {
  const engine = createTestEngine();
  const time = createManualClock();
  const initial = (): WorkspaceState => ({
    tabs: [
      tab('chat', { sessionFile: session, draft: '' }),
      tab('task', { kind: 'task', sessionFile: session, route: route('audit') }),
    ],
    groups: [], closed: [], activeId: 'chat', nextNumber: 3, recentIds: ['chat', 'task'],
  });
  const window = createWorkspaceController({ key: 'now', client: engine.client(), writer: 'w', initial, clock: time.clock, saveDelayMs: 10 });
  window.start();
  await time.advance(50);
  window.dispatch({ type: 'draft', id: 'chat', draft: 'shared words' });
  window.dispatch({ type: 'view', id: 'task', change: { route: route('child', ['audit']) } });
  const panes = window.getState().tabs.flatMap(panesOf);
  assert.deepEqual(panes.map(pane => pane.draft), ['shared words', 'shared words']);
  assert.deepEqual(panes.map(pane => pane.route?.taskId), [undefined, 'child']);
  window.stop();
});
