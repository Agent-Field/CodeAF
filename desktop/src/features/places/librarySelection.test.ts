// Places are libraries, not workspaces (owner decision 2026-10-11): selecting a place never replaces the tab set.
// These are structural tests. They read the sources that handle a place selection and fail if any of them could swap
// the strip, and they run every selection through the real reducer to prove no tab is lost or moved.
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import { setIdSource } from '../tabs/helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../tabs/model.ts';

const root = new URL('../../', import.meta.url).pathname;
const read = (file: string) => readFileSync(join(root, file), 'utf8');
const walk = (dir: string): string[] => readdirSync(join(root, dir)).flatMap(name => {
  const path = join(dir, name);
  return statSync(join(root, path)).isDirectory() ? walk(path) : [path];
});
const sources = (dir: string) => walk(dir).filter(file => /\.tsx?$/.test(file) && !/\.test\./.test(file));

test('the workspace is mounted once, never keyed or addressed by a place', () => {
  const app = read('App.tsx');
  assert.doesNotMatch(app, /<Workspace[^>]*\bkey=/);
  assert.doesNotMatch(app, /<Workspace[^>]*\bplace=/);
  const workspace = read('features/tabs/Workspace.tsx');
  assert.match(workspace, /const stripKey: WorkspaceKey = 'now'/);
  assert.doesNotMatch(workspace, /useWorkspaceSync\(\{ key: place/);
  assert.doesNotMatch(workspace, /home-ensure/);
});

test('no source handling a place selection can replace or adopt a tab set', () => {
  const files = [...sources('features/places'), ...sources('features/shell'), 'features/tabs/Workspace.tsx', 'features/tabs/reducers/home.ts'];
  for (const file of files) {
    const text = read(file);
    assert.doesNotMatch(text, /home-ensure|type: 'adopt'|'replace-tabs'|mergeWindowLocal/, file);
    assert.doesNotMatch(text, /workspaceKey\((?!stripKey)[a-z]*[pP]lace/, file);
  }
});

test('the per-place swap machinery is gone', () => {
  for (const gone of ['features/tabs/windowLocal.ts', 'features/tabs/workspaceSync.ts']) assert.throws(() => read(gone), gone);
});

let counter = 0;
setIdSource(() => `lib${++counter}`);
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: `draft ${id}`, pinned: false, ...over });
const strip = (): WorkspaceState => ({ tabs: [tab('a'), tab('b', { memberOf: ['pl_00000000000000aa'] }), tab('c')], groups: [], activeId: 'a', closed: [], nextNumber: 4, recentIds: ['a', 'b', 'c'] });

test('every place-selection action keeps every open tab, its order and its draft', () => {
  const selections: WorkspaceAction[] = [
    { type: 'home-open', place: 'pl_00000000000000aa', title: 'A' },
    { type: 'home-open', place: 'pl_00000000000000bb', title: 'B', background: true },
    { type: 'home-open', place: 'root', title: 'All places' },
    { type: 'home-root', fresh: true },
    { type: 'tab-place', id: 'c', place: 'pl_00000000000000aa' },
  ];
  let state = strip();
  for (const action of selections) {
    const next = workspaceReducer(state, action);
    const kept = next.tabs.filter(t => state.tabs.some(old => old.id === t.id));
    assert.deepEqual(kept.map(t => t.id), state.tabs.map(t => t.id), action.type);
    assert.deepEqual(kept.map(t => t.draft), state.tabs.map(t => t.draft), action.type);
    state = next;
  }
});
