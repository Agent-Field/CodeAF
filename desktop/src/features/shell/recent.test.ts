import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { setIdSource } from '../tabs/helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../tabs/model.ts';
import { RECENT_LIMIT, readRecentCollapsed, recentAge, recentItems, writeRecentCollapsed } from './recent.ts';

let counter = 0;
setIdSource(() => `r${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: `Chat ${id}`, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id), ...over });
const run = (s: WorkspaceState, ...actions: WorkspaceAction[]) => actions.reduce(workspaceReducer, s);
const describe = () => ({ icon: 'tab' as const });

test('three closed tabs list newest first with their own titles', () => {
  const closed = run(state([tab('a'), tab('b'), tab('c'), tab('d')], { activeId: 'd' }), { type: 'close', id: 'a' }, { type: 'close', id: 'b' }, { type: 'close', id: 'c' });
  assert.deepEqual(recentItems(closed.closed, closed.tabs, describe).map(i => i.title), ['Chat c', 'Chat b', 'Chat a']);
});

test('only five show, and a conversation that is open again or closed twice is listed once', () => {
  const many = Array.from({ length: 8 }, (_, i) => tab(`t${i}`, { sessionFile: `/s/${i}` }));
  const closed = many.map(t => ({ ...t, closedAt: 1 }));
  assert.equal(recentItems(closed, [tab('open')], describe).length, RECENT_LIMIT);
  const twice = [{ ...tab('old', { sessionFile: '/s/x', title: 'older' }) }, { ...tab('new', { sessionFile: '/s/x', title: 'newer' }) }];
  assert.deepEqual(recentItems(twice, [], describe).map(i => i.title), ['newer']);
  assert.deepEqual(recentItems(twice, [tab('open', { sessionFile: '/s/x' })], describe), []);
});

test('a Home tab carries its place and a filed tab its first place', () => {
  const items = recentItems([tab('f', { memberOf: ['pl_1'] }), tab('h', { kind: 'home', place: 'pl_2' }), tab('r', { kind: 'home', place: 'root' })], [], describe);
  assert.deepEqual(items.map(i => i.placeId), [undefined, 'pl_2', 'pl_1']);
});

test('the rail row and cmd-shift-T are the same list: reopening one removes the other', () => {
  const closed = run(state([tab('a'), tab('b'), tab('c')], { activeId: 'c' }), { type: 'close', id: 'a' }, { type: 'close', id: 'b' });
  const reopened = run(closed, { type: 'reopen' });
  assert.deepEqual(recentItems(reopened.closed, reopened.tabs, describe).map(i => i.id), ['a']);
  const byRow = run(closed, { type: 'reopen-id', id: 'a', nextToActive: true });
  assert.deepEqual(byRow.closed.map(t => t.id), ['b']);
});

test('a row click opens the tab beside the active one with its draft and route, and a background open keeps the front', () => {
  const base = state([tab('a'), tab('b'), tab('c', { draft: 'half a thought', route: 'task/7' })], { activeId: 'a' });
  const closed = run(base, { type: 'select', id: 'c' }, { type: 'close', id: 'c' }, { type: 'select', id: 'a' });
  const front = run(closed, { type: 'reopen-id', id: 'c', nextToActive: true });
  assert.deepEqual(front.tabs.map(t => t.id), ['a', 'c', 'b']);
  assert.equal(front.activeId, 'c');
  assert.equal(front.tabs[1].draft, 'half a thought');
  assert.equal(front.tabs[1].route, 'task/7');
  const behind = run(closed, { type: 'reopen-id', id: 'c', nextToActive: true, background: true });
  assert.deepEqual(behind.tabs.map(t => t.id), ['a', 'c', 'b']);
  assert.equal(behind.activeId, 'a');
});

test('forget takes one tab, or all of them, off the list and never touches an open tab', () => {
  const closed = run(state([tab('a'), tab('b'), tab('c')], { activeId: 'c' }), { type: 'close', id: 'a' }, { type: 'close', id: 'b' });
  assert.deepEqual(run(closed, { type: 'forget-closed', id: 'a' }).closed.map(t => t.id), ['b']);
  const cleared = run(closed, { type: 'forget-closed' });
  assert.deepEqual(cleared.closed, []);
  assert.deepEqual(cleared.tabs.map(t => t.id), ['c']);
});

test('open in split puts the closed tab beside the front one as a pane', () => {
  const closed = run(state([tab('a'), tab('b')], { activeId: 'a' }), { type: 'close', id: 'b' });
  const split = run(closed, { type: 'reopen-split', id: 'b' });
  assert.equal(split.tabs.length, 1);
  assert.equal(split.tabs[0].split?.panes.length, 2);
  assert.deepEqual(split.closed, []);
});

test('an archived tab joins the list', () => {
  const archived = run(state([tab('a'), tab('b')], { activeId: 'a' }), { type: 'history-archive', ids: ['b'] });
  assert.deepEqual(recentItems(archived.closed, archived.tabs, describe).map(i => i.id), ['b']);
});

test('ages are short, and an unknown age is nothing', () => {
  assert.equal(recentAge(undefined, 1e6), undefined);
  assert.equal(recentAge(1e6, 1e6 + 5_000), 'now');
  assert.equal(recentAge(1e6, 1e6 + 5 * 60_000), '5m');
  assert.equal(recentAge(1e6, 1e6 + 3 * 3_600_000), '3h');
  assert.equal(recentAge(1e6, 1e6 + 2 * 86_400_000), '2d');
});

test('the fold is remembered per window and a damaged value reads as open', () => {
  const store = new Map<string, string>();
  const storage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v), removeItem: (k: string) => void store.delete(k) };
  writeRecentCollapsed('main', true, storage);
  assert.equal(readRecentCollapsed('main', storage), true);
  assert.equal(readRecentCollapsed('w-2', storage), false);
  store.set('codeaf.desktop.window.main.rail-recent-collapsed', '{"x":');
  assert.equal(readRecentCollapsed('main', storage), false);
});

test('there is one recent store: the rail keeps no list of its own and only asks the workspace', () => {
  const rail = readFileSync(new URL('../places/rail/RailRecent.tsx', import.meta.url), 'utf8');
  const store = readFileSync(new URL('./recent.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(rail, /localStorage|sessionStorage/);
  assert.doesNotMatch(store, /setItem\([^)]*items|JSON\.stringify\(items/);
  assert.match(rail, /type: 'reopen-id'/);
  assert.match(rail, /type: 'forget-closed'/);
});
