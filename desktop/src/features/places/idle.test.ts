import assert from 'node:assert/strict';
import test from 'node:test';
import { OPEN_IDLE_MS, autoCloses, isIdle, reopen } from './idle.ts';

const now = '2026-10-10T12:00:00.000Z';
const ago = (ms: number) => new Date(Date.parse(now) - ms).toISOString();
const MIN = 60_000;

test('an Open place is idle at exactly 12h and not at 11h59', () => {
  assert.equal(isIdle(ago(OPEN_IDLE_MS - MIN), now), false);
  assert.equal(isIdle(ago(OPEN_IDLE_MS), now), true);
  assert.equal(isIdle(ago(OPEN_IDLE_MS + MIN), now), true);
});

test('running or needs-you work holds an idle place open', () => {
  assert.equal(autoCloses(ago(OPEN_IDLE_MS * 3), now, true), false);
  assert.equal(autoCloses(ago(OPEN_IDLE_MS), now, false), true);
  assert.equal(autoCloses(ago(OPEN_IDLE_MS - MIN), now, false), false);
});

test('an unparseable instant never closes a place', () => {
  assert.equal(isIdle('', now), false);
  assert.equal(isIdle(ago(OPEN_IDLE_MS * 2), 'nope'), false);
});

test('reopening lands on Home; only a person-closed place restores its tabs', () => {
  assert.deepEqual(reopen('idle'), { land: 'home', restoreTabs: false });
  assert.deepEqual(reopen('person'), { land: 'home', restoreTabs: true });
});
