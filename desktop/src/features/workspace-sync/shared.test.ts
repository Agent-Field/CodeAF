import test from 'node:test';
import assert from 'node:assert/strict';
import { compose, localOf, parseShared, sharedOf, emptyLocal } from './shared.ts';
import { freshWorkspace, workspaceReducer } from '../tabs/model.ts';

test('canonical round trip keeps a closed tab placement and emptied group for Reopen', () => {
  let state = freshWorkspace();
  const id = state.tabs[0].id;
  state = workspaceReducer(state, { type: 'group', id, title: 'Saved group' });
  state = workspaceReducer(state, { type: 'close', id });
  const doc = parseShared(sharedOf(state))!;
  assert.deepEqual(doc.closed[0].stood, state.closed[0].stood);
  assert.equal(typeof state.closed[0].closedAt, 'number');
  assert.equal(doc.closed[0].closedAt, state.closed[0].closedAt);
  assert.equal(compose(doc, emptyLocal()).closed[0].closedAt, state.closed[0].closedAt);
  const reopened = workspaceReducer(compose(doc, emptyLocal()), { type: 'reopen' });
  assert.equal(reopened.tabs.find(tab => tab.id === id)?.groupId, reopened.groups[0].id);
  assert.equal(reopened.groups[0].title, 'Saved group');
});

test('a shared closed tab keeps a real close time and drops one that is not', () => {
  const open = freshWorkspace().tabs[0];
  const doc = parseShared({
    schema: 1, tabs: [open], groups: [], nextNumber: 3,
    closed: [
      { ...open, id: 'c', title: 'Closed', closedAt: 1_700_000_000_000 },
      { ...open, id: 'd', title: 'Old', closedAt: 'yesterday' },
    ],
  });
  assert.equal(doc!.closed.find(tab => tab.id === 'c')!.closedAt, 1_700_000_000_000);
  assert.equal(doc!.closed.find(tab => tab.id === 'd')!.closedAt, undefined);
});

test('Home retains its place reference through the canonical reader', () => {
  const place = 'pl_0123456789abcdef';
  const doc = parseShared(sharedOf(freshWorkspace({ id: place, title: 'Work' })))!;
  assert.equal(doc.tabs[0].kind, 'home');
  assert.equal(doc.tabs[0].place, place);
  assert.equal(localOf(compose(doc, emptyLocal()), emptyLocal()).activeId, doc.tabs[0].id);
});
