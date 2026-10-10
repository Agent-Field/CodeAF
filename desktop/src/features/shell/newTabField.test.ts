import test from 'node:test';
import assert from 'node:assert/strict';
import { initialWorkspace, type Tab, type WorkspaceState } from '../tabs/model.ts';
import { newTabFieldAction } from './newTabField.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], activeId = tabs[0].id): WorkspaceState => ({ ...initialWorkspace(), tabs, activeId, recentIds: tabs.map(t => t.id) });

test('⌘K opens a New tab when none is empty', () => {
  assert.deepEqual(newTabFieldAction(state([tab('a')])), { type: 'new' });
});

test('⌘K shows the empty New tab that already exists instead of piling up another', () => {
  assert.deepEqual(newTabFieldAction(state([tab('a'), tab('n', { kind: 'newtab' })])), { type: 'select', id: 'n' });
});

test('a New tab with words typed in it is in use, so ⌘K opens a fresh one', () => {
  assert.deepEqual(newTabFieldAction(state([tab('n', { kind: 'newtab', draft: 'fix the lexer' })])), { type: 'new' });
});

test('when the active tab is already the empty field there is nothing to dispatch (the pane re-focuses itself)', () => {
  assert.equal(newTabFieldAction(state([tab('a'), tab('n', { kind: 'newtab' })], 'n')), undefined);
});
