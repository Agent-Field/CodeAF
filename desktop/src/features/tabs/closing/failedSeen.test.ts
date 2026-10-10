import test from 'node:test';
import assert from 'node:assert/strict';
import { hasUnseenFailure } from './failedSeen.ts';

test('a failure is unseen until the record covers its count, and a newer failure is unseen again', () => {
  assert.equal(hasUnseenFailure({}, 'chat', 1), true);
  assert.equal(hasUnseenFailure({ chat: 2 }, 'chat', 2), false);
  assert.equal(hasUnseenFailure({ chat: 2 }, 'chat', 3), true);
});
