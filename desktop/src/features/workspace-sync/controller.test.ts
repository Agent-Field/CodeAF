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

test('native focused-view arrival ignores a retired Inbox action before canonical load', async () => {
  const { engine, time, a } = await twoWindows('first', 'web-target');
  const incoming = createWorkspaceController({ key: 'now', client: engine.client(), writer: 'win-incoming', initial: seed('fresh-import'), focus: 'web-target', clock: time.clock });
  incoming.dispatch({ type: 'ensure-inbox' });
  incoming.start();
  await time.advance(100);
  assert.equal(incoming.getState().activeId, 'web-target');
  assert.deepEqual(ids(incoming.getState()), ['first', 'web-target']);
  assert.equal(a.getState().activeId, 'first', 'source focus stays window-local');
});


test('a first-ever offline launch preserves its seed, pending new tabs and drafts across reload', async () => {
  const engine = createTestEngine();
  engine.setDown(true);
  const time = createManualClock();
  let saved: Persisted | undefined;
  const make = (persisted?: Persisted) => createWorkspaceController({
    key: 'now', client: engine.client(), writer: 'offline-first', initial: seed('fresh-import'),
    persisted, clock: time.clock, persist: state => { saved = structuredClone(state); },
  });
  const first = make();
  first.start();
  first.dispatch({ type: 'open', tab: tab('queued-chat') });
  first.dispatch({ type: 'draft', id: 'queued-chat', draft: 'Keep these words' });
  await time.advance(200);
  first.stop();
  assert.ok(saved?.base, 'the import seed is durable before any engine answer');
  const reloaded = make(saved);
  assert.deepEqual(ids(reloaded.getState()), ['fresh-import', 'queued-chat']);
  assert.equal(reloaded.getState().tabs.find(t => t.id === 'queued-chat')?.draft, 'Keep these words');
  reloaded.start();
  engine.setDown(false);
  await time.advance(10000);
  assert.equal(reloaded.getStatus().phase, 'saved');
  assert.deepEqual(ids(reloaded.getState()), ['fresh-import', 'queued-chat']);
  reloaded.stop();
});


test('structural Undo minted ids stay stable through canonical replay and persisted queue reload', async () => {
  const { engine, time, a, b, saved, make } = await twoWindows('t1', 't2', 't3');
  engine.setDown(true);
  a.dispatch({ type: 'group', id: 't1', ids: ['t2'], title: 'Deterministic', mint: ['undo-group-id'] });
  await time.advance(200);
  assert.equal(a.getState().groups[0]?.id, 'undo-group-id');
  a.stop();
  assert.deepEqual(saved['win-a'].pending[0]?.action.mint, ['undo-group-id']);
  const reopened = make('win-a', saved['win-a']);
  reopened.start();
  b.dispatch({ type: 'rename', id: 't3', title: 'Other window words' });
  engine.setDown(false);
  await time.advance(10000);
  assert.equal(reopened.getState().groups[0]?.id, 'undo-group-id');
  assert.equal(b.getState().groups[0]?.id, 'undo-group-id');
  assert.equal(reopened.getState().tabs.find(t => t.id === 't3')?.title, 'Other window words');
  reopened.stop();
  b.stop();
});


test('picked group intent survives offline reload and another window rebase', async () => {
  const { engine, time, a, b, saved, make } = await twoWindows('t1', 't2', 't3', 't4');
  engine.setDown(true);
  a.dispatch({ type: 'pick', id: 't2' });
  a.dispatch({ type: 'pick', id: 't3' });
  a.dispatch({ type: 'group-picked', mint: ['picked-group'] });
  await time.advance(200);
  a.stop();
  assert.deepEqual(saved['win-a'].pending[0]?.action, { type: 'group', id: 't1', ids: ['t2', 't3'], mint: ['picked-group'] });
  const reopened = make('win-a', saved['win-a']);
  reopened.start();
  b.dispatch({ type: 'draft', id: 't4', draft: 'keep other window' });
  engine.setDown(false);
  await time.advance(10000);
  for (const w of [reopened, b]) {
    assert.deepEqual(w.getState().tabs.filter(t => t.groupId === 'picked-group').map(t => t.id), ['t1', 't2', 't3']);
    assert.equal(w.getState().tabs.find(t => t.id === 't4')?.draft, 'keep other window');
    w.stop();
  }
});

