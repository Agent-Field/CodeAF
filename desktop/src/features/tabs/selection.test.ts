import test from 'node:test';
import assert from 'node:assert/strict';
import { isSelectionPress, toggleSelection, retainSelection } from './selection.ts';
import { workspaceReducer, freshWorkspace } from './model.ts';
import { compose, emptyLocal, sharedOf } from '../workspace-sync/shared.ts';

const press = { button: 0, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false };
test('only Command-primary on Mac and Control-primary on Linux pick', () => {
  for (const mac of [true, false]) {
    const event = { ...press, [mac ? 'metaKey' : 'ctrlKey']: true };
    assert.equal(isSelectionPress(event, mac), true);
    assert.equal(isSelectionPress(event, !mac), false);
    for (const extra of [{ button: 1 }, { button: 2 }, { altKey: true }, { shiftKey: true }, { metaKey: true, ctrlKey: true }]) {
      assert.equal(isSelectionPress({ ...event, ...extra }, mac), false);
    }
    assert.equal(isSelectionPress(press, mac), false);
  }
});
test('toggle is immutable; pruning keeps only open tabs', () => {
  const selected = ['a'];
  assert.deepEqual(toggleSelection(selected, 'b'), ['a', 'b']);
  assert.deepEqual(toggleSelection(selected, 'a'), []);
  assert.deepEqual(selected, ['a']);
  assert.deepEqual(retainSelection(['a', 'b'], [{ id: 'b' }]), ['b']);
});
test('active and inactive tabs toggle; plain activation and Escape clearing keep focus honest', () => {
  let state = workspaceReducer(freshWorkspace(), { type: 'new' });
  const active = state.activeId;
  const other = state.tabs[0].id;
  state = workspaceReducer(state, { type: 'pick', id: active });
  state = workspaceReducer(state, { type: 'pick', id: other });
  assert.deepEqual(state.picked, [active, other]);
  assert.equal(state.activeId, active);
  state = workspaceReducer(state, { type: 'clear-picks' });
  assert.deepEqual(state.picked, []);
  assert.equal(state.activeId, active);
  state = workspaceReducer(state, { type: 'pick', id: other });
  state = workspaceReducer(state, { type: 'select', id: active });
  assert.deepEqual(state.picked, []);
});
test('selection survives remote updates in this window and never enters the shared document', () => {
  const state = freshWorkspace();
  const selected = { ...state, picked: [state.tabs[0].id] };
  const doc = sharedOf(selected);
  assert.equal('picked' in doc, false);
  assert.deepEqual(compose(doc, emptyLocal(), selected).picked, selected.picked);
  assert.equal(compose(doc, emptyLocal()).picked, undefined);
});
