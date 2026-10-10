import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../helpers.ts';
import { freshWorkspace, parseWorkspace, workspaceKey, workspaceReducer, type Tab, type WorkspaceState } from '../model.ts';
import { isPlaceHome } from './home.ts';

let counter = 0;
setIdSource(() => `id${++counter}`);
const place = 'pl_00000000000000aa';
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const withHome = (): WorkspaceState => workspaceReducer({ ...freshWorkspace(), tabs: [tab('a'), tab('b')], activeId: 'b', recentIds: ['b', 'a'] }, { type: 'home-ensure', place, title: 'Reading' });

test('each place has its own saved tab set; Now keeps the v1 key', () => {
  assert.equal(workspaceKey('now'), 'codeaf.desktop.workspace.v1');
  assert.equal(workspaceKey(place), `codeaf.desktop.workspace.v1:${place}`);
  const fresh = freshWorkspace({ id: place, title: 'Reading' });
  assert.equal(fresh.tabs.length, 1);
  assert.ok(isPlaceHome(fresh.tabs[0]) && fresh.tabs[0].pinned && fresh.tabs[0].title === 'Reading');
});

test('a place’s Home is pinned first, keeps focus where it was, and Go to lands on it', () => {
  const state = withHome();
  assert.equal(state.tabs[0].kind, 'home');
  assert.equal(state.tabs[0].place, place);
  assert.equal(state.activeId, 'b');
  const renamed = workspaceReducer(state, { type: 'home-ensure', place, title: 'Papers' });
  assert.equal(renamed.tabs[0].title, 'Papers');
  assert.equal(renamed.tabs.length, 3);
  const arrived = workspaceReducer(renamed, { type: 'home-ensure', place, title: 'Papers', focus: true });
  assert.equal(arrived.activeId, renamed.tabs[0].id);
  assert.equal(workspaceReducer(arrived, { type: 'home-ensure', place, title: 'Papers', focus: true }), arrived);
});

test('the Home never closes, unpins, moves, groups or joins a split', () => {
  const state = withHome();
  const home = state.tabs[0].id;
  for (const action of [{ type: 'close', id: home }, { type: 'pin', id: home }, { type: 'reorder', id: home, targetId: 'b', after: true }, { type: 'group', id: home }, { type: 'split-merge', id: 'a', withId: home }, { type: 'split-merge', id: home, withId: 'a' }] as const)
    assert.equal(workspaceReducer(state, action as never), state, action.type);
  // Nothing may be put before it.
  assert.equal(workspaceReducer(state, { type: 'reorder', id: 'b', targetId: home }), state);
});

test('All places opens once per strip, after the active tab, and is an ordinary closable tab', () => {
  const state = withHome();
  const opened = workspaceReducer(state, { type: 'home-root' });
  const root = opened.tabs.find(t => t.place === 'root')!;
  assert.equal(opened.activeId, root.id);
  assert.equal(opened.tabs.indexOf(root), 3);
  assert.equal(workspaceReducer({ ...opened, activeId: 'a' }, { type: 'home-root' }).activeId, root.id);
  assert.equal(workspaceReducer(opened, { type: 'close', id: root.id }).tabs.some(t => t.place === 'root'), false);
  const behind = workspaceReducer(state, { type: 'home-root', background: true });
  assert.equal(behind.activeId, 'b');
});

test('a chat started from Home opens right after it', () => {
  const state = withHome();
  const next = workspaceReducer(state, { type: 'open', tab: tab('new'), background: false, at: 1 });
  assert.deepEqual(next.tabs.map(t => t.id), [state.tabs[0].id, 'new', 'a', 'b']);
  assert.equal(next.activeId, 'new');
});

test('another window’s save is adopted, keeping this window’s own focus when that tab still exists', () => {
  const state = withHome();
  const theirs: WorkspaceState = { ...state, tabs: [...state.tabs, tab('c')], activeId: 'c' };
  const adopted = workspaceReducer(state, { type: 'adopt', state: parseWorkspace(JSON.stringify(theirs))! });
  assert.deepEqual(adopted.tabs.map(t => t.id), theirs.tabs.map(t => t.id));
  assert.equal(adopted.activeId, 'b');
  assert.equal(workspaceReducer(state, { type: 'adopt', state }), state);
  const gone = workspaceReducer(state, { type: 'adopt', state: { ...theirs, tabs: theirs.tabs.filter(t => t.id !== 'b') } });
  assert.equal(gone.activeId, 'c');
});

test('a saved Home tab keeps its place through a reload; an invalid place id is dropped', () => {
  const state = withHome();
  const read = parseWorkspace(JSON.stringify(state))!;
  assert.equal(read.tabs[0].place, place);
  const bad = parseWorkspace(JSON.stringify({ ...state, tabs: state.tabs.map((t, i) => (i === 0 ? { ...t, place: '../etc' } : t)) }))!;
  assert.equal(bad.tabs[0].place, undefined);
});


test('the All places shortcut and background press open fresh closable root views', () => {
  const first = workspaceReducer(withHome(), { type: 'home-root' });
  const fresh = workspaceReducer(first, { type: 'home-root', fresh: true });
  const roots = fresh.tabs.filter(tab => tab.place === 'root');
  assert.equal(roots.length, 2);
  assert.equal(fresh.activeId, roots[1].id);
  const behind = workspaceReducer(fresh, { type: 'home-root', background: true });
  assert.equal(behind.tabs.filter(tab => tab.place === 'root').length, 3);
  assert.equal(behind.activeId, fresh.activeId);
  const closed = workspaceReducer(behind, { type: 'close', id: roots[1].id });
  assert.equal(closed.tabs.filter(tab => tab.place === 'root').length, 2);
  assert.ok(isPlaceHome(closed.tabs[0]));
});
