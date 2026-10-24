import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULTS, limitsOf, Quota, RateLimit } from '../src/limits.js';
import { memorySql } from './sql.js';

const refusal = (fn) => {
  try {
    fn();
    return null;
  } catch (e) {
    return e;
  }
};

test('a rate limit admits its share per window, names the wait, and starts over with the next window', () => {
  const rate = new RateLimit(2, 60_000);
  rate.admit('k', 0);
  rate.admit('k', 1_000);
  const e = refusal(() => rate.admit('k', 20_000));
  assert.deepEqual([e.code, e.status, e.retryAfter], ['rate_limited', 429, 40]);
  rate.admit('other', 20_000); // one key's excess is not another's
  rate.admit('k', 60_000);
});

test('deployment numbers override the defaults', () => {
  assert.equal(limitsOf({}).framesPerDay, DEFAULTS.framesPerDay);
  assert.equal(limitsOf({ CAF_LIMITS: '{"framesPerDay":3}' }).framesPerDay, 3);
});

function quota(over = {}) {
  return new Quota(memorySql(), { ...DEFAULTS, ...over });
}
const frame = (size, objects = 1) => ({ size, objects });

test('quota refuses a frame past the byte ceiling with one clear code, and counts only what was recorded', () => {
  const q = quota({ storeBytes: 100 });
  q.admit(frame(60), 0);
  q.record(frame(60), 0);
  const e = refusal(() => q.admit(frame(60), 0));
  assert.deepEqual([e.code, e.status, e.limitBytes], ['full', 507, 100]);
  q.admit(frame(40), 0);
});

test('quota refuses past the object ceiling', () => {
  const q = quota({ storeObjects: 3 });
  q.record(frame(1, 2), 0);
  const e = refusal(() => q.admit(frame(1, 2), 0));
  assert.deepEqual([e.code, e.limitBytes], ['full', undefined], 'an object ceiling names no byte number');
});

test('frames a day is counted per day and forgiven at the next one', () => {
  const q = quota({ framesPerDay: 2 });
  q.record(frame(1), 0);
  q.record(frame(1), 1_000);
  assert.equal(refusal(() => q.admit(frame(1), 2_000)).code, 'full');
  q.admit(frame(1), 86_400_000);
});
