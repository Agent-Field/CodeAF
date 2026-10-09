// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import { costPart, durationText, isTaskRunning, modelName, modelTier, taskPageModel } from './taskPageTurn.ts';
import { logSummary, tookText } from './tasks/logLines.ts';

const NOW = Date.parse('2026-10-09T10:05:00Z');

const base = (over = {}) => ({
  Row: { ID: 't1', Title: 'Fix', Status: 'done' },
  Description: 'Do **it**',
  ...over,
});

const running = (over = {}) =>
  base({ Row: { ID: 't1', Title: 'x', Status: 'running', Parent: 'p', Started: '2026-10-09T10:00:00Z', Model: 'deepseek/deepseek-v4.1-flash' }, ...over });

test('a finished task leads with its result and keeps the brief as instructions', () => {
  const model = taskPageModel(base({ Steps: [{ step: 1, command: 'ls', observation: 'a' }], Result: 'All good' }), NOW);
  assert.equal(model.title, 'Fix');
  assert.equal(model.instructions, 'Do **it**');
  assert.equal(model.result, 'All good');
  assert.equal(model.steps.length, 1);
  assert.equal(model.ended, true);
});

test('state line: only known parts, in order state step elapsed model', () => {
  const page = running({ Live: { Step: 7, Command: 'make build', Since: '2026-10-09T10:04:48Z' } });
  const model = taskPageModel(page, NOW);
  assert.equal(model.mark.label, 'Running');
  assert.deepEqual(model.stateParts, ['step 7', '5m 0s', 'Flash']);
  const bare = taskPageModel(base({ Row: { ID: 'a', Title: '', Status: 'running' } }), NOW);
  assert.deepEqual(bare.stateParts, []);
});

test('finished tasks report a duration only when the page carries both ends', () => {
  const timed = base({ Row: { ID: 'a', Title: '', Status: 'done', Started: '2026-01-01T10:00:00Z', Ended: '2026-01-01T10:01:04Z' } });
  assert.deepEqual(taskPageModel(timed, NOW).stateParts, ['1m 4s']);
  assert.deepEqual(taskPageModel(base(), NOW).stateParts, []);
  assert.equal(durationText('2026-01-01T10:00:00Z', '2026-01-01T10:00:12Z'), '12s');
  assert.equal(durationText('2026-01-01T10:00:00Z', '2026-01-01T11:05:00Z'), '1h 5m');
  assert.equal(durationText('nope', undefined), '');
  assert.equal(durationText('0001-01-01T00:00:00Z', '2026-01-01T10:00:12Z'), '');
});

test('the live step is pinned apart from the recorded steps and counts in the summary', () => {
  const page = running({ Steps: [{ step: 1, command: 'ls' }], Live: { Step: 2, Command: 'go test', Since: '2026-10-09T10:04:00Z' } });
  const model = taskPageModel(page, NOW);
  assert.equal(model.steps.length, 1);
  assert.equal(model.live.state, 'running');
  assert.equal(model.live.command, 'go test');
  assert.equal(logSummary(model.steps, model.live, false), 'Working · 2 commands');
  assert.ok(isTaskRunning(page));
  const ended = taskPageModel(base({ Steps: [{ step: 1, command: 'ls' }], Live: { Step: 2, Command: 'stale' } }), NOW);
  assert.equal(ended.live, undefined);
});

test('refused steps read as refused, harness corrections are hidden, summary counts refusals', () => {
  const model = taskPageModel(
    base({
      Steps: [
        { step: 1, command: 'rm -rf /', refused: true, not_run: true },
        { step: 2, command: 'reply form', not_run: true },
        { step: 3, command: 'ls' },
      ],
    }),
    NOW,
  );
  assert.deepEqual(model.steps.map((s) => s.state), ['refused', 'done']);
  assert.equal(model.refused, 1);
  assert.equal(logSummary(model.steps, undefined, true), 'Ran 2 commands · 1 refused');
  assert.equal(logSummary([], undefined, true), '');
});

test('steps show the display command, not the run-copy cd or record shims', () => {
  const command = 'cd /run/copy && go test ./... && plandb record x';
  const parts = [
    { Command: 'cd /run/copy', Separator: '&&', Start: 0, End: 12, SepEnd: 15, RunCopyPrefix: true },
    { Command: 'go test ./...', Separator: '&&', Start: 16, End: 29, SepEnd: 32 },
    { Command: 'plandb record x', Start: 33, End: 48, RecordAddressed: true },
  ];
  const model = taskPageModel(base({ Steps: [{ step: 1, command, parts, observation: 'ok', took: 3_100_000_000 }] }), NOW);
  assert.equal(model.steps[0].command, 'go test ./...');
  assert.equal(model.steps[0].took, '3.1s');
  assert.equal(tookText(0), '');
  assert.equal(tookText(120_000_000), '0.1s');
});

test('a failed exit is marked failed; full output path is kept', () => {
  const model = taskPageModel(base({ Steps: [{ step: 1, command: 'false', exit: 1, full_output: '/tmp/o.txt' }] }), NOW);
  assert.equal(model.steps[0].state, 'failed');
  assert.equal(model.steps[0].fullOutput, '/tmp/o.txt');
});

