import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../helpers.ts';
import { inFocus, parseWorkspace, placesOf, workspaceKey, workspaceReducer, type Tab, type WorkspaceState } from '../model.ts';
import { landingFor, shownPlace } from '../focusModel.ts';
import { stampFocus } from '../focusStamp.ts';

let counter = 0;
setIdSource(() => `id${++counter}`);
const place = 'pl_00000000000000aa';
const other = 'pl_00000000000000bb';
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const strip = (): WorkspaceState => ({ tabs: [tab('a'), tab('b'), tab('c', { memberOf: [place] })], groups: [], activeId: 'b', closed: [], nextNumber: 4, recentIds: ['b', 'c', 'a'] });

test('every place shares the one saved tab set, under the v1 key', () => {
  assert.equal(workspaceKey('now'), 'codeaf.desktop.workspace.v1');
});

test('home-open adds the place’s Home as an ordinary tab right after the active one and leaves every other tab as it was', () => {
  const state = workspaceReducer(strip(), { type: 'home-open', place: other, title: 'Writing' });
  const opened = state.tabs.find(t => t.kind === 'home' && t.place === other)!;
  assert.equal(opened.pinned, false);
  assert.equal(state.activeId, opened.id);
  assert.deepEqual(state.tabs.filter(t => t.id !== opened.id).map(t => t.id), ['a', 'b', 'c']);
  assert.equal(state.tabs.indexOf(opened), state.tabs.findIndex(t => t.id === 'b') + 1);
  assert.equal(workspaceReducer(state, { type: 'close', id: opened.id }).tabs.some(t => t.id === opened.id), false);
});

test('home-open focuses a Home that is already open instead of duplicating it, and a background press never moves focus', () => {
  const once = workspaceReducer(strip(), { type: 'home-open', place: other, title: 'Writing', background: true });
  assert.equal(once.activeId, 'b');
  assert.equal(once.tabs.filter(t => t.place === other).length, 1);
  const again = workspaceReducer(once, { type: 'home-open', place: other, title: 'Writing' });
  assert.equal(again.tabs.length, once.tabs.length);
  assert.equal(again.activeId, once.tabs.find(t => t.place === other)!.id);
  assert.equal(workspaceReducer(again, { type: 'home-open', place: other, title: 'Writing', background: true }), again);
});

test('a Home belongs to its own place; any other tab belongs to its memberOf', () => {
  const home = tab('h', { kind: 'home', place });
  assert.deepEqual(placesOf(home), [place]);
  assert.deepEqual(placesOf(tab('x', { memberOf: [place, other] })), [place, other]);
  assert.deepEqual(placesOf(tab('y')), []);
});

test('dropping a tab on a place adds membership without moving or closing anything, and a tab may belong to several', () => {
  const state = strip();
  const added = workspaceReducer(state, { type: 'tab-place', id: 'a', place });
  assert.deepEqual(added.tabs.map(t => t.id), state.tabs.map(t => t.id));
  assert.deepEqual(added.tabs[0].memberOf, [place]);
  const twice = workspaceReducer(added, { type: 'tab-place', id: 'a', place: other });
  assert.deepEqual(twice.tabs[0].memberOf, [place, other]);
  assert.equal(workspaceReducer(twice, { type: 'tab-place', id: 'a', place }), twice);
  assert.equal(twice.activeId, state.activeId);
  assert.equal(workspaceReducer(state, { type: 'tab-place', id: 'missing', place }), state);
});

test('Focus filters the strip to the place and never closes or reorders a tab', () => {
  const state = workspaceReducer(strip(), { type: 'home-open', place, title: 'Reading' });
  assert.deepEqual(inFocus(state.tabs, place).map(t => t.id), ['c', state.tabs.find(t => t.kind === 'home')!.id].sort((x, y) => state.tabs.findIndex(t => t.id === x) - state.tabs.findIndex(t => t.id === y)));
  assert.equal(inFocus(state.tabs, undefined).length, state.tabs.length);
  assert.deepEqual(inFocus(state.tabs, other), []);
  // The tab in front stays drawn even when it is not in the place.
  assert.deepEqual(inFocus(state.tabs, other, 'a').map(t => t.id), ['a']);
  assert.equal(state.tabs.length, 4);
});

