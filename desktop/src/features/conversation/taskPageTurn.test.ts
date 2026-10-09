// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineTaskPage } from '../chat/engine-client';
import { taskPageModel, isTaskRunning } from './taskPageTurn.ts';

const base = (over: Partial<EngineTaskPage> = {}): EngineTaskPage => ({
  Row: { ID: 't1', Title: 'Fix', Status: 'done' },
  Description: 'Do **it**',
  ...over,
});

test('maps description, steps and result', () => {
  const model = taskPageModel(base({ Steps: [{ step: 1, command: 'ls', observation: 'a' }], Result: 'All good' }));
  assert.equal(model.turn.user, 'Do **it**');
  assert.equal(model.userFormat, 'markdown');
  assert.equal(model.turn.state, 'done');
  assert.deepEqual(model.turn.items.map((i) => i.kind), ['tools', 'text']);
});

test('live step is appended as running', () => {
  const page = base({
    Row: { ID: 't1', Title: 'x', Status: 'running' },
    Steps: [{ step: 1, command: 'ls' }],
    Live: { Step: 2, Command: 'go test' },
  });
  const model = taskPageModel(page);
  const tools = model.turn.items[0];
  assert.equal(tools.kind, 'tools');
  if (tools.kind === 'tools') assert.deepEqual(tools.steps.map((s) => s.state), ['done', 'running']);
  assert.equal(model.turn.state, 'working');
  assert.ok(isTaskRunning(page));
});

test('empty page has no items; failed and stopped states', () => {
  assert.equal(taskPageModel(base()).turn.items.length, 0);
  assert.equal(taskPageModel(base({ Row: { ID: 'a', Title: '', Status: 'failed' } })).turn.state, 'failed');
  const stopped = base({ Row: { ID: 'a', Title: '', Status: 'running', Stopped: true } });
  assert.equal(taskPageModel(stopped).turn.state, 'stopped');
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
