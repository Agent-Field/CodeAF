import test from 'node:test';
import assert from 'node:assert/strict';
import { desktopTabEvent } from '../../lib/desktopMenuRoute.ts';
import { initialWorkspace, parseWorkspace, workspaceReducer, type Tab, type WorkspaceState } from '../tabs/model.ts';
import { isOpenJobDetail, jobOpenAction, openJobDetail, openJobTab, type OpenJobDetail } from './open.ts';

const job = { sessionFile: 'sessions/chat-9.json', jobId: '9', title: 'nightly-bench' };

function installWindow(): () => void {
  const target = new EventTarget();
  const previous = globalThis.window;
  globalThis.window = {
    addEventListener: (type: string, listener: EventListener) => target.addEventListener(type, listener),
    removeEventListener: (type: string, listener: EventListener) => target.removeEventListener(type, listener),
    dispatchEvent: (event: Event) => target.dispatchEvent(event),
  } as unknown as Window & typeof globalThis;
  return () => { globalThis.window = previous; };
}

/** Applies each job-open the way the strip's listener does, so a second event sees the tab the first one added. */
function follow(onOpen: (event: Event) => void): () => void {
  window.addEventListener(desktopTabEvent, onOpen);
  return () => window.removeEventListener(desktopTabEvent, onOpen);
}

function apply(state: WorkspaceState, detail: unknown): WorkspaceState {
  if (!isOpenJobDetail(detail)) return state;
  const action = jobOpenAction(state.tabs, detail);
  return action ? workspaceReducer(state, action) : state;
}

test('a second open of the same job selects the existing tab', () => {
  const restore = installWindow();
  try {
    let state = initialWorkspace();
    const stop = follow(event => { state = apply(state, (event as CustomEvent).detail); });
    try {
      const first = openJobTab(job);
      assert.ok(first);
      assert.equal(first.kind, 'terminal');
      assert.deepEqual(first.job, { jobId: '9' });
      assert.equal(first.sessionFile, job.sessionFile);
      assert.equal(first.title, 'nightly-bench');
      assert.equal(first.titleSource, 'manual');
      assert.equal(state.tabs.filter(tab => tab.job?.jobId === '9').length, 1);
      assert.equal(state.activeId, first.id);

      const conversation = state.tabs.find(tab => tab.kind === 'conversation');
      assert.ok(conversation);
      state = { ...state, activeId: conversation.id };
      const second = openJobTab(job);
      assert.ok(second);
      assert.equal(second.kind, 'terminal');
      assert.deepEqual(second.job, { jobId: '9' });
      assert.notEqual(second.id, first.id);
      assert.equal(state.tabs.filter(tab => tab.job?.jobId === '9').length, 1);
      assert.equal(state.activeId, first.id);
    } finally { stop(); }
  } finally { restore(); }
});

test('a background open adds the tab without taking focus, and a second background open leaves the strip as it is', () => {
  const restore = installWindow();
  try {
    let state = initialWorkspace();
    const home = state.activeId;
    const stop = follow(event => { state = apply(state, (event as CustomEvent).detail); });
    try {
      const first = openJobTab(job, { background: true });
      assert.ok(first);
      assert.equal(state.activeId, home);
      assert.equal(state.tabs.some(tab => tab.id === first.id), true);
      openJobTab(job, { background: true });
      assert.equal(state.tabs.filter(tab => tab.job?.jobId === '9').length, 1);
      assert.equal(state.activeId, home);
    } finally { stop(); }
  } finally { restore(); }
});

test('the same job id in another conversation is a different tab', () => {
  const first = openJobDetail(job);
  const other = openJobDetail({ ...job, sessionFile: 'sessions/chat-2.json' });
  assert.ok(first && other);
  const opened = workspaceReducer(initialWorkspace(), jobOpenAction([], first)!);
  const again = jobOpenAction(opened.tabs, other);
  assert.equal(again?.type, 'open');
  const both = workspaceReducer(opened, again!);
  assert.equal(both.tabs.filter(tab => tab.kind === 'terminal').length, 2);
});

test('a job already showing in a split pane is selected there', () => {
  const pane: Tab = { id: 'pane', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false, sessionFile: job.sessionFile, job: { jobId: '9' } };
  const split: Tab = {
    id: 'split', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false,
    split: { layout: '1x2', focus: 0, panes: [{ id: 'other', kind: 'conversation', title: 'chat', draft: '' }, pane] },
  };
  const detail = openJobDetail(job);
  assert.ok(detail);
  assert.deepEqual(jobOpenAction([split], detail), { type: 'select', id: 'pane' });
  const next = workspaceReducer({ ...initialWorkspace(), tabs: [split], activeId: 'split', recentIds: ['split'] }, { type: 'select', id: 'pane' });
  assert.equal(next.tabs[0].split?.focus, 1);
});

test('a blank session or job id opens nothing, and a menu command is not a job open', () => {
  assert.equal(openJobTab({ sessionFile: '  ', jobId: '9', title: 'nightly-bench' }), undefined);
  assert.equal(openJobTab({ sessionFile: job.sessionFile, jobId: ' ', title: 'nightly-bench' }), undefined);
  assert.equal(isOpenJobDetail('new'), false);
  assert.equal(isOpenJobDetail({ type: 'open-job' }), false);
});

test('a reloaded strip still treats the saved tab as this job', () => {
  const detail = openJobDetail(job) as OpenJobDetail;
  const opened = workspaceReducer(initialWorkspace(), jobOpenAction([], detail)!);
  const loaded = parseWorkspace(JSON.stringify(opened));
  assert.ok(loaded);
  const again = jobOpenAction(loaded.tabs, openJobDetail(job)!);
  assert.equal(again?.type, 'select');
  if (again?.type === 'select') assert.equal(again.id, detail.tab.id);
});
