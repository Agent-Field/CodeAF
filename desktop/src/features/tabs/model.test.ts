import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from './helpers.ts';
import { cleanView } from './view-state.ts';
import { initialWorkspace, panesOf, readWorkspace, storageKey, visibleTabs, workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from './model.ts';

let counter = 0;
setIdSource(() => `id${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);
const ids = (s: WorkspaceState) => s.tabs.map(t => t.id);
const only = (s: WorkspaceState, id: string) => s.tabs.find(t => t.id === id)!;

// ---- tabs ----------------------------------------------------------------

test('new adds an active new-tab field, optionally of another kind or in a group', () => {
  const s = run(state([tab('a')]), { type: 'new' });
  assert.equal(s.tabs.length, 2);
  assert.equal(s.tabs[1].kind, 'newtab');
  assert.equal(s.activeId, s.tabs[1].id);
  assert.equal(s.nextNumber, 3);
  const g = run(state([tab('a', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: true }] }), { type: 'new', groupId: 'g', kind: 'conversation' });
  assert.equal(g.tabs[1].kind, 'conversation');
  assert.equal(g.groups[0].collapsed, false);
});

test('open and open-task add a tab, foreground or background', () => {
  const base = state([tab('a')]);
  const fg = run(base, { type: 'open', tab: tab('t', { kind: 'task' }), background: false });
  assert.equal(fg.activeId, 't');
  assert.equal(fg.recentIds[0], 't');
  const bg = run(base, { type: 'open-task', tab: tab('t', { kind: 'task' }), background: true });
  assert.equal(bg.activeId, 'a');
  assert.deepEqual(bg.recentIds, ['a', 't']);
});

test('select activates a tab, expands its group and keeps recents most-recent first', () => {
  const s = state([tab('a'), tab('b', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: true }] });
  const next = run(s, { type: 'select', id: 'b' });
  assert.equal(next.activeId, 'b');
  assert.equal(next.groups[0].collapsed, false);
  assert.deepEqual(next.recentIds, ['b', 'a']);
});

test('select by a pane id activates its split tab and focuses that pane', () => {
  const merged = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b' });
  const splitId = merged.activeId;
  const first = run(merged, { type: 'select', id: 'a' });
  assert.equal(first.activeId, splitId);
  assert.equal(only(first, splitId).split!.focus, 0);
  assert.equal(run(first, { type: 'select', id: 'b' }).tabs[0].split!.focus, 1);
});

test('close picks the neighbour, records the tab and never leaves the strip empty', () => {
  const s = state([tab('a'), tab('b'), tab('c')], { activeId: 'b' });
  const next = run(s, { type: 'close', id: 'b' });
  assert.deepEqual(ids(next), ['a', 'c']);
  assert.equal(next.activeId, 'c');
  assert.equal(next.closed[0].id, 'b');
  const last = run(state([tab('a')]), { type: 'close', id: 'a' });
  assert.equal(last.tabs.length, 1);
  assert.notEqual(last.tabs[0].id, 'a');
  assert.equal(last.closed[0].id, 'a');
  assert.equal(run(s, { type: 'close', id: 'missing' }), s);
});

test('close a background tab keeps the active one; closing the last member removes its group', () => {
  const s = state([tab('a'), tab('b', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: false }] });
  const next = run(s, { type: 'close', id: 'b' });
  assert.equal(next.activeId, 'a');
  assert.equal(next.groups.length, 0);
});

test('reopen restores the last closed tab with its draft, in its group even when closing emptied the group', () => {
  const s = state([tab('a'), tab('b', { draft: 'keep', groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: false }] });
  const closed = run(s, { type: 'close', id: 'b' });
  assert.deepEqual(closed.groups, []);
  const back = run(closed, { type: 'reopen' });
  assert.equal(only(back, 'b').draft, 'keep');
  assert.equal(back.activeId, 'b');
  assert.equal(only(back, 'b').groupId, 'g');
  assert.deepEqual(back.groups, [{ id: 'g', title: 'G', collapsed: false }]);
  assert.equal(run(s, { type: 'reopen' }), s);
});

test('pin toggles, leaves any group and orders pinned first', () => {
  const s = state([tab('a'), tab('b', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: false }] });
  const pinned = run(s, { type: 'pin', id: 'b' });
  assert.equal(only(pinned, 'b').pinned, true);
  assert.equal(only(pinned, 'b').groupId, undefined);
  assert.equal(pinned.groups.length, 0);
  assert.deepEqual(visibleTabs(pinned).map(t => t.id), ['b', 'a']);
  assert.equal(only(run(pinned, { type: 'pin', id: 'b' }), 'b').pinned, false);
});

test('rename is manual and wins over later engine titles; blank falls back', () => {
  let s = run(state([tab('a')]), { type: 'rename', id: 'a', title: '  Mine  ' });
  assert.equal(only(s, 'a').title, 'Mine');
  assert.equal(only(s, 'a').titleSource, 'manual');
  s = run(s, { type: 'title', id: 'a', title: 'Engine', source: 'engine' });
  assert.equal(only(s, 'a').title, 'Mine');
  assert.equal(only(run(s, { type: 'rename', id: 'a', title: ' ' }), 'a').title, 'New conversation');
});

test('title ranks message below engine, ignores blanks and repeats', () => {
  let s = run(state([tab('a')]), { type: 'title', id: 'a', title: 'First line', source: 'message' });
  assert.equal(only(s, 'a').titleSource, 'message');
  s = run(s, { type: 'title', id: 'a', title: 'From engine', source: 'engine' });
  assert.equal(only(s, 'a').title, 'From engine');
  const same = run(s, { type: 'title', id: 'a', title: 'Later line', source: 'message' });
  assert.equal(same, s);
  assert.equal(run(s, { type: 'title', id: 'a', title: '  ', source: 'engine' }), s);
});

test('view and draft update the tab, or the pane inside a split', () => {
  let s = run(state([tab('a')]), { type: 'draft', id: 'a', draft: 'hello' }, { type: 'view', id: 'a', change: { sessionFile: 'x.jsonl' } });
  assert.equal(only(s, 'a').draft, 'hello');
  assert.equal(only(s, 'a').sessionFile, 'x.jsonl');
  s = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b' });
  s = run(s, { type: 'draft', id: 'b', draft: 'in pane' }, { type: 'view', id: 'b', change: { sessionFile: 'p.jsonl' } });
  const panes = panesOf(s.tabs[0]);
  assert.equal(panes[1].draft, 'in pane');
  assert.equal(panes[1].sessionFile, 'p.jsonl');
  assert.equal(panes[0].draft, '');
});

test('reorder moves a tab before its target and never changes its pin', () => {
  const s = state([tab('c', { pinned: true }), tab('a'), tab('b')]);
  const next = run(s, { type: 'reorder', id: 'b', targetId: 'a' });
  assert.deepEqual(ids(next), ['c', 'b', 'a']);
  // Dropped on a pinned tab, a loose tab stays loose at the head of the loose tabs.
  const onPinned = run(s, { type: 'reorder', id: 'b', targetId: 'c' });
  assert.deepEqual(ids(onPinned), ['c', 'b', 'a']);
  assert.equal(only(onPinned, 'b').pinned, false);
  assert.equal(run(s, { type: 'reorder', id: 'a', targetId: 'a' }), s);
  assert.equal(run(s, { type: 'reorder', id: 'a', targetId: 'zzz' }), s);
});

// ---- groups --------------------------------------------------------------

test('group makes a uniquely named group, from one tab or a selection', () => {
  const s = state([tab('a'), tab('b'), tab('c')]);
  const one = run(s, { type: 'group', id: 'a' });
  assert.equal(one.groups[0].title, 'New group');
  assert.equal(only(one, 'a').groupId, one.groups[0].id);
  const two = run(one, { type: 'group', id: 'b' });
  assert.equal(two.groups[1].title, 'New group 2');
  const many = run(s, { type: 'group', id: 'a', ids: ['b', 'c'], title: 'Release' });
  assert.equal(many.groups[0].title, 'Release');
  assert.deepEqual(many.tabs.map(t => t.groupId), [many.groups[0].id, many.groups[0].id, many.groups[0].id]);
  assert.equal(run(s, { type: 'group', id: 'zzz' }), s);
});

test('move-group adds a tab to a group, expands it, and ungroups with no target', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b')], { groups: [{ id: 'g', title: 'G', collapsed: true }] });
  const moved = run(s, { type: 'move-group', id: 'b', groupId: 'g' });
  assert.equal(only(moved, 'b').groupId, 'g');
  assert.equal(moved.groups[0].collapsed, false);
  assert.equal(only(run(moved, { type: 'move-group', id: 'b' }), 'b').groupId, undefined);
  assert.equal(run(s, { type: 'move-group', id: 'b', groupId: 'nope' }), s);
});

test('rename-group, collapse-group and ungroup', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: false }] });
  assert.equal(run(s, { type: 'rename-group', id: 'g', title: ' Done ' }).groups[0].title, 'Done');
  assert.equal(run(s, { type: 'rename-group', id: 'g', title: ' ' }).groups[0].title, 'New group');
  const folded = run(s, { type: 'collapse-group', id: 'g' });
  assert.equal(folded.groups[0].collapsed, true);
  assert.equal(run(folded, { type: 'collapse-group', id: 'g' }).groups[0].collapsed, false);
  const apart = run(s, { type: 'ungroup', id: 'g' });
  assert.equal(apart.groups.length, 0);
  assert.deepEqual(apart.tabs.map(t => t.groupId), [undefined, undefined]);
});

test('a collapsed group shows only its active member in the strip order', () => {
  const s = state([tab('a'), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: true }], activeId: 'c' });
  assert.deepEqual(visibleTabs(s).map(t => t.id), ['a', 'c']);
});

// ---- split ---------------------------------------------------------------

test('split-merge folds two tabs into one merged tab that keeps the host place and group', () => {
  const s = state([tab('a', { draft: 'da', groupId: 'g' }), tab('b', { draft: 'db' }), tab('c')], { groups: [{ id: 'g', title: 'G', collapsed: false }] });
  const next = run(s, { type: 'split-merge', id: 'a', withId: 'b' });
  assert.equal(next.tabs.length, 2);
  const merged = next.tabs[0];
  assert.equal(next.activeId, merged.id);
  assert.equal(merged.groupId, 'g');
  assert.deepEqual(merged.split!.panes.map(p => p.id), ['a', 'b']);
  assert.deepEqual(merged.split!.panes.map(p => p.draft), ['da', 'db']);
  assert.equal(merged.split!.layout, '1x2');
  assert.equal(merged.split!.focus, 1);
  assert.equal(merged.title, 'a · b');
  assert.equal(merged.draft, '');
  assert.deepEqual(next.recentIds.slice(0, 2), [merged.id, 'c']);
});

test('split-merge grows a split to a 2x2 grid, refuses a fifth pane, pinned tabs and itself', () => {
  let s = run(state([tab('a'), tab('b'), tab('c'), tab('d'), tab('e')]), { type: 'split-merge', id: 'a', withId: 'b' });
  const id = s.activeId;
  s = run(s, { type: 'split-merge', id: id, withId: 'c' });
  assert.equal(only(s, id).split!.layout, '2x2');
  assert.equal(only(s, id).split!.focus, 2);
  s = run(s, { type: 'split-merge', id, withId: 'd' });
  assert.equal(only(s, id).split!.panes.length, 4);
  const full = run(s, { type: 'split-merge', id, withId: 'e' });
  assert.equal(full, s);
  assert.equal(run(state([tab('a'), tab('b', { pinned: true })]), { type: 'split-merge', id: 'a', withId: 'b' }).tabs.length, 2);
  assert.equal(run(state([tab('a')]), { type: 'split-merge', id: 'a', withId: 'a' }).tabs.length, 1);
});

test('split-merge honours an explicit layout and merges a split into a split up to four panes', () => {
  const two = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b', layout: '2x1' });
  assert.equal(two.tabs[0].split!.layout, '2x1');
  const s = state([tab('a'), tab('b'), tab('c'), tab('d')]);
  const left = run(s, { type: 'split-merge', id: 'a', withId: 'b' });
  const leftId = left.activeId;
  const right = run(left, { type: 'split-merge', id: 'c', withId: 'd' });
  const rightId = right.activeId;
  const all = run(right, { type: 'split-merge', id: leftId, withId: rightId });
  assert.equal(all.tabs.length, 1);
  assert.deepEqual(all.tabs[0].split!.panes.map(p => p.id), ['a', 'b', 'c', 'd']);
});

test('split-close-pane drops a pane, re-fits the layout, keeps focus on the same pane, and collapses to a plain tab', () => {
  let s = run(state([tab('a'), tab('b'), tab('c')]), { type: 'split-merge', id: 'a', withId: 'b' });
  const id = s.activeId;
  s = run(s, { type: 'split-merge', id, withId: 'c' }); // focus 2, 2x2 with three panes
  const closedFirst = run(s, { type: 'split-close-pane', id, paneId: 'a' });
  assert.deepEqual(only(closedFirst, id).split!.panes.map(p => p.id), ['b', 'c']);
  assert.equal(only(closedFirst, id).split!.layout, '1x2');
  assert.equal(only(closedFirst, id).split!.focus, 1);
  assert.equal(closedFirst.closed[closedFirst.closed.length - 1].id, 'a');
  const plain = run(closedFirst, { type: 'split-close-pane', id, paneId: 'c' });
  assert.equal(plain.tabs.length, 1);
  assert.equal(plain.tabs[0].id, 'b');
  assert.equal(plain.tabs[0].split, undefined);
  assert.equal(plain.activeId, 'b');
  assert.equal(run(s, { type: 'split-close-pane', id, paneId: 'zzz' }), s);
  assert.equal(run(s, { type: 'split-close-pane', id: 'nope', paneId: 'a' }), s);
});

test('split-focus moves focus inside range only', () => {
  const s = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b' });
  const id = s.activeId;
  assert.equal(only(run(s, { type: 'split-focus', id, index: 0 }), id).split!.focus, 0);
  assert.equal(run(s, { type: 'split-focus', id, index: 5 }), s);
  assert.equal(run(s, { type: 'split-focus', id, index: -1 }), s);
  assert.equal(run(s, { type: 'split-focus', id, index: 1 }), s);
});

test('split-layout switches between the two-pane layouts and refuses an unfit one', () => {
  const s = run(state([tab('a'), tab('b')]), { type: 'split-merge', id: 'a', withId: 'b' });
  const id = s.activeId;
  assert.equal(only(run(s, { type: 'split-layout', id, layout: '2x1' }), id).split!.layout, '2x1');
  assert.equal(run(s, { type: 'split-layout', id, layout: '2x2' }), s);
});

test('split-unmerge restores one tab per pane in place and activates the focused one', () => {
  const s = run(state([tab('a', { groupId: 'g' }), tab('b'), tab('z')], { groups: [{ id: 'g', title: 'G', collapsed: false }] }), { type: 'split-merge', id: 'a', withId: 'b' });
  const id = s.activeId;
  const apart = run(s, { type: 'split-focus', id, index: 0 }, { type: 'split-unmerge', id });
  assert.deepEqual(ids(apart), ['a', 'b', 'z']);
  assert.equal(apart.activeId, 'a');
  assert.equal(only(apart, 'a').groupId, 'g');
  assert.equal(only(apart, 'b').split, undefined);
});

test('split-group opens a group as a split, up to four plain tabs', () => {
  const members = ['a', 'b', 'c', 'd', 'e'].map(i => tab(i, { groupId: 'g' }));
  const s = state([...members, tab('z')], { groups: [{ id: 'g', title: 'G', collapsed: false }] });
  const next = run(s, { type: 'split-group', groupId: 'g' });
  const merged = next.tabs.find(t => t.split)!;
  assert.deepEqual(merged.split!.panes.map(p => p.id), ['a', 'b', 'c', 'd']);
  assert.equal(merged.split!.layout, '2x2');
  assert.deepEqual(next.tabs.map(t => t.split ? 'split' : t.id), ['split', 'e', 'z']);
  assert.equal(run(state([tab('a', { groupId: 'g' })], { groups: [{ id: 'g', title: 'G', collapsed: false }] }), { type: 'split-group', groupId: 'g' }).tabs.length, 1);
});

test('a closed split tab reopens whole', () => {
  const s = run(state([tab('a'), tab('b'), tab('z')]), { type: 'split-merge', id: 'a', withId: 'b' });
  const id = s.activeId;
  const back = run(s, { type: 'close', id }, { type: 'reopen' });
  assert.equal(only(back, id).split!.panes.length, 2);
});

test('an unknown action leaves the state untouched', () => {
  const s = state([tab('a')]);
  assert.equal(workspaceReducer(s, { type: 'nope' } as unknown as WorkspaceAction), s);
});

// ---- persistence ---------------------------------------------------------

function withStorage(value: unknown, body: () => void) {
  const store = new Map<string, string>(value === undefined ? [] : [[storageKey, typeof value === 'string' ? value : JSON.stringify(value)]]);
  const g = globalThis as { localStorage?: unknown };
  const before = g.localStorage;
  g.localStorage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => { store.set(k, v); } };
  try { body(); } finally { g.localStorage = before; }
}

test('a v1 save (no kind, no split) loads with every tab a conversation', () => {
  withStorage({ tabs: [{ id: 'a', title: 'A', draft: 'x', pinned: false }, { id: 'b', title: 'B', draft: '', pinned: true, sessionFile: 's.jsonl' }], groups: [], closed: [], activeId: 'b', nextNumber: 3 }, () => {
    const s = readWorkspace();
    assert.deepEqual(s.tabs.map(t => t.kind), ['conversation', 'conversation']);
    assert.equal(s.activeId, 'b');
    // The pinned tab stands first, as the strip always drew it.
    assert.deepEqual(s.tabs.map(t => t.id), ['b', 'a']);
    assert.equal(only(s, 'b').sessionFile, 's.jsonl');
  });
});

test('saved kinds and splits round-trip; an unknown kind becomes a conversation', () => {
  const merged = run(state([tab('a', { kind: 'task' }), tab('b', { draft: 'keep' })]), { type: 'split-merge', id: 'a', withId: 'b' });
  withStorage(JSON.parse(JSON.stringify({ ...merged, tabs: [...merged.tabs, { id: 'w', title: 'W', draft: '', pinned: false, kind: 'hologram' }] })), () => {
    const s = readWorkspace();
    assert.equal(s.tabs[0].split!.panes[0].kind, 'task');
    assert.equal(s.tabs[0].split!.panes[1].draft, 'keep');
    assert.equal(s.tabs[0].split!.focus, 1);
    assert.equal(s.tabs[1].kind, 'conversation');
  });
});

test('an invalid split is dropped and the tab survives; invalid tabs fall back to a fresh workspace', () => {
  const broken = { id: 'a', title: 'A', draft: '', pinned: false, split: { layout: '1x2', focus: 0, panes: [{ id: 'p' }] } };
  withStorage({ tabs: [broken], groups: [], closed: [], activeId: 'a', nextNumber: 2 }, () => {
    const s = readWorkspace();
    assert.equal(s.tabs[0].id, 'a');
    assert.equal(s.tabs[0].split, undefined);
  });
  withStorage({ tabs: [{ id: 'bad', title: 'bad', draft: '', pinned: false }], groups: [], closed: [null], activeId: 'bad', nextNumber: -1 }, () => {
    assert.notEqual(readWorkspace().tabs[0].id, 'bad');
  });
  withStorage('{not json', () => assert.equal(readWorkspace().tabs.length, 1));
  withStorage(undefined, () => assert.equal(readWorkspace().tabs.length, 1));
});

test('duplicate ids across tabs and panes fall back to a fresh workspace', () => {
  const pane = (id: string) => ({ id, title: id, draft: '' });
  withStorage({ tabs: [{ id: 'a', title: 'A', draft: '', pinned: false }, { id: 's', title: 'S', draft: '', pinned: false, split: { layout: '1x2', focus: 0, panes: [pane('a'), pane('c')] } }], groups: [], closed: [], activeId: 'a', nextNumber: 3 }, () => {
    assert.equal(readWorkspace().tabs.length, 1);
    assert.notEqual(readWorkspace().tabs[0].id, 'a');
  });
});

test('initialWorkspace is one empty conversation', () => {
  const s = initialWorkspace();
  assert.equal(s.tabs.length, 1);
  assert.equal(s.tabs[0].kind, 'conversation');
  assert.equal(s.activeId, s.tabs[0].id);
});

test('a pane target survives a reload; unknown or non-string fields are dropped', () => {
  const tabs = [{ id: 'f', title: 'parse.go', kind: 'file', draft: '', pinned: false, target: { path: 'internal/parse.go', sessionId: 's1', extra: 1, url: 5 } }, { id: 'g', title: 'g', kind: 'file', draft: '', pinned: false, target: ['x'] }];
  withStorage({ tabs, groups: [], closed: [], activeId: 'f', nextNumber: 3, recentIds: ['f', 'g'] }, () => {
    const s = readWorkspace();
    assert.deepEqual(s.tabs[0].target, { sessionId: 's1', path: 'internal/parse.go' });
    assert.equal(s.tabs[1].target, undefined);
  });
});

test('cleanView keeps a valid web url and drops a javascript: url', () => {
  assert.deepEqual(cleanView({ web: { url: 'https://example.com/a' } }).web, { url: 'https://example.com/a' });
  assert.equal(cleanView({ web: { url: 'javascript:alert(1)' } }).web, undefined);
  assert.equal(cleanView({ web: { url: 'not a url' } }).web, undefined);
  assert.equal(cleanView({ web: { url: 'javascript:1' }, sessionFile: 's.jsonl' }).sessionFile, 's.jsonl');
});

test('cleanView keeps job.jobId', () => {
  assert.deepEqual(cleanView({ job: { jobId: 'j1' } }).job, { jobId: 'j1' });
  assert.equal(cleanView({ job: { jobId: '' } }).job, undefined);
});
