import test from 'node:test';
import assert from 'node:assert/strict';
import { newTab, setIdSource, panesOf } from '../tabs/helpers.ts';
import type { Tab, WorkspaceState } from '../tabs/model.ts';
import { createWorkspaceController, type Persisted } from './controller.ts';
import { createManualClock, createTestEngine } from './testEngine.ts';

let counter = 0;
setIdSource(() => `id-${++counter}`);

const tab = (id: string, extra: Partial<Tab> = {}): Tab => newTab({ id, title: id, kind: 'conversation', ...extra });
const seed = (...ids: string[]) => (): WorkspaceState => {
  const tabs = ids.map(id => tab(id));
  return { tabs, groups: [], closed: [], activeId: tabs[0].id, nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id) };
};
const ids = (state: WorkspaceState) => state.tabs.map(t => t.id);

/** Two windows on one place, on one engine and one clock. */
async function twoWindows(...start: string[]) {
  const engine = createTestEngine();
  const time = createManualClock();
  const saved: Record<string, Persisted> = {};
  const make = (writer: string, persisted?: Persisted) => createWorkspaceController({
    key: 'now', client: engine.client(), writer, initial: seed(...start), clock: time.clock, persisted,
    persist: state => { saved[writer] = structuredClone(state); }, saveDelayMs: 10, persistDelayMs: 5,
  });
  const a = make('win-a');
  a.start();
  await time.advance(50);
  const b = make('win-b');
  b.start();
  await time.advance(50);
  return { engine, time, a, b, make, saved };
}

test('the first window seeds the engine with its imported tabs, and the second adopts them rather than importing again', async () => {
  const { engine, a, b } = await twoWindows('t1', 't2');
  assert.deepEqual(ids(a.getState()), ['t1', 't2']);
  assert.deepEqual(ids(b.getState()), ['t1', 't2']);
  assert.equal(engine.doc('now')?.revision, 1, 'one seed write, no second import');
  assert.equal(a.getStatus().phase, 'saved');
});

test('two windows opening a tab at the same moment both keep both tabs', async () => {
  const { time, a, b } = await twoWindows('t1');
  a.dispatch({ type: 'open', tab: tab('from-a'), background: true });
  b.dispatch({ type: 'open', tab: tab('from-b'), background: true });
  await time.advance(100);
  assert.deepEqual(ids(a.getState()).sort(), ['from-a', 'from-b', 't1']);
  assert.deepEqual(ids(b.getState()).sort(), ['from-a', 'from-b', 't1']);
  assert.deepEqual(ids(a.getState()), ids(b.getState()), 'one order, not two');
  assert.equal(a.getStatus().overtaken + b.getStatus().overtaken, 0);
});

test('a reorder in one window and a new tab in the other both land', async () => {
  const { time, a, b } = await twoWindows('t1', 't2', 't3');
  a.dispatch({ type: 'reorder', id: 't3', targetId: 't1' });
  b.dispatch({ type: 'new' });
  await time.advance(100);
  const order = ids(a.getState());
  assert.deepEqual(order, ids(b.getState()));
  assert.equal(order[0], 't3');
  assert.equal(order.length, 4);
});

test('a group made in one window keeps its id through a rebase over the other window\'s change', async () => {
  const { time, a, b } = await twoWindows('t1', 't2', 't3');
  a.dispatch({ type: 'group', id: 't1', ids: ['t2'], title: 'Release' });
  const groupId = a.getState().groups[0].id;
  b.dispatch({ type: 'rename', id: 't3', title: 'Renamed in B' });
  await time.advance(100);
  for (const w of [a, b]) {
    const s = w.getState();
    assert.deepEqual(s.groups.map(g => g.id), [groupId], 'the same group id everywhere');
    assert.deepEqual(s.tabs.filter(t => t.groupId === groupId).map(t => t.id), ['t1', 't2']);
    assert.equal(s.tabs.find(t => t.id === 't3')?.title, 'Renamed in B');
  }
});

test('drafts typed in two windows at once are both kept, and a burst of typing is one write', async () => {
  const { engine, time, a, b } = await twoWindows('t1', 't2');
  const before = engine.calls.filter(c => c.method === 'PUT').length;
  for (const text of ['h', 'he', 'hel', 'hello']) a.dispatch({ type: 'draft', id: 't1', draft: text });
  b.dispatch({ type: 'draft', id: 't2', draft: 'from b' });
  await time.advance(100);
  for (const w of [a, b]) {
    assert.equal(w.getState().tabs.find(t => t.id === 't1')?.draft, 'hello');
    assert.equal(w.getState().tabs.find(t => t.id === 't2')?.draft, 'from b');
  }
  const puts = engine.calls.filter(c => c.method === 'PUT').length - before;
  assert.ok(puts <= 3, `typing is coalesced: ${puts} writes`);
});