test('a second window late draft follows the durable relocation and survives reload without resurrecting its source tab', async () => {
  const { engine, time, a, b, saved } = await twoWindows('home', 'moving', 'other');
  const destination = 'pl_0123456789abcdef';
  const original = engine.doc('now')!.workspace as import('./shared.ts').SharedWorkspace;
  engine.write(destination, { ...original, tabs: [tab('place-home'), original.tabs[1]] });
  b.dispatch({ type: 'draft', id: 'moving', draft: 'words typed before the transfer answer' });
  engine.write('now', { ...original, tabs: original.tabs.filter(t => t.id !== 'moving') }, { moving: destination });
  await time.advance(100);
  const there = engine.doc(destination)!.workspace as import('./shared.ts').SharedWorkspace;
  assert.equal(there.tabs.find(t => t.id === 'moving')?.draft, 'words typed before the transfer answer');
  assert.deepEqual(ids(a.getState()), ['home', 'other']);
  assert.deepEqual(ids(b.getState()), ['home', 'other']);
  assert.equal(saved['win-b'].pending.length, 0, 'retired only after destination write acknowledgment');
  assert.equal(b.getStatus().overtaken, 0);
  assert.ok(engine.calls.every(call => call.path.startsWith('/workspaces/')), 'no session stop or model request');
  a.stop(); b.stop();
  const reloaded = createWorkspaceController({ key: 'now', client: engine.client(), writer: 'win-b', initial: seed('fallback'), persisted: saved['win-b'], clock: time.clock });
  reloaded.start(); await time.advance(50);
  assert.deepEqual(ids(reloaded.getState()), ['home', 'other']);
  reloaded.stop();
});

test('a missing relocation hint preserves an unresolved late draft locally and reports refusal', async () => {
  const { engine, time, a, b, saved } = await twoWindows('home', 'moving');
  b.dispatch({ type: 'draft', id: 'moving', draft: 'do not lose these words' });
  const original = engine.doc('now')!.workspace as import('./shared.ts').SharedWorkspace;
  engine.write('now', { ...original, tabs: original.tabs.filter(t => t.id !== 'moving') });
  await time.advance(100);
  assert.equal(b.getStatus().code, 'relocation_missing');
  assert.equal(saved['win-b'].pending[0]?.action.type, 'draft');
  assert.equal((saved['win-b'].pending[0]?.action as { draft: string }).draft, 'do not lose these words');
  assert.deepEqual(ids(b.getState()), ['home']);
  a.stop(); b.stop();
});

test('late-draft forwarding rebases a destination CAS race and retries a lost acknowledgment without duplicate tabs', async () => {
  const engine = createTestEngine(), time = createManualClock();
  const destination = 'pl_0123456789abcdef';
  const sourceDoc = { schema: 1 as const, tabs: [tab('home'), tab('moving')], groups: [], closed: [], nextNumber: 3 };
  engine.write('now', sourceDoc);
  engine.write(destination, { ...sourceDoc, tabs: [tab('place-home'), tab('moving')] });
  const original = engine.client();
  let raced = false;
  const racing = { ...original, put: async (...args: Parameters<typeof original.put>) => {
    if (args[0] === destination && !raced) {
      raced = true;
      const current = engine.doc(destination)!.workspace as import('./shared.ts').SharedWorkspace;
      engine.write(destination, { ...current, tabs: [...current.tabs, tab('other-window-later')] });
    }
    return original.put(...args);
  } };
  let saved: Persisted | undefined;
  const window = createWorkspaceController({ key: 'now', client: racing, writer: 'late-writer', initial: seed('fallback'), clock: time.clock, persist: value => { saved = structuredClone(value); }, saveDelayMs: 10 });
  window.start(); await time.advance(30);
  window.dispatch({ type: 'draft', id: 'moving', draft: 'latest queued words' });
  engine.write('now', { ...sourceDoc, tabs: [tab('home')] }, { moving: destination });
  engine.loseNextAnswer();
  await time.advance(100);
  assert.ok(saved!.pending.length, 'lost answer leaves the intent durable locally');
  assert.equal(window.getStatus().phase, 'offline');
  await time.advance(5000);
  const current = engine.doc(destination)!.workspace as import('./shared.ts').SharedWorkspace;
  assert.deepEqual(current.tabs.map(t => t.id), ['place-home', 'moving', 'other-window-later']);
  assert.equal(current.tabs.find(t => t.id === 'moving')?.draft, 'latest queued words');
  assert.equal(saved!.pending.length, 0);
  assert.deepEqual(ids(window.getState()), ['home']);
  window.stop();
});

