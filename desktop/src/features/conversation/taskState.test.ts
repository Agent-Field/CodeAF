// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import { isEnded, taskControls, taskKind, taskMark } from './taskState.ts';

test('state words follow the engine table', () => {
  const word = (status, flags) => taskMark(status, flags).label;
  assert.equal(word('pending'), 'Queued');
  assert.equal(word('ready'), 'Running');
  assert.equal(word('claimed'), 'Running');
  assert.equal(word('running'), 'Running');
  assert.equal(word('done'), 'Done');
  assert.equal(word('failed'), 'Incomplete');
  assert.equal(word('cancelled'), 'Incomplete');
  assert.equal(word('paused'), 'Your call');
  assert.equal(word('???'), 'Queued');
});

test('Stopped and Interrupted flags win over the status; Hold queues ready and running work', () => {
  assert.equal(taskMark('running', { stopped: true }).label, 'Stopped');
  assert.equal(taskMark('done', true).label, 'Stopped');
  assert.equal(taskMark('running', { interrupted: true }).label, 'Interrupted');
  assert.equal(taskKind('running', { hold: 'budget' }), 'queued');
  assert.equal(taskKind('ready', { hold: 'budget' }), 'queued');
  assert.equal(taskKind('done', { hold: 'budget' }), 'done');
});

test('a person-paused task gets the pause mark; your call gets the attention tone', () => {
  assert.equal(taskMark('running', { paused: true }).icon, 'pause');
  assert.equal(taskMark('paused').tone, 'attention');
  assert.equal(taskMark('paused').icon, 'alert');
});

test('what can be done to a task', () => {
  assert.deepEqual(taskControls('running', true), { pause: true, resume: false, stop: true });
  assert.deepEqual(taskControls('running', false), { pause: false, resume: false, stop: true });
  assert.deepEqual(taskControls('paused', true), { pause: false, resume: true, stop: true });
  assert.deepEqual(taskControls('done', true), { pause: false, resume: false, stop: false });
  assert.ok(isEnded('interrupted'));
  assert.ok(!isEnded('queued'));
});
