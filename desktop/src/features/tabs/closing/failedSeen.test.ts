import test from 'node:test';
import assert from 'node:assert/strict';
import { failedSeenKey, hasUnseenFailure, markFailedSeen, readFailedSeen } from './failedSeen.ts';

const store = (value: string | null) => ({ getItem: (key: string) => (key === failedSeenKey ? value : null) });

test('a saved record that does not validate reads as nothing seen', () => {
  for (const bad of [null, 'nope', '[]', '"x"', '{"a":-1,"b":1.5,"c":"2","":3}']) assert.deepEqual(readFailedSeen(store(bad)), {});
  assert.deepEqual(readFailedSeen(store('{"a":2,"b":0}')), { a: 2 });
});

test('seeing failures raises the mark and never lowers it; a new failure is unseen again', () => {
  let seen = markFailedSeen({}, 'chat', 2);
  assert.equal(hasUnseenFailure(seen, 'chat', 2), false);
  assert.equal(hasUnseenFailure(seen, 'chat', 3), true);
  assert.equal(markFailedSeen(seen, 'chat', 1), seen);
  seen = markFailedSeen(seen, 'chat', 3);
  assert.equal(seen.chat, 3);
  assert.equal(markFailedSeen(seen, '', 5), seen);
});

test('the record is bounded', () => {
  let seen = {};
  for (let i = 0; i < 260; i++) seen = markFailedSeen(seen, `c${i}`, 1);
  assert.equal(Object.keys(seen).length, 200);
  assert.equal('c259' in seen, true);
  assert.equal('c0' in seen, false);
});

test('the engine decides when it sends a count; a pending mark hides only the failure it names', async () => {
  const { rowHasUnseenFailure } = await import('./failedSeen.ts');
  const failure = { task: '2', at: '2026-10-02T10:00:00.25Z' };
  const row = { session: 'c', failed: 3, unseenFailed: 1, failure };
  assert.equal(rowHasUnseenFailure(row, { c: 99 }), true, 'a local record never overrides the engine');
  assert.equal(rowHasUnseenFailure(row, {}, { c: failure.at }), false);
  assert.equal(rowHasUnseenFailure(row, {}, { c: '2026-10-01T10:00:00Z' }), true, 'an older pending mark does not hide a newer failure');
  assert.equal(rowHasUnseenFailure({ ...row, unseenFailed: 0 }, {}), false);
  assert.equal(rowHasUnseenFailure({ session: 'c', failed: 2 }, { c: 1 }), true, 'an engine without the mark falls back to the window-local count');
  assert.equal(rowHasUnseenFailure({ session: 'c', failed: 2 }, { c: 2 }), false);
});
