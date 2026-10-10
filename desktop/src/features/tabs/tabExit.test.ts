import test from 'node:test';
import assert from 'node:assert/strict';
import { departureIds, departuresFrom, exitHoldMs, groupsForDepartures, mergeDepartures, retainDepartures, withDepartures } from './tabExit.ts';
import type { Tab, TabGroup } from './types.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const group = (id: string): TabGroup => ({ id, title: id, collapsed: false });

test('a tab that left is remembered between the neighbours it had', () => {
  const before = [tab('a'), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('d')];
  const gone = departuresFrom(before, [tab('a'), tab('c', { groupId: 'g' }), tab('d')], [group('g')]);
  assert.deepEqual(gone.map(item => [item.tab.id, item.index, item.group?.id]), [['b', 1, 'g']]);
  assert.deepEqual(withDepartures([tab('a'), tab('c', { groupId: 'g' }), tab('d')], gone).map(item => item.id), ['a', 'b', 'c', 'd']);
});

test('two departures restore the old order, and a tab that came back is not drawn twice', () => {
  const before = [tab('a'), tab('b'), tab('c'), tab('d')];
  const gone = departuresFrom(before, [tab('a'), tab('c')], []);
  assert.deepEqual(withDepartures([tab('a'), tab('c')], gone).map(item => item.id), ['a', 'b', 'c', 'd']);
  const back = mergeDepartures(gone, [], new Set(['b']));
  assert.equal(departureIds(back), 'd');
  assert.deepEqual(withDepartures([tab('a'), tab('b'), tab('c')], back).map(item => item.id), ['a', 'b', 'c', 'd']);
});

test('reduced motion retains nothing, and an emptied group is kept only while its member is leaving', () => {
  const gone = departuresFrom([tab('a', { groupId: 'g' })], [], [group('g')]);
  assert.deepEqual(retainDepartures(true, gone), []);
  assert.equal(groupsForDepartures([], retainDepartures(false, gone))[0]?.id, 'g');
  assert.equal(groupsForDepartures([group('g')], gone).length, 1);
});

test('the hold reads dur-base as milliseconds and treats anything else as instant', () => {
  assert.equal(exitHoldMs('200ms'), 200);
  assert.equal(exitHoldMs('0.2s'), 200);
  assert.equal(exitHoldMs('0ms'), 0);
  assert.equal(exitHoldMs(''), 0);
});