test('a result belongs to a finished task; an unfinished one shows none', () => {
  assert.equal(taskPageModel(running({ Result: 'old' }), NOW).result, '');
  assert.equal(taskPageModel(base({ Ended: { Result: 'from trajectory' } }), NOW).result, 'from trajectory');
});

test('ended reason and last words only appear on an ended task', () => {
  const ended = taskPageModel(base({ Row: { ID: 'a', Title: '', Status: 'failed' }, Ended: { Reason: 'ran out of budget' }, LastWords: 'got as far as the parser' }), NOW);
  assert.equal(ended.endReason, 'ran out of budget');
  assert.equal(ended.lastWords, 'got as far as the parser');
  assert.equal(ended.mark.label, 'Incomplete');
  assert.equal(taskPageModel(running({ Ended: { Reason: 'x' }, LastWords: 'y' }), NOW).lastWords, '');
});

test('person notes get a receipt until a later step starts; others are muted lines with an author', () => {
  const notes = [
    { Author: 'chat', Person: false, Body: 'started from the conversation', At: '2026-10-09T10:01:00Z' },
    { Person: true, Body: 'also cover the empty case', At: '2026-10-09T10:04:30Z' },
    { Person: true, Body: 'earlier note', At: '2026-10-09T10:01:30Z' },
  ];
  const waiting = taskPageModel(running({ Notes: notes, Live: { Step: 3, Command: 'ls', Since: '2026-10-09T10:04:00Z' } }), NOW);
  assert.deepEqual(waiting.notes.map((n) => n.receipt), ['', 'Delivered at its next step', 'Read']);
  assert.equal(waiting.notes[0].author, 'Conversation');
  const finished = taskPageModel(base({ Notes: notes }), NOW);
  assert.deepEqual(finished.notes.map((n) => n.receipt), ['', '', '']);
});

test('a note that repeats the result is not drawn again; empty notes vanish', () => {
  const model = taskPageModel(base({ Result: 'All good', Notes: [{ Body: 'done · ran 4s · All good' }, { Body: 'left the timeout alone' }, { Body: '' }] }), NOW);
  assert.deepEqual(model.notes.map((n) => n.body), ['left the timeout alone']);
});

test('controls: the root cannot be paused, an ended task cannot be stopped, a paused one resumes', () => {
  assert.deepEqual(taskPageModel(running(), NOW).controls, { pause: true, resume: false, stop: true });
  const root = running({ Row: { ID: 'r', Title: '', Status: 'running' } });
  assert.deepEqual(taskPageModel(root, NOW).controls, { pause: false, resume: false, stop: true });
  const paused = running({ Row: { ID: 'a', Title: '', Status: 'running', Parent: 'p', Paused: true } });
  assert.deepEqual(taskPageModel(paused, NOW).controls, { pause: false, resume: true, stop: true });
  assert.deepEqual(taskPageModel(base(), NOW).controls, { pause: false, resume: false, stop: false });
});

test('children, waits, checks and changed files', () => {
  const model = taskPageModel(
    base({
      Children: [{ ID: 'c1', Title: 'Child', Status: 'done', Depth: 2 }],
      WaitRows: [{ ID: 'w1', Title: 'Parse config', Status: 'running' }],
      Checks: ['tests pass'],
      Changed: ['a.go', 'b/c.go'],
    }),
    NOW,
  );
  assert.equal(model.children[0].row.ID, 'c1');
  assert.equal(model.children[0].depth, 1);
  assert.equal(model.waits[0].row.Title, 'Parse config');
  assert.deepEqual(model.checks, ['tests pass']);
  assert.deepEqual(model.changed, ['a.go', 'b/c.go']);
});

test('an empty page has nothing to show', () => {
  const empty = taskPageModel(base({ Description: undefined }), NOW);
  assert.equal(empty.steps.length, 0);
  assert.equal(empty.live, undefined);
  assert.equal(empty.result, '');
  assert.equal(empty.instructions, '');
  assert.equal(empty.changed.length, 0);
});

test('model names read as words', () => {
  assert.equal(modelName('deepseek/deepseek-v4.1-flash'), 'Deepseek v4.1 Flash');
  assert.equal(modelName(undefined), '');
});

test('state line carries the model tier and a cost only when the engine sent one', () => {
  assert.equal(modelTier('deepseek/deepseek-v4.1-flash'), 'Flash');
  assert.equal(modelTier('acme/oss-120b'), 'Oss 120b');
  assert.equal(modelTier(undefined), '');
  assert.equal(costPart(0.0612), '$0.06');
  assert.equal(costPart(0), '');
  assert.equal(costPart(undefined), '');
  const priced = taskPageModel(running({ Row: { ID: 'a', Title: '', Status: 'running', Model: 'deepseek/deepseek-v4.1-flash', USD: 0.06 } }), NOW);
  assert.deepEqual(priced.stateParts.slice(-2), ['Flash', '$0.06']);
});
