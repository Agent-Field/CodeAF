// The laws of the window's structural Undo: it takes back exactly this window's step, keeps every word typed since,
// never takes back another window's change, refuses rather than forcing, and holds at most 20 steps.
import test from 'node:test';
import assert from 'node:assert/strict';
import { mintWith, setIdSource } from '../helpers.ts';
import { workspaceReducer, type Tab, type TabGroup, type WorkspaceAction, type WorkspaceState } from '../model.ts';
import { createStructuralUndo, isUndoable, undoLimit } from './structuralUndo.ts';

let counter = 0;
setIdSource(() => `u${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const group = (id: string, title = id): TabGroup => ({ id, title, collapsed: false });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const pane = (id: string, over: Partial<Tab> = {}) => ({ id, kind: 'conversation' as const, title: id, draft: '', ...over });

/** The structure a person sees: order, pins, groups by name, splits with their panes. */
const structure = (s: WorkspaceState) => s.tabs.map(t => `${t.pinned ? '*' : ''}${t.id}${t.groupId ? `@${s.groups.find(g => g.id === t.groupId)?.title}` : ''}${t.split ? `[${t.split.panes.map(p => p.id).join(',')}:${t.split.layout}]` : ''}`).join(' ');

/** A window: its own dispatch records steps exactly the way useStructuralUndo does. */
function windowOn(start: WorkspaceState) {
  const undo = createStructuralUndo();
  let current = start;
  return {
    get state() { return current; },
    set state(next: WorkspaceState) { current = next; },
    act(action: WorkspaceAction) {
      if (!isUndoable(action.type)) { current = workspaceReducer(current, action); return; }
      const { result, ids } = mintWith(action.mint, () => workspaceReducer(current, action));
      const pinned = ids.length ? { ...action, mint: ids } : action;
      undo.record(current, pinned, result);
      // The real apply of the pinned action lands on exactly the predicted tabs.
      const applied = workspaceReducer(current, pinned);
      assert.equal(structure(applied), structure(result), `${action.type} applied differently from its prediction`);
      current = applied;
    },
    /** ⌘Z: true when something was undone, false when refused or empty. */
    undo() {
      const plan = undo.take(current);
      if (plan.kind !== 'apply') return plan.kind;
      for (const action of plan.actions) current = workspaceReducer(current, action);
      return 'applied';
    },
    stack: undo,
  };
}

const base = () => state([tab('p', { pinned: true }), tab('a'), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('d'), tab('e')], { groups: [group('g', 'Bench')], activeId: 'a' });

test('each structural step is taken back exactly, and words typed since are kept', () => {
  const steps: WorkspaceAction[] = [
    { type: 'pin', id: 'a' }, { type: 'pin', id: 'p' }, { type: 'group', id: 'd', ids: ['e'] }, { type: 'group-picked' },
    { type: 'ungroup', id: 'g' }, { type: 'move-group', id: 'a', groupId: 'g' }, { type: 'move-group', id: 'b' },
    { type: 'reorder', id: 'e', targetId: 'a' }, { type: 'reorder', id: 'a', targetId: 'c' }, { type: 'reorder-group', id: 'g', targetId: 'a' },
    { type: 'split-merge', id: 'a', withId: 'd' }, { type: 'split-group', groupId: 'g' },
  ];
  for (const step of steps) {
    const w = windowOn(base());
    const before = structure(w.state);
    w.act(step);
    assert.notEqual(structure(w.state), before, `${step.type} changed nothing`);
    // Words typed into every tab after the step.
    for (const t of w.state.tabs) for (const p of t.split ? t.split.panes : [t]) w.act({ type: 'draft', id: p.id, draft: `typed in ${p.id}` });
    assert.equal(w.undo(), 'applied', step.type);
    assert.equal(structure(w.state), before, `${step.type} was not taken back exactly`);
    for (const t of w.state.tabs) for (const p of t.split ? t.split.panes : [t]) assert.equal(p.draft, `typed in ${p.id}`, `${step.type}: the words in ${p.id} were lost`);
  }
});

test('a split taken apart or closed pane by pane comes back with its panes, layout and the words in them', () => {
  const split = tab('s', { split: { layout: '2x2', focus: 0, panes: [pane('p1'), pane('p2', { draft: 'two' }), pane('p3')] } });
  for (const step of [{ type: 'split-unmerge', id: 's' }, { type: 'split-close-pane', id: 's', paneId: 'p2' }, { type: 'split-swap', id: 's', paneId: 'p1', withPaneId: 'p3' }, { type: 'split-layout', id: 's', layout: '2x2' }] as WorkspaceAction[]) {
    const w = windowOn(state([tab('x'), split, tab('y')], { activeId: 's' }));
    const before = structure(w.state);
    w.act(step);
    const changed = structure(w.state) !== before;
    if (!changed) { assert.equal(w.stack.size, 0, `${step.type} recorded a step that changed nothing`); continue; }
    assert.equal(w.undo(), 'applied', step.type);
    assert.equal(structure(w.state), before, step.type);
    assert.equal(w.state.tabs[1].split!.panes[1].draft, 'two');
    assert.ok(!w.state.closed.some(t => t.id === 'p2'), 'the pane came back out of the closed list');
  }
});

test('closing is taken back by reopening in place, newest first; a close the toast already undid is skipped', () => {
  const w = windowOn(base());
  w.act({ type: 'close', id: 'd' });
  w.act({ type: 'close-group', id: 'g' });
  assert.equal(structure(w.state), '*p a e');
  assert.equal(w.undo(), 'applied');
  assert.equal(structure(w.state), '*p a b@Bench c@Bench e');
  // The closing toast's own Undo reopened d first: ⌘Z finds that step spent and has nothing left.
  w.state = workspaceReducer(w.state, { type: 'reopen-id', id: 'd' });
  assert.equal(w.undo(), 'empty');
  assert.equal(structure(w.state), '*p a b@Bench c@Bench d e');
});

test('NEVER ANOTHER WINDOW\'S CHANGE: a step whose tabs another window changed since is refused and changes nothing', () => {
  const w = windowOn(base());
  w.act({ type: 'group', id: 'd', ids: ['e'] });
  // Another window moves d out of that group; the tab set arrives here as it now is.
  w.state = workspaceReducer(w.state, { type: 'move-group', id: 'd' });
  const theirs = w.state;
  assert.equal(w.undo(), 'refused');
  assert.equal(w.state, theirs);
  assert.equal(w.stack.size, 0, 'a refused step is dropped, never retried behind the person\'s back');
});

test('another window reordering untouched tabs also refuses: Undo never re-sorts what it did not move', () => {
  const w = windowOn(base());
  w.act({ type: 'pin', id: 'e' });
  w.state = workspaceReducer(w.state, { type: 'reorder', id: 'd', targetId: 'a' });
  const theirs = w.state;
  assert.equal(w.undo(), 'refused');
  assert.equal(w.state, theirs);
});

test('tabs another window opened, closed or typed into since are kept as they are', () => {
  const w = windowOn(base());
  w.act({ type: 'group', id: 'a', ids: ['d'] });
  // The other window opens a tab, closes an untouched one and types into a grouped one.
  w.state = workspaceReducer(w.state, { type: 'open', tab: tab('z'), background: true });
  w.state = workspaceReducer(w.state, { type: 'close', id: 'e' });
  w.state = workspaceReducer(w.state, { type: 'draft', id: 'a', draft: 'their words' });
  assert.equal(w.undo(), 'applied');
  assert.equal(structure(w.state), '*p a b@Bench c@Bench d z');
  assert.equal(w.state.tabs.find(t => t.id === 'a')!.draft, 'their words');
  assert.ok(w.state.closed.some(t => t.id === 'e'), 'a tab the other window closed stays closed');
});

test('a pane reopened elsewhere since is never pulled out of its tab to rebuild a split', () => {
  const split = tab('s', { split: { layout: '1x2', focus: 0, panes: [pane('p1'), pane('p2')] } });
  const w = windowOn(state([split, tab('x')]));
  w.act({ type: 'split-close-pane', id: 's', paneId: 'p2' });
  w.state = workspaceReducer(w.state, { type: 'reopen' });
  assert.ok(w.state.tabs.some(t => t.id === 'p2'));
  const now = w.state;
  assert.equal(w.undo(), 'refused');
  assert.equal(w.state, now);
});

test('the inverse is an ordinary action: replayed over a tab set that moved on, it refuses or applies by the same rule', () => {
  const w = windowOn(base());
  w.act({ type: 'reorder', id: 'e', targetId: 'a' });
  const plan = w.stack.take(w.state);
  assert.equal(plan.kind, 'apply');
  const inverse = (plan as { actions: WorkspaceAction[] }).actions[0];
  const json = JSON.parse(JSON.stringify(inverse)) as WorkspaceAction;
  assert.equal(structure(workspaceReducer(w.state, json)), structure(base()));
  // Applied twice (a replay after its save landed) the second is a no-op, never a second change.
  const once = workspaceReducer(w.state, json);
  assert.equal(workspaceReducer(once, json), once);
});

test('non-structural actions are never steps, and the stack holds the newest 20 steps', () => {
  const w = windowOn(base());
  for (const action of [{ type: 'draft', id: 'a', draft: 'x' }, { type: 'select', id: 'd' }, { type: 'collapse-group', id: 'g' }, { type: 'rename', id: 'a', title: 'A' }, { type: 'new' }] as WorkspaceAction[]) w.act(action);
  assert.equal(w.stack.size, 0);
  const many = windowOn(base());
  for (let i = 0; i < undoLimit + 5; i++) many.act({ type: 'pin', id: 'a' });
  assert.equal(many.stack.size, undoLimit);
  let undone = 0;
  while (many.undo() === 'applied') undone++;
  assert.equal(undone, undoLimit);
  // 25 toggles, 20 undone: the five oldest stay done, so a stays pinned.
  assert.ok(many.state.tabs.find(t => t.id === 'a')!.pinned);
});

test('ROUND TRIP: any run of structural steps, undone in reverse, gives back the strip it started from', () => {
  let seed = 11;
  const random = (n: number) => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed % n; };
  for (let round = 0; round < 40; round++) {
    const w = windowOn(base());
    const start = structure(w.state);
    for (let step = 0; step < 12; step++) {
      const ids = w.state.tabs.map(t => t.id);
      const any = () => ids[random(ids.length)];
      const someGroup = () => w.state.groups[random(Math.max(w.state.groups.length, 1))]?.id ?? 'none';
      const options: WorkspaceAction[] = [
        { type: 'pin', id: any() }, { type: 'reorder', id: any(), targetId: any(), after: random(2) === 1 },
        { type: 'group', id: any(), ids: [any()] }, { type: 'move-group', id: any(), groupId: random(2) ? someGroup() : undefined },
        { type: 'reorder-group', id: someGroup(), targetId: any(), after: random(2) === 1 }, { type: 'ungroup', id: someGroup() },
        { type: 'split-merge', id: any(), withId: any() }, { type: 'split-unmerge', id: any() }, { type: 'pick', id: any() }, { type: 'group-picked' },
        { type: 'draft', id: any(), draft: `r${round}s${step}` },
      ];
      w.act(options[random(options.length)]);
    }
    while (w.stack.size) assert.equal(w.undo(), 'applied', `round ${round}: a step of this window alone was refused`);
    assert.equal(structure(w.state), start, `round ${round}`);
  }
});
