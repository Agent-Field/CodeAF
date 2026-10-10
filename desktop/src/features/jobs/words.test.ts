import assert from 'node:assert/strict';
import test from 'node:test';
import { jobTabMeta, jobTone, jobWords, toScreen } from './words.ts';

const now = Date.parse('2026-10-10T12:00:00Z');
const at = (msAgo: number) => new Date(now - msAgo).toISOString();

test('a running job reads Running with its clock; with no start it says the state alone', () => {
  assert.equal(jobWords({ id: 1, state: 'running', startedAt: at(134_000) }, now), 'Running · 2m 14s');
  assert.equal(jobWords({ id: 1, state: 'running' }, now), 'Running');
});

test('a finished job reads exit N and when it ended; a stopped one is not an exit', () => {
  assert.equal(jobWords({ id: 1, state: 'done', exitCode: 0, startedAt: at(240_000), elapsedMs: 120_000 }, now), 'exit 0 · 2m 0s ago');
  assert.equal(jobWords({ id: 1, state: 'failed', exitCode: 2 }, now), 'exit 2');
  assert.equal(jobWords({ id: 1, state: 'stopped' }, now), 'stopped');
  assert.equal(jobWords({ id: 1 }, now), '');
});

test('tone follows state, and only a job that ended itself gives the tab an exit', () => {
  assert.equal(jobTone({ id: 1, state: 'stopped' }), 'stopped');
  assert.deepEqual(jobTabMeta({ id: 1, state: 'done', exitCode: 0 }), { kind: 'job', state: 'exited', exitCode: 0 });
  assert.deepEqual(jobTabMeta({ id: 1, state: 'stopped' }), { kind: 'job', state: 'closed' });
  assert.equal(jobTabMeta(undefined), undefined);
});

test('bare newlines become carriage-return newlines for the screen', () => {
  assert.equal(toScreen('a\nb\r\nc'), 'a\r\nb\r\nc');
});
