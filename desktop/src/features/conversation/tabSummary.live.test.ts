import assert from 'node:assert/strict';
import test from 'node:test';
import type { EngineSnapshot } from '../chat/engine-client.ts';
import { summarize } from './tabSummary.ts';

const snapshot = (tasks: EngineSnapshot['tasks']): EngineSnapshot => ({
  id: 's', sessionFile: 's.jsonl', workspace: '/w', model: 'm', persistent: true,
  running: true, needsPerson: false, entries: [], tasks,
  usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 },
  title: '', seq: 1,
});

test('summarize keeps a live step and command, and omits a task that sent neither', () => {
  const summary = summarize(snapshot([
    { ID: 't7', Title: 'Fixtures', Status: 'running', Live: { Step: 7, Command: 'go test ./internal/parse' } },
    { ID: 't8', Title: 'Quiet', Status: 'running' },
  ]));
  assert.equal(summary.running, 2);
  assert.deepEqual(summary.taskLive, { t7: { step: 7, command: 'go test ./internal/parse' } });
  assert.equal(summarize(snapshot([{ ID: 't8', Title: 'Quiet', Status: 'running' }])).taskLive, undefined);
});