test('reload replays an unsaved foreground open without replacing a later explicit selection', async () => {
 const {a,make,saved} = await twoWindows('first');
 a.dispatch({type:'open',tab:tab('pending-terminal'),background:false});
 a.dispatch({type:'select',id:'first'});
 a.persistNow();
 assert.equal(saved['win-a'].pending.length,1,'exercise an unconfirmed open, not a settled queue');
 assert.equal(saved['win-a'].local.activeId,'first');
 const restored=make('win-a-reloaded',structuredClone(saved['win-a']));
 assert.deepEqual(ids(restored.getState()),['first','pending-terminal']);
 assert.equal(restored.getState().activeId,'first','the later window-local choice survives shared replay');
});

test('reload keeps foreground focus on a pending new tab when the person has not selected elsewhere', async () => {
 const {a,make,saved}=await twoWindows('first');
 a.dispatch({type:'open',tab:tab('pending-terminal'),background:false});
 a.persistNow();
 const restored=make('win-a-reloaded',structuredClone(saved['win-a']));
 assert.equal(restored.getState().activeId,'pending-terminal');
});

test('remote rebase of a pending foreground open preserves the later local selection and recency', async () => {
 const {a,b,time}=await twoWindows('first','second');
 a.dispatch({type:'open',tab:tab('pending-terminal'),background:false});
 a.dispatch({type:'select',id:'first'});
 b.dispatch({type:'open',tab:tab('other-window'),background:true});
 await time.advance(150);
 assert.deepEqual(ids(a.getState()).sort(),['first','second','pending-terminal','other-window'].sort());
 assert.equal(a.getState().activeId,'first');
 assert.equal(a.getState().recentIds[0],'first');
});

test('a pending new-tab terminal keeps foreground focus on restore before its canonical base contains it', async () => {
  const { a, make, saved } = await twoWindows('first');
  a.dispatch({ type: 'new' });
  const id = a.getState().activeId;
  a.dispatch({ type: 'newtab-become', id, kind: 'terminal', title: 'zsh', sessionFile: 's', terminalId: 'term' });
  a.persistNow();
  assert.equal(saved['win-a'].base?.tabs.some(tab => tab.id === id), false);
  assert.equal(saved['win-a'].pending.length, 2);
  assert.equal(saved['win-a'].local.activeId, id);
  const restored = make('win-a-reloaded', structuredClone(saved['win-a']));
  assert.equal(restored.getState().activeId, id);
  assert.equal(restored.getState().tabs.find(tab => tab.id === id)?.kind, 'terminal');
});

test('an obsolete bootstrap response cannot replace focus while the restarted controller saves a pending terminal', async () => {
  const { a, engine, time, saved } = await twoWindows('first');
  a.dispatch({ type: 'new' });
  const id = a.getState().activeId;
  a.dispatch({ type: 'newtab-become', id, kind: 'terminal', title: 'zsh', sessionFile: 's', terminalId: 'term' });
  a.stop();
  const client = engine.client();
  const reads: ((value: Awaited<ReturnType<typeof client.get>>) => void)[] = [];
  let finishWrite: (() => Promise<void>) | undefined;
  const restarted = createWorkspaceController({
    key: 'now', writer: 'restarted', initial: seed('first'), persisted: structuredClone(saved['win-a']), clock: time.clock,
    client: {
      ...client,
      get: () => new Promise(resolve => reads.push(resolve)),
      put: (...args) => new Promise(resolve => { finishWrite = async () => { resolve(await client.put(...args)); }; }),
    },
  });
  restarted.start();
  restarted.stop();
  restarted.start();
  const old = await client.get('now');
  reads[1](old);
  await time.advance(0);
  assert.ok(restarted.inspect().inflight, 'latest start is saving the pending terminal');
  reads[0](old);
  await time.advance(0);
  try {
    assert.equal(restarted.getState().activeId, id, 'obsolete start must not rebuild over an in-flight foreground tab');
  } finally {
    await finishWrite?.();
    await time.advance(0);
    restarted.stop();
  }
});

