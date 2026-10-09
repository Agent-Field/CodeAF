import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineEntry, EngineSnapshot } from '../chat/engine-client.ts';
import { digestOf, emptyOverlay, projectConversation, reduceLiveEvent } from './transcript.ts';

const entry = (e: Partial<EngineEntry> & { Role: EngineEntry['Role'] }): EngineEntry =>
  ({ Text: '', ...e }) as EngineEntry;

const snap = (entries: EngineEntry[], running = false, tasks: unknown[] = []): EngineSnapshot =>
  ({
    id: 's1',
    sessionFile: 'f.jsonl',
    entries,
    running,
    tasks,
    title: 'T',
    questions: [],
  }) as unknown as EngineSnapshot;

const call = (CallID: string, Answered: boolean, extra: Partial<EngineEntry> = {}) =>
  entry({ Role: 'tool', Tool: 'bash', Hint: `run ${CallID}`, CallID, Answered, ...extra });

const event = (kind: string, text = '', tool = '', hint = '') => ({ kind, text, tool, hint, raw: {} });

test('tool entries sharing a CallID are one step', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'go' }),
      call('a', false, { Output: '' }),
      call('a', true, { Output: 'ok' }),
    ]),
  );
  const tools = model.turns[0].items[0];
  assert.equal(tools.kind, 'tools');
  if (tools.kind !== 'tools') return;
  assert.equal(tools.steps.length, 1);
  assert.equal(tools.steps[0].output, 'ok');
  assert.equal(tools.steps[0].state, 'done');
});

test('consecutive calls group; text splits groups', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'go' }),
      call('a', true),
      call('b', true),
      entry({ Role: 'assistant', Text: 'between' }),
      call('c', true),
    ]),
  );
  const kinds = model.turns[0].items.map((i) => i.kind);
  assert.deepEqual(kinds, ['tools', 'text', 'tools']);
  const first = model.turns[0].items[0];
  assert.equal(first.kind === 'tools' && first.steps.length, 2);
});

test('blank assistant records do not split a group of steps', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'go' }),
      entry({ Role: 'assistant', Text: '' }),
      call('a', true),
      entry({ Role: 'assistant', Text: '  ' }),
      call('b', true),
    ]),
  );
  const items = model.turns[0].items;
  assert.deepEqual(items.map((i) => i.kind), ['tools']);
  assert.equal(items[0].kind === 'tools' && items[0].steps.length, 2);
});

test('raw results without a tool name are skipped', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'go' }),
      call('a', true),
      entry({ Role: 'tool', Text: 'raw result', CallID: 'a' }),
    ]),
  );
  assert.equal(model.turns[0].items.length, 1);
});

test('only the last unanswered call runs while the engine runs', () => {
  const entries = [entry({ Role: 'user', Text: 'go' }), call('a', true), call('b', false)];
  const live = projectConversation(snap(entries, true));
  const item = live.turns[0].items[0];
  assert.equal(item.kind === 'tools' && item.steps[1].state, 'running');
  const idle = projectConversation(snap(entries, false));
  const done = idle.turns[0].items[0];
  assert.equal(done.kind === 'tools' && done.steps[1].state, 'done');
});

test('task aside becomes a task item', () => {
  const text =
    'Split this into three small tasks... done · ran 30s · Counted words in README.md and produced word-count.txt. Commands and raw output were kept.';
  const model = projectConversation(
    snap([entry({ Role: 'user', Text: 'go' }), entry({ Role: 'aside', Text: text })], false, [
      { ID: 't-7', Title: 'Split this into three small tasks...' },
    ]),
  );
  const item = model.turns[0].items[0];
  assert.equal(item.kind, 'task');
  if (item.kind !== 'task') return;
  assert.equal(item.title, 'Split this into three small tasks...');
  assert.equal(item.status, 'done');
  assert.equal(item.summary, 'Counted words in README.md and produced word-count.txt.');
  assert.equal(item.body, text);
  assert.equal(item.taskId, 't-7');
});

test('a run report (ask, blank line, status line) becomes a task item', () => {
  const text = 'Count the words.\n\ndone · ran 1m 8s · Produced word-count.txt with the total. More detail. · nothing to land';
  const model = projectConversation(snap([entry({ Role: 'user', Text: 'go' }), entry({ Role: 'aside', Text: text })]));
  const item = model.turns[0].items[0];
  assert.equal(item.kind, 'task');
  if (item.kind !== 'task') return;
  assert.equal(item.title, 'Count the words.');
  assert.equal(item.status, 'done');
  assert.equal(item.summary, 'Produced word-count.txt with the total.');
});