test('a draft for a tab another window closed is kept on the closed tab, and Reopen brings it back', async () => {
  const { time, a, b } = await twoWindows('t1', 't2');
  b.dispatch({ type: 'close', id: 't2' });
  a.dispatch({ type: 'draft', id: 't2', draft: 'do not lose me' });
  await time.advance(100);
  assert.deepEqual(ids(a.getState()), ['t1']);
  assert.equal(a.getState().closed.find(t => t.id === 't2')?.draft, 'do not lose me');
  b.dispatch({ type: 'reopen' });
  await time.advance(100);
  assert.equal(a.getState().tabs.find(t => t.id === 't2')?.draft, 'do not lose me');
});

test('closing the tab another window shows moves that window to the neighbour, not the first tab', async () => {
  const { time, a, b } = await twoWindows('t1', 't2', 't3', 't4');
  b.dispatch({ type: 'select', id: 't3' });
  a.dispatch({ type: 'close', id: 't3' });
  await time.advance(100);
  assert.equal(b.getState().activeId, 't4');
});

test('selecting a tab or a split pane is this window\'s alone: nothing is written and the other window does not move', async () => {
  const { engine, time, a, b } = await twoWindows('t1', 't2', 't3');
  a.dispatch({ type: 'split-merge', id: 't1', withId: 't2' });
  await time.advance(100);
  const split = a.getState().tabs.find(t => t.split)!;
  assert.equal(b.getState().tabs.find(t => t.id === split.id)?.split?.panes.length, 2, 'the split itself is shared');
  const puts = engine.calls.filter(c => c.method === 'PUT').length;
  b.dispatch({ type: 'select', id: 't3' });
  b.dispatch({ type: 'select', id: split.split!.panes[0].id });
  a.dispatch({ type: 'split-focus', id: split.id, index: 1 });
  await time.advance(100);
  assert.equal(engine.calls.filter(c => c.method === 'PUT').length, puts, 'no write for a selection');
  assert.equal(a.getState().tabs.find(t => t.id === split.id)?.split?.focus, 1);
  assert.equal(b.getState().tabs.find(t => t.id === split.id)?.split?.focus, 0);
  assert.notEqual(a.getState().activeId, 't3');
});

test('pinning the same tab in both windows at once leaves it pinned, not toggled back', async () => {
  const { time, a, b } = await twoWindows('t1', 't2');
  a.dispatch({ type: 'pin', id: 't2' });
  b.dispatch({ type: 'pin', id: 't2' });
  await time.advance(100);
  assert.equal(a.getState().tabs.find(t => t.id === 't2')?.pinned, true);
  assert.equal(b.getState().tabs.find(t => t.id === 't2')?.pinned, true);
});

test('Reopen replayed after another window closed a second tab reopens the tab it meant', async () => {
  const { time, a, b } = await twoWindows('t1', 't2', 't3');
  a.dispatch({ type: 'close', id: 't2' });
  await time.advance(100);
  a.dispatch({ type: 'reopen' });
  b.dispatch({ type: 'close', id: 't3' });
  await time.advance(100);
  assert.deepEqual(ids(a.getState()).sort(), ['t1', 't2']);
  assert.deepEqual(a.getState().closed.map(t => t.id), ['t3']);
});

test('offline: the window keeps working, says why, keeps its queue, retries on a doubling delay and catches up', async () => {
  const { engine, time, a, b } = await twoWindows('t1');
  engine.setDown(true);
  a.dispatch({ type: 'open', tab: tab('offline-1'), background: true });
  await time.advance(50);
  assert.equal(a.getStatus().phase, 'offline');
  assert.equal(a.getStatus().error, 'codeaf engine is not running');
  assert.deepEqual(ids(a.getState()), ['t1', 'offline-1'], 'the change stays on screen');
  const before = engine.calls.length;
  await time.advance(1_000);
  assert.ok(engine.calls.length - before <= 2, `no busy retry: ${engine.calls.length - before} calls in a second`);
  a.dispatch({ type: 'open', tab: tab('offline-2'), background: true });
  engine.setDown(false);
  await time.advance(60_000);
  assert.equal(a.getStatus().phase, 'saved');
  assert.deepEqual(ids(b.getState()), ['t1', 'offline-1', 'offline-2']);
});

test('a reload keeps unsaved changes, this window\'s focus and its scroll', async () => {
  const { engine, time, a, make, saved } = await twoWindows('t1', 't2');
  a.dispatch({ type: 'select', id: 't2' });
  a.setScroll('t2', 480);
  engine.setDown(true);
  a.dispatch({ type: 'open', tab: tab('unsaved'), background: true });
  await time.advance(50);
  a.stop();
  engine.setDown(false);
  const again = make('win-a', saved['win-a']);
  assert.equal(again.getState().activeId, 't2', 'focus is back before the engine answers');
  assert.equal(again.scrollOf('t2'), 480);
  assert.deepEqual(ids(again.getState()), ['t1', 't2', 'unsaved']);
  again.start();
  await time.advance(100);
  assert.deepEqual((engine.doc('now')!.workspace as { tabs: { id: string }[] }).tabs.map(t => t.id), ['t1', 't2', 'unsaved']);
});