test('a late previous-lifecycle write acknowledgement preserves newer drafts and foreground terminal identity', async () => {
  const { engine, time } = await twoWindows('first');
  const client = engine.client();
  let releaseWrite: (() => void) | undefined;
  let first = true;
  const controller = createWorkspaceController({
    key: 'now', writer: 'late-write', initial: seed('first'), clock: time.clock, saveDelayMs: 10,
    client: {
      ...client,
      put: async (...args) => {
        const answer = await client.put(...args);
        if (first) { first = false; await new Promise<void>(resolve => { releaseWrite = resolve; }); }
        return answer;
      },
    },
  });
  controller.start(); await time.advance(50);
  controller.dispatch({ type: 'open', tab: tab('terminal', {kind:'terminal',target:{terminalId:'term'}}), background: false });
  controller.dispatch({ type: 'draft', id: 'first', draft: 'initial' });
  await time.advance(20);
  assert.ok(controller.inspect().inflight);
  controller.stop(); controller.start();
  controller.dispatch({ type: 'draft', id: 'first', draft: 'newer while restarting' });
  releaseWrite?.(); await time.advance(150);
  assert.equal(controller.getState().activeId,'terminal');
  assert.equal(controller.getState().tabs.find(tab=>tab.id==='first')?.draft,'newer while restarting');
  const record=await client.get('now');
  assert.equal(record.workspace?.tabs.find(tab=>tab.id==='first')?.draft,'newer while restarting');
  assert.equal(record.workspace?.tabs.filter(tab=>tab.id==='terminal').length,1);
  assert.equal(controller.inspect().pending.length,0);
  controller.stop();
});

test('a late previous-lifecycle watch response cannot replace the restarted window selection or saved draft', async () => {
  const { engine,time }=await twoWindows('first');
  const client=engine.client();
  const old=await client.get('now');
  let releaseWatch: ((record:typeof old)=>void)|undefined;
  let first=true;
  const controller=createWorkspaceController({key:'now',writer:'late-watch',initial:seed('first'),clock:time.clock,saveDelayMs:10,
    client:{...client,wait:(...args)=>{if(first){first=false;return new Promise(resolve=>{releaseWatch=resolve;});}return client.wait(...args);}},
  });
  controller.start();await time.advance(50);
  controller.dispatch({type:'open',tab:tab('terminal',{kind:'terminal',target:{terminalId:'term'}}),background:false});
  controller.dispatch({type:'draft',id:'first',draft:'keep this'});
  await time.advance(100);
  controller.stop();controller.start();await time.advance(50);
  releaseWatch?.(old);await time.advance(50);
  assert.equal(controller.getState().activeId,'terminal');
  assert.equal(controller.getState().tabs.find(tab=>tab.id==='first')?.draft,'keep this');
  controller.stop();
});

test('a delayed same-lifecycle watch response cannot rewind an acknowledged terminal and its focus', async () => {
  const { engine, time } = await twoWindows('first');
  const client = engine.client();
  const old = await client.get('now');
  let releaseWatch: ((record: typeof old) => void) | undefined;
  let first = true;
  const controller = createWorkspaceController({ key: 'now', writer: 'delayed-watch', initial: seed('first'), clock: time.clock, saveDelayMs: 10,
    client: { ...client, wait: (...args) => { if (first) { first = false; return new Promise(resolve => { releaseWatch = resolve; }); } return client.wait(...args); } },
  });
  controller.start(); await time.advance(50);
  controller.dispatch({ type: 'open', tab: tab('terminal', {kind:'terminal',target:{terminalId:'term'}}), background: false });
  await time.advance(100);
  releaseWatch?.(old); await time.advance(50);
  assert.equal(controller.getState().activeId, 'terminal');
  assert.equal(controller.getState().tabs.filter(tab=>tab.id==='terminal').length, 1);
  controller.stop();
});

