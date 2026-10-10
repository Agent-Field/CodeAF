import test from 'node:test';
import assert from 'node:assert/strict';
import { setIdSource } from '../tabs/helpers.ts';
import { initialWorkspace, workspaceReducer, type Tab, type WorkspaceState } from '../tabs/model.ts';
import { isShellKind, leaveKindAction, openKindAction } from './openKind.ts';

let counter = 0;
setIdSource(() => `id${++counter}`);

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ ...initialWorkspace(), tabs, activeId: tabs[0].id, recentIds: tabs.map(t => t.id), ...over });

test('the first request opens a Settings tab and focuses it; a second focuses the one that is open', () => {
  const first = workspaceReducer(state([tab('a')]), openKindAction(state([tab('a')]), 'settings'));
  const settings = first.tabs.filter(t => t.kind === 'settings');
  assert.equal(settings.length, 1);
  assert.equal(first.activeId, settings[0].id);
  const again = workspaceReducer({ ...first, activeId: 'a' }, openKindAction({ ...first, activeId: 'a' }, 'settings'));
  assert.equal(again.tabs.filter(t => t.kind === 'settings').length, 1);
  assert.equal(again.activeId, settings[0].id);
});

test('inbox is not a shell kind, so the rail cannot ask the workspace to open it', () => {
  assert.equal(isShellKind('inbox'), false);
  assert.equal(isShellKind('settings'), true);
  assert.equal(isShellKind('history'), true);
});

test('leaving Settings goes to the most recent other tab, or opens a new one when there is none', () => {
  const s = state([tab('a'), tab('s', { kind: 'settings' })], { activeId: 's', recentIds: ['s', 'a'] });
  assert.deepEqual(leaveKindAction(s, 'settings'), { type: 'select', id: 'a' });
  const alone = state([tab('s', { kind: 'settings' })], { recentIds: ['s'] });
  assert.deepEqual(leaveKindAction(alone, 'settings'), { type: 'new' });
});