test('a save whose answer was lost is not applied twice', async () => {
  const { engine, time, a, b } = await twoWindows('t1');
  engine.loseNextAnswer();
  a.dispatch({ type: 'open', tab: tab('once'), background: true });
  a.dispatch({ type: 'group', id: 'once', title: 'G' });
  await time.advance(100);
  b.dispatch({ type: 'rename', id: 't1', title: 'moved on' });
  await time.advance(60_000);
  for (const w of [a, b]) {
    assert.deepEqual(ids(w.getState()), ['t1', 'once']);
    assert.equal(w.getState().groups.length, 1);
  }
  assert.equal(a.getStatus().phase, 'saved');
});

test('a write from outside (another process) is mirrored live', async () => {
  const { engine, time, a } = await twoWindows('t1');
  const current = engine.doc('now')!.workspace as { tabs: unknown[] };
  engine.write('now', { ...current, tabs: [...current.tabs, { id: 'outside', title: 'Outside', draft: '', pinned: false, kind: 'conversation' }] });
  await time.advance(10);
  assert.deepEqual(ids(a.getState()), ['t1', 'outside']);
});

test('a refusal is said in the engine\'s words and not retried until the next change', async () => {
  const engine = createTestEngine();
  const time = createManualClock();
  const evil = createWorkspaceController({
    key: 'now', client: engine.client(), writer: 'win-x', initial: seed('t1'), clock: time.clock, saveDelayMs: 10,
    // A reducer that leaks a window-local field into a tab: the engine must refuse it.
    reduce: (state, action) => (action.type === 'rename' ? { ...state, tabs: state.tabs.map(t => ({ ...t, activeId: 'x' } as Tab)) } : state),
  });
  evil.start();
  await time.advance(50);
  evil.dispatch({ type: 'rename', id: 't1', title: 'x' });
  await time.advance(100);
  assert.equal(evil.getStatus().phase, 'refused');
  assert.match(evil.getStatus().error ?? '', /could not be saved/);
  const calls = engine.calls.length;
  await time.advance(120_000);
  assert.ok(engine.calls.length - calls <= 6, 'only the long poll runs, no write retries');
});

test('Move to new window on the same place hands focus over without taking the tab from either window', async () => {
  const { engine, time, a, make } = await twoWindows('t1', 't2', 't3');
  a.dispatch({ type: 'select', id: 't2' });
  const puts = engine.calls.filter(c => c.method === 'PUT').length;
  const handed = a.handoff('t2');
  assert.deepEqual(handed, { key: 'now', tabId: 't2' });
  assert.equal(a.getState().activeId, 't3');
  assert.deepEqual(ids(a.getState()), ['t1', 't2', 't3'], 'the tab stays: both windows mirror one tab set');
  const fresh = make('win-c');
  const opened = createWorkspaceController({ key: 'now', client: engine.client(), writer: 'win-new', initial: seed('unused'), clock: time.clock, focus: handed!.tabId });
  opened.start();
  await time.advance(50);
  assert.equal(opened.getState().activeId, 't2');
  assert.equal(engine.calls.filter(c => c.method === 'PUT').length, puts, 'a handoff writes nothing');
  fresh.stop();
});

test('a draft is one composer for every pane of the same conversation', async () => {
  const engine = createTestEngine();
  const time = createManualClock();
  const twin = (): WorkspaceState => {
    const tabs = [tab('p1', { sessionFile: '/s/a/session.jsonl' }), tab('p2', { sessionFile: '/s/a/session.jsonl' }), tab('p3', { sessionFile: '/s/b/session.jsonl' })];
    return { tabs, groups: [], closed: [], activeId: 'p1', nextNumber: 4, recentIds: ['p1', 'p2', 'p3'] };
  };
  const w = createWorkspaceController({ key: 'now', client: engine.client(), writer: 'w', initial: twin, clock: time.clock, saveDelayMs: 10 });
  w.start();
  await time.advance(50);
  w.dispatch({ type: 'draft', id: 'p1', draft: 'same words' });
  const drafts = w.getState().tabs.flatMap(panesOf).map(p => p.draft);
  assert.deepEqual(drafts, ['same words', 'same words', '']);
});

test('two windows typing into the same draft at once: the later typing wins, and the overwrite is counted, not silent', async () => {
  const { time, a, b } = await twoWindows('t1');
  a.dispatch({ type: 'draft', id: 't1', draft: 'words from a' });
  b.dispatch({ type: 'draft', id: 't1', draft: 'words from b' });
  await time.advance(100);
  const drafts = [a, b].map(w => w.getState().tabs[0].draft);
  assert.equal(drafts[0], drafts[1], 'one composer');
  assert.equal(a.getStatus().overtaken + b.getStatus().overtaken, 1, 'the overwritten words are counted once');
});
