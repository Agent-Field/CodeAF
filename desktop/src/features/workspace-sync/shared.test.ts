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
  const reopened = workspaceReducer(compose(doc, emptyLocal()), { type: 'reopen' });
  assert.equal(reopened.tabs.find(tab => tab.id === id)?.groupId, reopened.groups[0].id);
  assert.equal(reopened.groups[0].title, 'Saved group');
});

test('Home retains its place reference through the canonical reader', () => {
  const place = 'pl_0123456789abcdef';
  const doc = parseShared(sharedOf(freshWorkspace({ id: place, title: 'Work' })))!;
  assert.equal(doc.tabs[0].kind, 'home');
  assert.equal(doc.tabs[0].place, place);
  assert.equal(localOf(compose(doc, emptyLocal()), emptyLocal()).activeId, doc.tabs[0].id);
});
