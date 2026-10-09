// The strip's integrity laws (tabs audit findings 3, 4 and 5): one order for the strip, the keys and the overview;
// groups that stand together; Reopen that puts a tab back where it stood; a closed split that comes back whole.
import test from 'node:test';
import assert from 'node:assert/strict';
import { isArranged, setIdSource, stripItems } from '../helpers.ts';
import { readWorkspace, storageKey, visibleTabs, workspaceReducer, type Tab, type TabGroup, type WorkspaceAction, type WorkspaceState } from '../model.ts';

let counter = 0;
setIdSource(() => `g${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const pane = (id: string, over: Partial<Tab> = {}) => ({ id, kind: 'conversation' as const, title: id, draft: '', ...over });
const group = (id: string, title = id): TabGroup => ({ id, title, collapsed: false });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);
const ids = (s: WorkspaceState) => s.tabs.map(t => t.id);
const groupOf = (s: WorkspaceState, id: string) => s.tabs.find(t => t.id === id)?.groupId;
/** The strip as a person reads it: pinned ids, then a loose id or "[group: members]". */
const strip = (s: WorkspaceState) => [...s.tabs.filter(t => t.pinned).map(t => t.id), ...stripItems(s).map(item => (item.kind === 'tab' ? item.tab.id : `[${item.group.id}: ${item.members.map(m => m.id).join(' ')}]`))];

/** Every law `arrange` promises, checked on any state. */
function assertLaws(s: WorkspaceState, label: string) {
  assert.ok(isArranged(s.tabs), `${label}: pinned first and every group one run: ${ids(s).join(' ')} / ${s.tabs.map(t => t.groupId ?? '-').join(' ')}`);
  assert.ok(s.tabs.every(t => !t.pinned || !t.groupId), `${label}: a pinned tab is in a group`);
  const runs = [...new Set(s.tabs.flatMap(t => (t.groupId ? [t.groupId] : [])))];
  assert.deepEqual(s.groups.map(g => g.id), runs, `${label}: groups are exactly the runs, in strip order`);
  const visible = visibleTabs(s).map(t => t.id);
  assert.deepEqual(visible, ids(s).filter(id => visible.includes(id)), `${label}: the keys' order is the strip's order`);
  assert.ok(s.tabs.some(t => t.id === s.activeId), `${label}: the active tab is open`);
  assert.equal(new Set(s.tabs.flatMap(t => [t.id, ...(t.split?.panes.map(p => p.id) ?? [])])).size, s.tabs.reduce((n, t) => n + 1 + (t.split?.panes.length ?? 0), 0), `${label}: ids are unique`);
}

// ---- finding 5: one order, groups that stand together --------------------------------------------------

test('a group can stand before loose tabs, and the strip, the keys and the groups all read state.tabs (design 2b)', () => {
  const s = run(state([tab('a'), tab('b'), tab('c')]), { type: 'group', id: 'a' });
  assert.deepEqual(strip(s), ['[g1: a]', 'b', 'c']);
  assert.deepEqual(visibleTabs(s).map(t => t.id), ['a', 'b', 'c']);
  assertLaws(s, 'group');
});

test('a new tab opens at the end of the strip, after every group; a new tab in a group opens at the end of that group', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b')], { groups: [group('g')] });
  const fresh = run(s, { type: 'new' });
  assert.equal(ids(fresh).at(-1), fresh.activeId);
  const inGroup = run(s, { type: 'new', groupId: 'g' });
  assert.deepEqual(strip(inGroup), [`[g: a ${inGroup.activeId}]`, 'b']);
  assertLaws(inGroup, 'new in group');
});

test('dropping a loose tab after a group\'s last member puts it beside the group, not in it', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('c'), tab('d')], { groups: [group('g')] });
  const next = run(s, { type: 'reorder', id: 'd', targetId: 'b', after: true });
  assert.deepEqual(strip(next), ['[g: a b]', 'd', 'c']);
  const before = run(s, { type: 'reorder', id: 'd', targetId: 'a' });
  assert.deepEqual(strip(before), ['d', '[g: a b]', 'c']);
  assertLaws(next, 'after edge');
});

test('dropping a tab between two members joins that group; a member dropped at its own group\'s edge stays in it', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('x')], { groups: [group('g')] });
  assert.deepEqual(strip(run(s, { type: 'reorder', id: 'x', targetId: 'b' })), ['[g: a x b c]']);
  assert.deepEqual(strip(run(s, { type: 'reorder', id: 'c', targetId: 'a' })), ['[g: c a b]', 'x']);
  assert.deepEqual(strip(run(s, { type: 'reorder', id: 'a', targetId: 'c', after: true })), ['[g: b c a]', 'x']);
  // A member dropped among loose tabs leaves its group.
  const out = run(state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('x'), tab('y')], { groups: [group('g')] }), { type: 'reorder', id: 'a', targetId: 'y' });
  assert.deepEqual(strip(out), ['[g: b]', 'x', 'a', 'y']);
});

test('the only member of a group carries its group when dragged, and a tab dropped on another group\'s member edge never splits that group', () => {
  const s = state([tab('x'), tab('a', { groupId: 'g' }), tab('b', { groupId: 'h' }), tab('c', { groupId: 'h' })], { groups: [group('g'), group('h')] });
  const moved = run(s, { type: 'reorder', id: 'a', targetId: 'x' });
  assert.deepEqual(strip(moved), ['[g: a]', 'x', '[h: b c]']);
  assertLaws(moved, 'group of one');
});

test('pinned tabs stay put: reordering never pins or unpins, and a pinned tab dropped among loose tabs stays among the pinned', () => {
  const s = state([tab('p', { pinned: true }), tab('q', { pinned: true }), tab('a'), tab('b')]);
  const pinnedOut = run(s, { type: 'reorder', id: 'p', targetId: 'b', after: true });
  assert.deepEqual(ids(pinnedOut), ['q', 'p', 'a', 'b']);
  assert.ok(pinnedOut.tabs.find(t => t.id === 'p')!.pinned);
  const looseIn = run(s, { type: 'reorder', id: 'b', targetId: 'p' });
  assert.deepEqual(ids(looseIn), ['p', 'q', 'b', 'a']);
  assert.equal(looseIn.tabs.find(t => t.id === 'b')!.pinned, false);
});

test('dragging a group label moves the whole group before or after a tab or another group, and never in among the pinned', () => {
  const s = state([tab('p', { pinned: true }), tab('l'), tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('c', { groupId: 'h' }), tab('d', { groupId: 'h' })], { groups: [group('g'), group('h')] });
  assert.deepEqual(strip(run(s, { type: 'reorder-group', id: 'g', targetId: 'l' })), ['p', '[g: a b]', 'l', '[h: c d]']);
  assert.deepEqual(strip(run(s, { type: 'reorder-group', id: 'g', targetId: 'c', after: true })), ['p', 'l', '[h: c d]', '[g: a b]']);
  assert.deepEqual(strip(run(s, { type: 'reorder-group', id: 'h', targetId: 'g' })), ['p', 'l', '[h: c d]', '[g: a b]']);
  assert.deepEqual(strip(run(s, { type: 'reorder-group', id: 'g', targetId: 'p' })), ['p', '[g: a b]', 'l', '[h: c d]']);
  const moved = run(s, { type: 'reorder-group', id: 'g', targetId: 'd', after: true });
  assert.deepEqual(moved.groups.map(g => g.id), ['h', 'g']);
  assert.equal(run(s, { type: 'reorder-group', id: 'g', targetId: 'a' }), s);
  assert.equal(run(s, { type: 'reorder-group', id: 'zzz', targetId: 'l' }), s);
});

test('joining a group goes to its end; leaving goes to just after the group, never into its middle', () => {
  const s = state([tab('x'), tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('y')], { groups: [group('g')] });
  assert.deepEqual(strip(run(s, { type: 'move-group', id: 'x', groupId: 'g' })), ['[g: a b c x]', 'y']);
  assert.deepEqual(strip(run(s, { type: 'move-group', id: 'a' })), ['x', '[g: b c]', 'a', 'y']);
  assert.deepEqual(strip(run(s, { type: 'move-group', id: 'b' })), ['x', '[g: a c]', 'b', 'y']);
});

test('an overview drop lands beside the card it was dropped on, in that card\'s section', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('d'), tab('e')], { groups: [group('g', 'Bench')] });
  // Right half of Alpha: join the group just after Alpha. Left half of Echo: leave, just before Echo.
  assert.deepEqual(strip(run(s, { type: 'move-group', id: 'd', groupId: 'g', beside: { id: 'a', after: true } })), ['[g: a d b]', 'e']);
  const joined = run(s, { type: 'move-group', id: 'd', groupId: 'g', beside: { id: 'a', after: true } });
  assert.deepEqual(strip(run(joined, { type: 'move-group', id: 'b', beside: { id: 'e' } })), ['[g: a d]', 'b', 'e']);
  // Left half of the first member, and a drop on empty section space (no beside) at the end.
  assert.deepEqual(strip(run(s, { type: 'move-group', id: 'd', groupId: 'g', beside: { id: 'a' } })), ['[g: d a b]', 'e']);
  assert.deepEqual(strip(run(s, { type: 'move-group', id: 'd', groupId: 'g' })), ['[g: a b d]', 'e']);
  // Pinned is not an anchor, and a drop onto the card's own place changes nothing.
  const pinned = state([tab('p', { pinned: true }), tab('a'), tab('b')]);
  assert.deepEqual(strip(run(pinned, { type: 'move-group', id: 'b', beside: { id: 'p' } })), strip(pinned));
  assert.equal(run(s, { type: 'move-group', id: 'a', groupId: 'g', beside: { id: 'a', after: true } }).tabs.map(t => t.id).join(' '), s.tabs.map(t => t.id).join(' '));
});

test('⌘-click picks tabs and ⌘G groups them with the active tab where the first of them stands; a plain select drops the picks', () => {
  const s = state([tab('a'), tab('b'), tab('c'), tab('d')], { activeId: 'b' });
  const picked = run(s, { type: 'pick', id: 'd' }, { type: 'pick', id: 'a' }, { type: 'pick', id: 'c' }, { type: 'pick', id: 'c' });
  assert.deepEqual(picked.picked, ['d', 'a']);
  const grouped = run(picked, { type: 'group-picked' });
  const made = grouped.groups[0].id;
  assert.deepEqual(strip(grouped), [`[${made}: a b d]`, 'c']);
  assert.deepEqual(grouped.picked, []);
  assert.equal(grouped.groups[0].title, 'New group');
  assert.deepEqual(run(picked, { type: 'select', id: 'c' }).picked, []);
  // The active tab cannot be picked away, and ⌘G with nothing picked groups the active tab alone.
  assert.equal(run(s, { type: 'pick', id: 'b' }), s);
  const alone = run(s, { type: 'group-picked' });
  assert.deepEqual(strip(alone), ['a', `[${alone.groups[0].id}: b]`, 'c', 'd']);
});

test('⌘G never puts the Inbox in a group', () => {
  const s = state([tab('inbox', { kind: 'inbox', pinned: true }), tab('a')], { activeId: 'a', picked: ['inbox'] });
  const grouped = run(s, { type: 'group-picked' });
  assert.equal(groupOf(grouped, 'inbox'), undefined);
  assert.ok(grouped.tabs.find(t => t.id === 'inbox')!.pinned);
});

test('a task opened from a grouped conversation joins its group, at the end', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('z')], { groups: [group('g')] });
  const opened = run(s, { type: 'open-task', tab: tab('t', { kind: 'task' }), background: true, from: 'a' });
  assert.deepEqual(strip(opened), ['[g: a b t]', 'z']);
  assert.equal(opened.activeId, 'a');
  // From a loose conversation or with no opener it is an ordinary open, at the end.
  assert.deepEqual(strip(run(s, { type: 'open-task', tab: tab('t', { kind: 'task' }), background: true, from: 'z' })), ['[g: a b]', 'z', 't']);
});

test('an emptied group name takes a default no other group has', () => {
  const s = state([tab('a', { groupId: 'g1' }), tab('b', { groupId: 'g2' })], { groups: [group('g1', 'New group'), group('g2', 'Docs')] });
  const renamed = run(s, { type: 'rename-group', id: 'g2', title: '  ' });
  assert.deepEqual(renamed.groups.map(g => g.title), ['New group', 'New group 2']);
});

test('a save from before the laws loads in the order its strip drew (pinned, loose, then each group)', () => {
  const store = new Map([[storageKey, JSON.stringify({ tabs: [tab('a', { groupId: 'g' }), tab('l'), tab('b', { groupId: 'g' }), tab('p', { pinned: true })], groups: [group('g')], closed: [], activeId: 'a', nextNumber: 5 })]]);
  const g = globalThis as { localStorage?: unknown };
  const before = g.localStorage;
  g.localStorage = { getItem: (k: string) => store.get(k) ?? null, setItem: () => undefined };
  try {
    const s = readWorkspace();
    assert.deepEqual(strip(s), ['p', 'l', '[g: a b]']);
    assertLaws(s, 'legacy');
  } finally { g.localStorage = before; }
});

// ---- finding 4: Reopen puts a tab back where it stood -------------------------------------------------

test('⌘⇧T puts a closed tab back between the same neighbours, not at the end', () => {
  const s = state([tab('a'), tab('b', { draft: 'kept' }), tab('c')], { activeId: 'b' });
  const back = run(s, { type: 'close', id: 'b' }, { type: 'reopen' });
  assert.deepEqual(ids(back), ['a', 'b', 'c']);
  assert.equal(back.activeId, 'b');
  assert.equal(back.tabs[1].draft, 'kept');
  assert.equal((back.tabs[1] as { stood?: unknown }).stood, undefined, 'the record of where it stood does not stay on the open tab');
});

test('a closed group member comes back in its group, and a group that closing emptied comes back with its name and place', () => {
  const s = state([tab('a'), tab('b', { groupId: 'solo' }), tab('c')], { groups: [group('solo', 'Solo')], activeId: 'b' });
  const closed = run(s, { type: 'close', id: 'b' });
  assert.deepEqual(closed.groups, []);
  const back = run(closed, { type: 'reopen' });
  assert.deepEqual(strip(back), ['a', '[solo: b]', 'c']);
  assert.equal(back.groups[0].title, 'Solo');
  const member = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('d')], { groups: [group('g')] });
  assert.deepEqual(strip(run(member, { type: 'close', id: 'b' }, { type: 'reopen' })), ['[g: a b c]', 'd']);
});

test('when its neighbour is gone, Reopen falls back to the tab before it, then to the end; a collapsed group opens', () => {
  const s = state([tab('a'), tab('b'), tab('c'), tab('d')], { activeId: 'a' });
  const gone = run(s, { type: 'close', id: 'b' }, { type: 'close', id: 'c' });
  // c's neighbours were a and d; b's were a and c. Reopening b while c is still closed: after a.
  assert.deepEqual(ids(run(gone, { type: 'reopen-id', id: 'b' })), ['a', 'b', 'd']);
  assert.deepEqual(ids(run(gone, { type: 'reopen' }, { type: 'reopen' })), ['a', 'b', 'c', 'd']);
  const collapsed = state([tab('a', { groupId: 'g' }), tab('b', { groupId: 'g' }), tab('x')], { groups: [{ id: 'g', title: 'G', collapsed: true }], activeId: 'b' });
  const back = run(collapsed, { type: 'close', id: 'b' }, { type: 'select', id: 'x' }, { type: 'reopen' });
  assert.equal(back.groups[0].collapsed, false);
});

test('reopen-id uses the place a caller remembered over the recorded one (the closing toast\'s Undo)', () => {
  const s = state([tab('a'), tab('c')], { closed: [{ ...tab('b'), stood: { before: 'c' } }] });
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b' })), ['a', 'b', 'c']);
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b', after: 'c' })), ['a', 'b', 'c']);
  assert.deepEqual(ids(run(s, { type: 'reopen-id', id: 'b', before: 'a' })), ['b', 'a', 'c']);
  assert.equal(run(s, { type: 'reopen-id', id: 'nope' }), s);
});

test('a reopened loose tab never lands inside a group that has grown around its old place', () => {
  const s = state([tab('a', { groupId: 'g' }), tab('b'), tab('c', { groupId: 'g' })].map(t => t), { groups: [group('g')], activeId: 'b' });
  const closed = run(s, { type: 'close', id: 'b' });
  const back = run(closed, { type: 'reopen' });
  assert.deepEqual(strip(back), ['[g: a c]', 'b']);
  assertLaws(back, 'grown');
});

test('a pane closed out of a split reopens just after the split, in its group', () => {
  const split = { ...tab('s', { groupId: 'g' }), split: { layout: '1x2' as const, focus: 0, panes: [pane('p1'), pane('p2', { draft: 'p2 words' })] } };
  const s = state([split, tab('x')], { groups: [group('g')] });
  const back = run(s, { type: 'split-close-pane', id: 's', paneId: 'p2' }, { type: 'reopen' });
  assert.deepEqual(strip(back), ['[g: p1 p2]', 'x']);
  assert.equal(back.tabs.find(t => t.id === 'p2')!.draft, 'p2 words');
});

test('the recorded place survives a reload', () => {
  const s = run(state([tab('a'), tab('b', { groupId: 'g' }), tab('c')], { groups: [group('g', 'Solo')] }), { type: 'close', id: 'b' });
  const store = new Map([[storageKey, JSON.stringify(s)]]);
  const g = globalThis as { localStorage?: unknown };
  const before = g.localStorage;
  g.localStorage = { getItem: (k: string) => store.get(k) ?? null, setItem: () => undefined };
  try {
    const loaded = readWorkspace();
    assert.deepEqual(loaded.closed[0].stood, { before: 'c', after: 'a', group: group('g', 'Solo') });
    assert.deepEqual(strip(run(loaded, { type: 'reopen' })), ['a', '[g: b]', 'c']);
  } finally { g.localStorage = before; }
});

// ---- finding 3: the new-tab field reopens a closed split whole ---------------------------------------

const closedSplit = (): Tab => ({
  ...tab('sp', { title: 'Split', groupId: 'g' }),
  split: { layout: '2x2', focus: 2, ratios: { col: 0.4, row: 0.6 }, panes: [pane('p1', { draft: 'lexer draft', sessionFile: '/s/1.jsonl' }), pane('p2', { draft: 'parser draft', sessionFile: '/s/2.jsonl', kind: 'terminal' }), pane('p3', { sessionFile: '/s/3.jsonl' })] },
});

test('reopening a closed split from the field brings back every pane with its draft, session, id, layout and focused pane', () => {
  const s = run(state([tab('o'), closedSplit(), tab('z')], { groups: [group('g', 'Parse')], activeId: 'sp' }), { type: 'close', id: 'sp' }, { type: 'new' });
  const field = s.activeId;
  const back = run(s, { type: 'newtab-reopen', id: field, closedId: 'sp' });
  const split = back.tabs.find(t => t.id === 'sp')!.split!;
  assert.deepEqual(split.panes.map(p => [p.id, p.draft, p.sessionFile, p.kind]), [['p1', 'lexer draft', '/s/1.jsonl', 'conversation'], ['p2', 'parser draft', '/s/2.jsonl', 'terminal'], ['p3', '', '/s/3.jsonl', 'conversation']]);
  assert.deepEqual([split.layout, split.focus, split.ratios], ['2x2', 2, { col: 0.4, row: 0.6 }]);
  // Where it stood and in its group (made again: closing emptied it), and the field is gone, not closed.
  assert.deepEqual(strip(back), ['o', '[g: sp]', 'z']);
  assert.equal(back.groups[0].title, 'Parse');
  assert.equal(back.activeId, 'sp');
  assert.ok(!back.tabs.some(t => t.id === field) && !back.closed.some(t => t.id === field));
  assert.deepEqual(back.closed, []);
  assertLaws(back, 'field reopen');
});

test('a field that is a pane of a split takes in a closed plain tab under that tab\'s id', () => {
  const host = { ...tab('h'), split: { layout: '1x2' as const, focus: 1, panes: [pane('a'), pane('f', { kind: 'newtab', title: 'New tab' })] } };
  const s = state([host], { closed: [{ ...tab('c', { draft: 'half', sessionFile: '/s/9.jsonl' }), stood: {} }] });
  const back = run(s, { type: 'newtab-reopen', id: 'f', closedId: 'c' });
  const split = back.tabs[0].split!;
  assert.deepEqual(split.panes.map(p => [p.id, p.draft, p.sessionFile]), [['a', '', undefined], ['c', 'half', '/s/9.jsonl']]);
  assert.equal(split.focus, 1);
  assert.equal((split.panes[1] as { stood?: unknown }).stood, undefined);
  assert.deepEqual(back.closed, []);
});

test('a field pane cannot hold a closed split: the split reopens whole as its own tab and the field pane leaves', () => {
  const host = { ...tab('h'), split: { layout: '1x2' as const, focus: 1, panes: [pane('a'), pane('f', { kind: 'newtab', title: 'New tab' })] } };
  const s = state([host, tab('z')], { closed: [{ ...closedSplit(), groupId: undefined, stood: { before: 'z' } }] });
  const back = run(s, { type: 'newtab-reopen', id: 'f', closedId: 'sp' });
  assert.deepEqual(ids(back), ['a', 'sp', 'z']);
  assert.equal(back.tabs[1].split!.panes.length, 3);
  assert.equal(back.activeId, 'sp');
  assertLaws(back, 'split into split');
});

test('only a field reopens, and an unknown closed id changes nothing', () => {
  const s = state([tab('a')], { closed: [tab('c')] });
  assert.equal(run(s, { type: 'newtab-reopen', id: 'a', closedId: 'c' }), s);
  assert.equal(run(s, { type: 'newtab-reopen', id: 'zzz', closedId: 'c' }), s);
});

// ---- the laws hold whatever happens --------------------------------------------------------------------

test('the laws hold after every step of a long deterministic mix of actions', () => {
  let seed = 7;
  const random = (n: number) => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed % n; };
  let s = state([tab('t0'), tab('t1'), tab('t2'), tab('t3')]);
  for (let step = 0; step < 600; step++) {
    const open = s.tabs.map(t => t.id);
    const any = () => open[random(open.length)];
    const someGroup = () => s.groups[random(Math.max(s.groups.length, 1))]?.id ?? 'none';
    const actions: WorkspaceAction[] = [
      { type: 'new' }, { type: 'new', groupId: someGroup() }, { type: 'close', id: any() }, { type: 'reopen' },
      { type: 'pin', id: any() }, { type: 'reorder', id: any(), targetId: any(), after: random(2) === 1 },
      { type: 'group', id: any(), ids: [any()] }, { type: 'move-group', id: any(), groupId: random(3) ? someGroup() : undefined },
      { type: 'reorder-group', id: someGroup(), targetId: any(), after: random(2) === 1 }, { type: 'ungroup', id: someGroup() },
      { type: 'collapse-group', id: someGroup() }, { type: 'select', id: any() }, { type: 'pick', id: any() }, { type: 'group-picked' },
      { type: 'split-merge', id: any(), withId: any() }, { type: 'split-unmerge', id: any() },
      { type: 'open-task', tab: tab(`task${step}`, { kind: 'task' }), background: true, from: any() },
    ];
    const action = actions[random(actions.length)];
    s = run(s, action);
    assertLaws(s, `step ${step} ${action.type}`);
  }
});