test('structured task fields win; other asides are notes', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'go' }),
      { ...entry({ Role: 'aside', Text: 'x failed · ran 2s · broke.' }), TaskIDs: ['t-1'], TaskStatus: 'failed' } as EngineEntry,
      entry({ Role: 'aside', Text: 'Resumed after an interrupt.' }),
    ]),
  );
  const [task, note] = model.turns[0].items;
  assert.equal(task.kind === 'task' && task.taskId, 't-1');
  assert.equal(note.kind, 'note');
});

test('notes before the first user entry are preface', () => {
  const model = projectConversation(
    snap([entry({ Role: 'note', Text: 'Compacted.' }), entry({ Role: 'user', Text: 'hi' })]),
  );
  assert.equal(model.preface.length, 1);
  assert.equal(model.turns.length, 1);
  assert.equal(model.turns[0].id, 'f.jsonl:1');
});

test('interrupted turn is stopped; finished turn is done', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'a' }),
      entry({ Role: 'assistant', Text: '**Done.** Yes' }),
      entry({ Role: 'user', Text: 'b' }),
      entry({ Role: 'assistant', Text: 'half', Interrupted: true }),
    ]),
  );
  assert.deepEqual(model.turns.map((t) => t.state), ['done', 'stopped']);
  assert.equal(model.turns[0].digest, 'Done. Yes');
});

test('streaming overlay appends to the last turn without duplicating', () => {
  const entries = [entry({ Role: 'user', Text: 'go' })];
  let overlay = reduceLiveEvent(emptyOverlay(), event('text', 'Hel'), entries.length);
  overlay = reduceLiveEvent(overlay, event('text', 'lo'), entries.length);
  const streaming = projectConversation(snap(entries, true), overlay);
  const turn = streaming.turns[0];
  assert.equal(turn.state, 'streaming');
  assert.deepEqual(turn.items.map((i) => i.kind === 'text' && i.text), ['Hello']);

  const recorded = [...entries, entry({ Role: 'assistant', Text: 'Hello world' })];
  const after = projectConversation(snap(recorded, true), overlay);
  assert.equal(after.turns[0].items.length, 1);
  assert.equal(after.turns[0].state, 'working');

  const reset = reduceLiveEvent(overlay, event('assistantDone'), recorded.length);
  assert.equal(reset.text, '');
});

test('overlay shows thinking and the active tool, and clears them', () => {
  const entries = [entry({ Role: 'user', Text: 'go' })];
  let overlay = reduceLiveEvent(emptyOverlay(), event('thinking', 'hmm'), 1);
  overlay = reduceLiveEvent(overlay, event('toolBegin', '', 'bash', 'ls'), 1);
  const turn = projectConversation(snap(entries, true), overlay).turns[0];
  assert.deepEqual(turn.items.map((i) => i.kind), ['thinking', 'tools']);
  overlay = reduceLiveEvent(overlay, event('toolEnd'), 1);
  assert.equal(overlay.activeTool, undefined);
  assert.equal(reduceLiveEvent(overlay, event('turnDone'), 1).thinking, '');
});

test('overlay is ignored once the snapshot is not running', () => {
  const overlay = reduceLiveEvent(emptyOverlay(), event('text', 'late'), 1);
  const model = projectConversation(snap([entry({ Role: 'user', Text: 'go' })], false), overlay);
  assert.equal(model.turns[0].items.length, 0);
});

test('digestOf strips marks and clips', () => {
  assert.equal(digestOf('## **Bold** [link](http://x) `code`\nmore'), 'Bold link code');
  assert.equal(digestOf('```\nskip\n```\n- item one'), 'item one');
  assert.equal(digestOf(''), '');
  assert.ok(digestOf('x'.repeat(300)).length <= 140);
});

test('a call the record marks Failed is a failed step, answered or not', () => {
  const model = projectConversation(
    snap([
      entry({ Role: 'user', Text: 'go' }),
      call('a', true, { Failed: true, Output: 'boom' }),
      call('b', true, { Output: 'ok' }),
    ]),
  );
  const item = model.turns[0].items[0];
  assert.equal(item.kind === 'tools' && item.steps[0].state, 'failed');
  assert.equal(item.kind === 'tools' && item.steps[1].state, 'done');
});