test('the place shown follows the tab in front: the focused place when the tab is in it, else its first place, else Now', () => {
  assert.equal(shownPlace(tab('a', { memberOf: [place, other] }), other), other);
  assert.equal(shownPlace(tab('a', { memberOf: [place, other] }), undefined), place);
  assert.equal(shownPlace(tab('a'), place), 'now');
  assert.equal(shownPlace(tab('r', { kind: 'home', place: 'root' }), undefined), 'now');
});

test('Focus lands on the focused place’s most recently used tab', () => {
  assert.equal(landingFor(strip(), place), 'c');
  assert.equal(landingFor(strip(), other), undefined);
});

test('a tab opened while Focus is on a place belongs to it; with no Focus the action is untouched', () => {
  const opened = stampFocus({ type: 'open', tab: tab('n'), background: false }, place);
  assert.deepEqual((opened as { tab: Tab }).tab.memberOf, [place]);
  const made = workspaceReducer(strip(), stampFocus({ type: 'new' }, place));
  assert.deepEqual(made.tabs[made.tabs.length - 1].memberOf, [place]);
  const action = { type: 'open', tab: tab('n'), background: false } as const;
  assert.equal(stampFocus(action, undefined), action);
  const already = tab('m', { kind: 'home', place });
  assert.equal((stampFocus({ type: 'open', tab: already, background: false }, place) as { tab: Tab }).tab, already);
});

test('a chat started from a Home opens right after the tab in front', () => {
  const state = strip();
  const next = workspaceReducer(state, { type: 'open', tab: tab('new', { memberOf: [place] }), background: false, at: 2 });
  assert.deepEqual(next.tabs.map(t => t.id), ['a', 'b', 'new', 'c']);
  assert.equal(next.activeId, 'new');
});

test('membership and a Home’s place survive a save and a read; an invalid place id is dropped', () => {
  const state = workspaceReducer(strip(), { type: 'home-open', place, title: 'Reading' });
  const read = parseWorkspace(JSON.stringify(state))!;
  assert.deepEqual(read.tabs.find(t => t.id === 'c')!.memberOf, [place]);
  assert.equal(read.tabs.find(t => t.kind === 'home')!.place, place);
  const bad = parseWorkspace(JSON.stringify({ ...state, tabs: state.tabs.map(t => (t.kind === 'home' ? { ...t, place: '../etc' } : { ...t, memberOf: [5, ''] })) }))!;
  assert.equal(bad.tabs.find(t => t.kind === 'home')!.place, undefined);
  assert.equal(bad.tabs[0].memberOf, undefined);
});

test('All places opens once per view, after the active tab, and is an ordinary closable tab', () => {
  const opened = workspaceReducer(strip(), { type: 'home-root' });
  const root = opened.tabs.find(t => t.place === 'root')!;
  assert.equal(opened.activeId, root.id);
  assert.equal(opened.tabs.indexOf(root), 2);
  assert.equal(workspaceReducer({ ...opened, activeId: 'a' }, { type: 'home-root' }).activeId, root.id);
  assert.equal(workspaceReducer(opened, { type: 'close', id: root.id }).tabs.some(t => t.place === 'root'), false);
  assert.equal(workspaceReducer(strip(), { type: 'home-root', background: true }).activeId, 'b');
});

test('the All places shortcut and background press open fresh closable root views', () => {
  const first = workspaceReducer(strip(), { type: 'home-root' });
  const fresh = workspaceReducer(first, { type: 'home-root', fresh: true });
  const roots = fresh.tabs.filter(t => t.place === 'root');
  assert.equal(roots.length, 2);
  assert.equal(fresh.activeId, roots[1].id);
  const behind = workspaceReducer(fresh, { type: 'home-root', background: true });
  assert.equal(behind.tabs.filter(t => t.place === 'root').length, 3);
  assert.equal(behind.activeId, fresh.activeId);
});

test('home-open for All places is the existing root Home', () => {
  const state = workspaceReducer(strip(), { type: 'home-open', place: 'root', title: 'All places' });
  assert.equal(state.tabs.filter(t => t.place === 'root').length, 1);
});
