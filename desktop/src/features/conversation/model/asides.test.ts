import test from 'node:test';
import assert from 'node:assert/strict';
import { digestOf } from '../transcript-parse.ts';
import { asideItem } from './tasks.ts';
import { entry, snap } from './testkit.ts';

// Ported from the v1 transcript tests: the canonical task row outranks words parsed out of the note.
const read = (Text: string, rows: unknown[] = [], extra: object = {}) =>
  asideItem(entry({ Role: 'aside', Text, ...extra }), 'a', snap([], { tasks: rows as never }));

test('task aside becomes a task item', () => {
  const text =
    'Split this into three small tasks... done · ran 30s · Counted words in README.md and produced word-count.txt. Commands and raw output were kept.';
  const item = read(text, [{ ID: 't-7', Title: 'Split this into three small tasks...' }]);
  assert.equal(item.kind, 'task');
  if (item.kind !== 'task') return;
  assert.equal(item.title, 'Split this into three small tasks...');
  assert.equal(item.status, 'done');
  assert.equal(item.summary, 'Counted words in README.md and produced word-count.txt.');
  assert.equal(item.body, text);
  assert.equal(item.taskId, 't-7');
});

test('a run report takes its title from the task whose note it carries, never from the ask', () => {
  const note = 'done · ran 1m 8s · Produced word-count.txt with the total. More detail.';
  const item = read(`Count the words, then list the files and summarise.\n\n${note} · nothing to land`, [
    { ID: 't-1', Title: 'List files', Status: 'done', Note: 'done · ran 2s · Listed.' },
    { ID: 't-2', Title: 'Count words', Status: 'done', Note: note },
  ]);
  assert.equal(item.kind === 'task' && item.title, 'Count words');
  assert.equal(item.kind === 'task' && item.taskId, 't-2');
  assert.equal(item.kind === 'task' && item.summary, 'Produced word-count.txt with the total.');
});

test('a notice summary never ends inside code and drops code marks', () => {
  const note = 'done · ran 6s · Created files.txt via `find . -type f | sort`. Verified.';
  const item = read(`Ask.\n\n${note}`, [{ ID: 't-1', Title: 'List files', Status: 'done', Note: note }]);
  assert.equal(item.kind === 'task' && item.summary, 'Created files.txt via find . -type f | sort.');
});

test('a run report with no matching task is a note, not a notice titled by the ask', () => {
  assert.equal(read('Count the words.\n\ndone · ran 1m 8s · Produced word-count.txt with the total.').kind, 'note');
});

test('structured task fields win; other asides are notes', () => {
  const task = read('x failed · ran 2s · broke.', [], { TaskIDs: ['t-1'], TaskStatus: 'failed' });
  assert.equal(task.kind === 'task' && task.taskId, 't-1');
  assert.equal(task.kind === 'task' && task.status, 'failed');
  assert.equal(read('Resumed after an interrupt.').kind, 'note');
});

test('an aside with a task id but no parsable shape takes its title and status from the task row', () => {
  const text = 'Task 2 started: Migrate the settings screen. More detail follows.';
  const item = read(text, [{ ID: 't-2', Title: 'Migrate the settings screen', Status: 'running' }], { TaskIDs: ['t-2'] });
  assert.equal(item.kind, 'task');
  if (item.kind !== 'task') return;
  assert.equal(item.title, 'Migrate the settings screen');
  assert.equal(item.status, 'running');
  assert.equal(item.summary, 'Task 2 started: Migrate the settings screen.');
});

test('the task row named by id outranks the run request parsed out of the note', () => {
  const text = 'Use propose_task to make two tasks done · ran 30s · Counted words.';
  const item = read(text, [{ ID: '2', Title: 'Count the words in README.md', Status: 'done' }], { TaskIDs: ['2'] });
  assert.equal(item.kind === 'task' && item.title, 'Count the words in README.md');
});

test('an aside with a task id but no title anywhere is a note, never an unnamed task', () => {
  assert.equal(read('Task 9 started: something.', [], { TaskIDs: ['t-9'] }).kind, 'note');
});

test('aside kinds choose their row; unlabelled long notes fold', () => {
  const job = read('raw model note', [], { AsideKind: 'job', AsideTitle: 'build' });
  assert.equal(job.kind === 'aside' && job.title, 'build');
  assert.equal(read('w', [], { AsideKind: 'watch' }).kind, 'aside');
  const resume = read('r', [], { AsideKind: 'resume' });
  assert.equal(resume.kind === 'note' && resume.long, undefined);
  const long = read('x'.repeat(300));
  assert.equal(long.kind === 'note' && long.long, true);
});

test('digestOf strips marks and clips', () => {
  assert.equal(digestOf('## **Bold** [link](http://x) `code`\nmore'), 'Bold link code');
  assert.equal(digestOf('```\nskip\n```\n- item one'), 'item one');
  assert.equal(digestOf(''), '');
  assert.ok(digestOf('x'.repeat(300)).length <= 140);
});

// The record shape of a hand-off landing (session task_run.go taskNote, batched by
// "while you worked:"): the engine's instruction to the model, the task head with a
// transcript link, then the report. The task is a quick hand-off with no plan row.
const LANDING = [
  'while you worked:',
  '',
  'A note from the session, not from the person: work you handed off landed `done` — say that word back and no other, then answer the request it was for in its latest wording. Do not say again that it landed, and do not grade it.',
  'task 1 done: reading: List the files in this folder, read README if any, then c… · transcript file:///store/runs/s/tasks/1.jsonl',
  '`notes/hello.md` (3-line summary):',
  'Folder: a scratch workspace.',
].join('\n');

test('a session note to the model is drawn in a person\'s words: no instruction, no transcript path, folded', () => {
  const item = read(LANDING, [], { AsideKind: 'task', TaskIDs: ['1'] });
  assert.equal(item.kind, 'note');
  if (item.kind !== 'note') return;
  assert.equal(item.long, true);
  assert.equal(item.text.split('\n')[0], 'task 1 done: reading: List the files in this folder, read README if any, then c…');
  assert.ok(!/while you worked|A note from the session|file:\/\/|store\/runs/.test(item.text), item.text);
  assert.ok(item.text.includes('Folder: a scratch workspace.'));
});

test('a task notice built from a session note never carries the instruction in its summary or body', () => {
  const item = read(LANDING, [{ ID: '1', Title: 'Read the folder', Status: 'done' }], { AsideKind: 'task', TaskIDs: ['1'] });
  assert.equal(item.kind, 'task');
  if (item.kind !== 'task') return;
  assert.equal(item.title, 'Read the folder');
  assert.ok(!/A note from the session|file:\/\//.test(item.summary + item.body), item.body);
});

test('a one-line session note is a quiet line, not a fold', () => {
  const item = read('while you worked: the build task finished');
  assert.deepEqual(item.kind === 'note' && [item.text, item.long], ['the build task finished', undefined]);
});
