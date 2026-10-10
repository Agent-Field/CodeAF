import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  RECONNECTING_NOTICE,
  RECONNECT_WINDOW_MS,
  UNREACHABLE_NOTICE,
  backoffDelay,
  reconnectAdvance,
  reconnectDown,
  reconnectOnline,
  reconnectRetry,
  reconnectSettled,
  reconnectWake,
} from './reconnect.ts';

test('the two sentences are the design\'s, with one ellipsis character', () => {
  assert.equal(RECONNECTING_NOTICE, 'Reconnecting to the engine…');
  assert.equal(RECONNECTING_NOTICE.includes('...'), false);
  assert.equal(UNREACHABLE_NOTICE, "Can't reach the engine");
  assert.equal(RECONNECT_WINDOW_MS, 30_000);
});

test('backoff grows and then holds the last delay', () => {
  assert.deepEqual([0, 1, 2, 3, 4].map(backoffDelay), [1_000, 2_000, 5_000, 10_000, 10_000]);
});

test('a fake clock probes on the backoff and offers Retry at 30s, without sleeping', () => {
  const started = Date.now();
  let state = reconnectDown(0);
  let now = 0;
  const probes: number[] = [];
  while (state.phase === 'reconnecting') {
    const wake = reconnectWake(state);
    assert.ok(wake != null && wake > now);
    now = wake;
    const step = reconnectAdvance(state, now);
    state = step.state;
    if (step.attempt) probes.push(now);
  }
  assert.equal(state.phase, 'unreachable');
  assert.equal(now, 30_000);
  assert.deepEqual(probes, [1_000, 3_000, 8_000, 18_000, 28_000]);
  assert.equal(reconnectWake(state), null);
  assert.ok(Date.now() - started < 1_000, 'the clock is fake; this must not wait out 30s');
});

test('an early reading does not probe, and a jump past 30s does not burst', () => {
  const early = reconnectAdvance(reconnectDown(0), 999);
  assert.equal(early.attempt, false);
  assert.equal(early.state.phase, 'reconnecting');
  assert.equal(early.state.attempts, 0);

  const jumped = reconnectAdvance(reconnectDown(0), 60_000);
  assert.equal(jumped.attempt, false);
  assert.equal(jumped.state.phase, 'unreachable');
  assert.equal(jumped.state.attempts, 0);
});

test('Retry asks once, a miss brings the button back, and an answer clears the line', () => {
  const givenUp = reconnectAdvance(reconnectDown(0), 30_000).state;
  assert.equal(reconnectRetry(reconnectDown(0)).attempt, false);

  const asked = reconnectRetry(givenUp);
  assert.equal(asked.attempt, true);
  assert.equal(asked.state.phase, 'probing');
  assert.equal(reconnectWake(asked.state), null);

  const missed = reconnectSettled(asked.state, false);
  assert.equal(missed.phase, 'unreachable');
  assert.equal(reconnectRetry(missed).attempt, true);

  assert.equal(reconnectSettled(asked.state, true).phase, 'online');
  assert.equal(reconnectAdvance(reconnectOnline(), 5_000).attempt, false);
});
