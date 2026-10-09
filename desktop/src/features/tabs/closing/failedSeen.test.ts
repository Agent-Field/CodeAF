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
