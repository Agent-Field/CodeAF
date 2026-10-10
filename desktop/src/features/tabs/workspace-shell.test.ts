import test from 'node:test';
import assert from 'node:assert/strict';
import { cleanView } from './view-state.ts';
import { initialWorkspace, parseWorkspace } from './model.ts';
import { emptyLocal, localOf, sharedOf } from '../workspace-sync/shared.ts';

test('single-scroller offsets accept finite nonnegative pixels only', () => {
  for (const scrollOffset of [0, 125.5]) assert.equal(cleanView({ scrollOffset }).scrollOffset, scrollOffset);
  for (const scrollOffset of [-1, Infinity, NaN, '12', null]) assert.equal(cleanView({ scrollOffset }).scrollOffset, undefined);
});

test('legacy offsets migrate to this window and never enter the shared tab set', () => {
  const state = initialWorkspace();
  state.tabs[0].scrollOffset = 124;
  state.tabs[0].split = { layout: '1x2', focus: 0, panes: [
    { id: 'left', kind: 'conversation', title: 'Left', draft: '', scrollOffset: 40 },
    { id: 'right', kind: 'conversation', title: 'Right', draft: '', scrollOffset: 80 },
  ] };
  state.closed = [{ id: 'closed', kind: 'conversation', title: 'Closed', draft: '', pinned: false, scrollOffset: 200, closedAt: 1234 }];
  const shared = sharedOf(state);
  assert.equal(JSON.stringify(shared).includes('scrollOffset'), false);
  assert.equal(shared.closed[0].closedAt, 1234);
  assert.deepEqual(localOf(state, emptyLocal()).scroll, { left: 40, right: 80, closed: 200 });
  assert.equal(state.tabs[0].split.panes[0].scrollOffset, 40);
  assert.equal(localOf(state, { ...emptyLocal(), scroll: { left: 400 } }).scroll.left, 400, 'a newer local position wins over the imported offset');
});

test('invalid workspace entries still reject the save', () => {
  const state = initialWorkspace();
  assert.equal(parseWorkspace(JSON.stringify({ ...state, tabs: [{ ...state.tabs[0], draft: null }] })), undefined);
  assert.equal(parseWorkspace('{'), undefined);
  const valid = parseWorkspace(JSON.stringify({ ...state, tabs: [{ ...state.tabs[0], scrollOffset: -10 }] }));
  assert.ok(valid);
  assert.equal(valid.tabs[0].scrollOffset, undefined);
});
