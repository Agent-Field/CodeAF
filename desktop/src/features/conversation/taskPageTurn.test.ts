// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineTaskPage } from '../chat/engine-client';
import { durationText, taskPageModel, isTaskRunning, turnStateOf } from './taskPageTurn.ts';

const base = (over: Partial<EngineTaskPage> = {}): EngineTaskPage => ({
  Row: { ID: 't1', Title: 'Fix', Status: 'done' },
  Description: 'Do **it**',
  ...over,
});

test('maps result first, steps, and keeps the brief as instructions', () => {
  const model = taskPageModel(base({ Steps: [{ step: 1, command: 'ls', observation: 'a' }], Result: 'All good' }));
  assert.equal(model.title, 'Fix');
  assert.equal(model.instructions, 'Do **it**');
  assert.equal(model.result, 'All good');
  assert.equal(model.worked?.steps.length, 1);
  assert.equal(model.worked?.id, 'steps:t1');
});

test('live step is appended as running and the detail counts steps', () => {
  const page = base({
    Row: { ID: 't1', Title: 'x', Status: 'running' },
    Steps: [{ step: 1, command: 'ls' }],
    Live: { Step: 2, Command: 'go test' },
  });
  const model = taskPageModel(page);
  assert.deepEqual(model.worked.steps.map((s) => s.state), ['done', 'running']);
  assert.equal(model.detail, '2 steps');
  assert.ok(isTaskRunning(page));
});

test('finished tasks report a duration only when the page carries both ends', () => {
  const timed = base({ Row: { ID: 'a', Title: '', Status: 'done', Started: '2026-01-01T10:00:00Z', Ended: '2026-01-01T10:01:04Z' } });
  assert.equal(taskPageModel(timed).detail, '1m 4s');
  assert.equal(taskPageModel(base()).detail, '');
  assert.equal(durationText('2026-01-01T10:00:00Z', '2026-01-01T10:00:12Z'), '12s');
  assert.equal(durationText('2026-01-01T10:00:00Z', '2026-01-01T11:05:00Z'), '1h 5m');
  assert.equal(durationText('nope', undefined), '');
});

test('empty page has nothing to show; failed and stopped states', () => {
  const empty = taskPageModel(base({ Description: undefined }));
  assert.equal(empty.worked, undefined);
  assert.equal(empty.result, '');
  assert.equal(empty.instructions, '');
  assert.equal(turnStateOf(base({ Row: { ID: 'a', Title: '', Status: 'failed' } }).Row), 'failed');
  const stopped = base({ Row: { ID: 'a', Title: '', Status: 'running', Stopped: true } });
  assert.equal(taskPageModel(stopped).stopped, true);
  assert.equal(isTaskRunning(stopped), false);
  assert.equal(isTaskRunning(base()), false);
});

test('children, checks and notes', () => {
  const model = taskPageModel(base({
    Children: [{ ID: 'c1', Title: 'Child', Status: 'done' }],
    Checks: ['tests pass'],
    Notes: [{ Body: 'hello' }, { Body: '' }],
  }));
  assert.equal(model.children[0].taskId, 'c1');
  assert.equal(model.children[0].summary, '');
  assert.deepEqual(model.checks, ['tests pass']);
  assert.deepEqual(model.notes, ['hello']);
});

test('refused steps read as failed; harness corrections are not steps', () => {
  const model = taskPageModel(
    base({
      Steps: [
        { step: 1, command: 'rm -rf /', refused: true, not_run: true },
        { step: 2, command: 'reply form', not_run: true },
        { step: 3, command: 'ls' },
      ],
    }),
  );
  assert.deepEqual(model.worked.steps.map((s) => s.state), ['failed', 'done']);
});

test('a note that repeats the result is not drawn again', () => {
  const model = taskPageModel(base({ Result: 'All good', Notes: [{ Body: 'done · ran 4s · All good' }, { Body: 'left the timeout alone' }] }));
  assert.deepEqual(model.notes, ['left the timeout alone']);
});

test('an unfinished task shows no result: it can only be a stale one', () => {
  const running = base({ Row: { ID: 'a', Title: '', Status: 'running' }, Result: 'old' });
  assert.equal(taskPageModel(running).result, '');
});