test('saved Inbox slots drop without losing other tabs, drafts, groups or closed records', async () => {
  const { parseShared } = await import('./shared.ts');
  const { parseWorkspace } = await import('../tabs/model.ts');
  const old = { tabs: [{ id: 'inbox', kind: 'inbox', pinned: true }, tab('keep', { draft: 'keep my words', groupId: 'g' })], groups: [{ id: 'g', title: 'Work', collapsed: false }], closed: [tab('closed'), { id: 'old', kind: 'inbox' }], nextNumber: 9, activeId: 'inbox', recentIds: ['inbox', 'keep'] };
  const local = parseWorkspace(JSON.stringify(old))!;
  assert.deepEqual(ids(local), ['keep']);
  assert.equal(local.activeId, 'keep');
  assert.equal(local.tabs[0].draft, 'keep my words');
  assert.deepEqual(local.closed.map(t => t.id), ['closed']);
  const base = parseShared({ ...old, schema: 1 })!;
  const engine = createTestEngine(), time = createManualClock();
  engine.write('now', { ...old, schema: 1 } as never);
  const controller = createWorkspaceController({ key: 'now', writer: 'migrate', client: engine.client(), initial: seed('fallback'), persisted: { base, revision: 1, pending: [], local: { activeId: 'inbox', recentIds: ['inbox', 'keep'], focus: {}, scroll: {} } }, clock: time.clock });
  controller.start(); await time.advance(50);
  assert.deepEqual(ids(controller.getState()), ['keep']);
  assert.equal(controller.getState().tabs[0].draft, 'keep my words');
  assert.deepEqual(controller.getState().groups, old.groups);
  controller.stop();
});

test('an Inbox-only save becomes a quiet New tab and mixed splits keep their remaining draft', async () => {
  const { parseWorkspace } = await import('../tabs/model.ts');
  const { parseShared } = await import('./shared.ts');
  const old = { tabs: [{ id: 'inbox', kind: 'inbox', pinned: true }], closed: [tab('closed')], groups: [], nextNumber: 4 };
  for (const state of [parseWorkspace(JSON.stringify(old))!, parseShared({ ...old, schema: 1 })!]) {
    assert.equal(state.tabs[0].kind, 'newtab');
    assert.deepEqual(state.closed.map(t => t.id), ['closed']);
    assert.equal(state.nextNumber, 4);
  }
  const split = { ...tab('split'), split: { layout: '1x2', focus: 1, panes: [{ id: 'gone', kind: 'inbox' }, tab('keep', { draft: 'surviving pane' })] } };
  for (const state of [parseWorkspace(JSON.stringify({ ...old, tabs: [split] }))!, parseShared({ ...old, schema: 1, tabs: [split] })!]) {
    assert.equal(state.tabs[0].id, 'split');
    assert.equal(state.tabs[0].draft, 'surviving pane');
    assert.equal(state.tabs[0].split, undefined);
  }
});

test('an offline queued Inbox open cannot recreate the retired kind on reload', () => {
  const engine = createTestEngine();
  const pending = [{ action: { type: 'open', background: false, tab: { ...tab('inbox'), kind: 'inbox', pinned: true } }, ids: [] }];
  const controller = createWorkspaceController({ key: 'now', writer: 'old-window', client: engine.client(), initial: seed('keep'), persisted: { revision: 0, pending: pending as never, local: { activeId: 'inbox', recentIds: ['inbox'], focus: {}, scroll: {} } } });
  assert.deepEqual(ids(controller.getState()), ['keep']);
  assert.equal(controller.inspect().pending.length, 0);
});
