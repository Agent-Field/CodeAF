import test from 'node:test';
import assert from 'node:assert/strict';
import type { TurnBlock, WorkBlock } from '../types.ts';
import { projectTurnsV2 } from './project.ts';
import { entry, final, narrate, snap, tool, update, user } from './testkit.ts';

const kinds = (blocks: TurnBlock[]) => blocks.map((b) => b.kind);
const works = (blocks: TurnBlock[]) => blocks.filter((b): b is WorkBlock => b.kind === 'work');
const first = (entries: ReturnType<typeof user>[]) => projectTurnsV2(snap(entries)).turns[0];

test('interim updates and the final answer are told apart by flags, not position', () => {
  const turn = first([
    user('fix the bug'),
    update('Found it, fixing now.'),
    tool('edit', 'c1', { path: 'a.go', edits: [{ oldText: 'a', newText: 'b' }] }),
    final('Fixed.\n\nDetails below.'),
  ]);
  assert.deepEqual(kinds(turn.blocks), ['update', 'work', 'answer', 'deliverable']);
  assert.equal(turn.digest, 'Fixed.');
});

test('an interrupted [update] is a cut update; the turn is stopped', () => {
  const cut = update('Half a thou', { Answer: false, Interrupted: true });
  const turn = first([user('go'), cut]);
  assert.deepEqual(turn.blocks[0], { kind: 'update', id: 'f.jsonl:0:1', text: 'Half a thou', cut: true, streaming: false });
  assert.equal(turn.state, 'stopped');
});

test('narration titles the step that follows; it is never an answer', () => {
  const turn = first([
    user('look around'),
    narrate('Let me read the loader.'),
    tool('read', 'c1', { path: 'internal/x/a.go' }),
    final('Done.'),
  ]);
  const [work] = works(turn.blocks);
  assert.equal(work.steps[0].title, 'Let me read the loader');
  assert.equal(work.steps[0].titleSource, 'narration');
  assert.deepEqual(kinds(turn.blocks), ['work', 'answer']);
});

test('calls after one assistant entry are one batch; a new assistant entry starts another', () => {
  const turn = first([
    user('go'),
    narrate('Reading both files.'),
    tool('read', 'a', { path: 'x/a.go' }, { Caption: 'Reading the sources', CaptionCategory: 'read', Took: 2_000_000 }),
    tool('read', 'b', { path: 'x/b.go' }, { Took: 3_000_000 }),
    narrate(''),
    tool('bash', 'c', { command: 'ls' }),
    tool('bash', 'd', { command: 'pwd' }),
    final('ok'),
  ]);
  const [work] = works(turn.blocks);
  assert.equal(work.steps.length, 2);
  assert.equal(work.steps[0].calls.length, 2);
  // The two reads ran side by side: the step took as long as the longer one.
  assert.equal(work.steps[0].tookMs, 3);
  assert.deepEqual(work.steps[0].calls.map((c) => c.tookMs), [2, 3]);
  assert.equal(work.steps[1].title, 'Ran 2 commands');
  assert.equal(work.steps[1].titleSource, 'composed');
  assert.equal(work.steps[1].category, 'run');
  assert.deepEqual(work.summary, { seconds: 1, thoughtSeconds: undefined, steps: 2, calls: 4, failed: 0 });
});

test('caption beats composed when there is no narration', () => {
  const turn = first([
    user('go'),
    narrate(''),
    tool('bash', 'a', { command: 'go test ./...' }, { Caption: 'Checking the suite', CaptionCategory: 'test' }),
    final('ok'),
  ]);
  const step = works(turn.blocks)[0].steps[0];
  assert.deepEqual([step.title, step.titleSource, step.category], ['Checking the suite', 'caption', 'test']);
});

test('consecutive work between conversation blocks is one work block with its notes', () => {
  const turn = first([
    user('go'),
    narrate('One.'),
    tool('bash', 'a', { command: 'ls' }),
    entry({ Role: 'note', Text: 'Earlier messages summarized' }),
    narrate('Two.'),
    tool('bash', 'b', { command: 'pwd' }),
    entry({ Role: 'aside', Text: 'log tail', AsideKind: 'job', AsideTitle: 'make watch' }),
    final('done'),
  ]);
  const blocks = works(turn.blocks);
  assert.equal(blocks.length, 1);
  assert.equal(blocks[0].steps.length, 2);
  assert.deepEqual(blocks[0].notes.map((n) => n.kind), ['note', 'aside']);
});

test('an update splits the work into two blocks', () => {
  const turn = first([
    user('go'),
    narrate('A'),
    tool('bash', 'a', { command: 'ls' }),
    update('Halfway there.'),
    narrate('B'),
    tool('bash', 'b', { command: 'pwd' }),
    final('end'),
  ]);
  assert.deepEqual(kinds(turn.blocks), ['work', 'update', 'work', 'answer']);
});

test('narration with no tool after it is kept as a quiet work line', () => {
  const turn = first([user('go'), narrate('Thinking aloud with nothing to run.'), final('x')]);
  const [work] = works(turn.blocks);
  assert.equal(work.steps.length, 0);
  assert.equal(work.notes[0].kind, 'note');
});

test('a steer user entry joins the turn, not a new one', () => {
  const steer = user('also check the tests', { Steer: { Landing: 'waiting for the running step', Consumed: true } });
  const turn = projectTurnsV2(snap([user('go'), narrate('A'), tool('bash', 'a', { command: 'ls' }), steer, final('ok')])).turns;
  assert.equal(turn.length, 1);
  assert.deepEqual(turn[0].steer[0], { id: 'f.jsonl:0:3', text: 'also check the tests', landing: 'waiting for the running step', consumed: true });
});

test('failed and stopped calls colour their step', () => {
  const entries = [
    user('go'),
    narrate('A'),
    tool('bash', 'a', { command: 'false' }, { Failed: true }),
    narrate('B'),
    tool('bash', 'b', { command: 'sleep 9' }, { Answered: false, Interrupted: true }),
  ];
  const turn = first(entries);
  const [work] = works(turn.blocks);
  assert.deepEqual(work.steps.map((s) => s.state), ['failed', 'stopped']);
  assert.equal(work.summary.failed, 1);
  assert.equal(turn.state, 'stopped');
});

test('the in-flight call of a running snapshot is running', () => {
  const turn = projectTurnsV2(snap([user('go'), narrate('A'), tool('bash', 'a', {}, { Answered: false })], { running: true })).turns[0];
  assert.equal(works(turn.blocks)[0].steps[0].state, 'running');
  assert.equal(turn.state, 'working');
});

test('task asides become task blocks; the canonical row names them', () => {
  const aside = entry({ Role: 'aside', Text: 'Parse config done · wrote the loader · took 3m', TaskIDs: ['t-7'] });
  const tasks = [{ ID: 't-7', Title: 'Parse config', Status: 'done' }];
  const turn = projectTurnsV2(snap([user('go'), aside], { tasks: tasks as never })).turns[0];
  const block = turn.blocks[0];
  assert.equal(block.kind, 'task');
  assert.deepEqual(block.kind === 'task' && [block.taskId, block.title, block.status], ['t-7', 'Parse config', 'done']);
});

test('an unnamed aside is only a work note', () => {
  const turn = first([user('go'), entry({ Role: 'aside', Text: 'some long machine text' })]);
  assert.equal(works(turn.blocks)[0].notes[0].kind, 'note');
});

test('records before the first user message are the preface', () => {
  const { turns, preface } = projectTurnsV2(snap([entry({ Role: 'note', Text: 'Resumed' }), user('hi'), final('hello')]));
  assert.equal(turns.length, 1);
  assert.deepEqual(preface.map((p) => p.kind), ['note']);
});

test('a repeated CallID updates the call instead of adding a row', () => {
  const turn = first([
    user('go'),
    narrate('A'),
    tool('bash', 'a', { command: 'ls' }, { Answered: false }),
    tool('bash', 'a', { command: 'ls' }, { Output: 'x' }),
  ]);
  const calls = works(turn.blocks)[0].steps[0].calls;
  assert.equal(calls.length, 1);
  assert.deepEqual([calls[0].state, calls[0].output], ['done', 'x']);
});

test('a lone word said back ("done") never titles a step; the tools do, and durations are per call', () => {
  // The record shape of a hand-off landing: the model says the landing word back, then lists.
  const turn = first([
    user('List the files, then write notes/hello.md'),
    narrate('done'),
    tool('ls', 'l1', { path: '/ws/notes' }, { Output: 'hello.md' }),
    narrate(''),
    tool('read', 'r1', { path: '/ws/notes/hello.md' }, { Took: 6_094_000_000 }),
    final('Created notes/hello.md.'),
  ]);
  const [work] = works(turn.blocks);
  assert.deepEqual(work.steps.map((s) => [s.title, s.titleSource]), [['Listed 1 item', 'composed'], ['Read hello.md', 'composed']]);
  assert.deepEqual(work.steps.map((s) => s.tookMs), [undefined, 6094]);
  assert.equal(work.steps[1].calls[0].tookMs, 6094);
});

test('a repeated CallID without a duration keeps the one already known', () => {
  const turn = first([
    user('go'),
    tool('read', 'r1', { path: 'a.go' }, { Took: 2_000_000_000 }),
    tool('read', 'r1', { path: 'a.go' }, { Output: 'package a' }),
    final('ok'),
  ]);
  assert.equal(works(turn.blocks)[0].steps[0].calls[0].tookMs, 2000);
});

test('a failed call retried to success at the same target is not counted failed', () => {
  const turn = first([
    user('go'),
    narrate('A'),
    tool('propose_task', 'a', { title: 'x' }, { Failed: true, Hint: 'propose_task x' }),
    narrate('B'),
    tool('propose_task', 'b', { title: 'x' }, { Hint: 'propose_task x' }),
    tool('bash', 'c', { command: 'false' }, { Failed: true, Hint: 'bash false' }),
    tool('bash', 'd', { command: 'true' }, { Hint: 'bash true' }),
    final('ok'),
  ]);
  const [work] = works(turn.blocks);
  // The propose_task retry worked; the bash failure has no later success at its own command.
  assert.equal(work.summary.failed, 1);
});

test('time spent waiting on a question is not worked time', () => {
  const ns = (s: number) => s * 1e9;
  const turn = first([
    user('go'),
    narrate('A'),
    tool('bash', 'a', { command: 'ls' }, { Took: ns(2) }),
    narrate('B'),
    tool('ask', 'q', { head: 'Which?' }, { Took: ns(80) }),
    final('ok'),
  ]);
  const [work] = works(turn.blocks);
  assert.equal(work.summary.seconds, 2);
  assert.equal(work.steps[1].tookMs, undefined);
});

test('context changes stay chronological outside collapsed work and carry exact Undo authority',()=>{
 const turn=first([user('go'),tool('read','c1',{path:'a.go'}),entry({Role:'aside',AsideKind:'places',Text:'Now also using Release',UndoReceipts:['rc_exact']}),final('Done.')]);
 assert.deepEqual(kinds(turn.blocks),['work','context-note','answer']);
 const note=turn.blocks[1];
 assert.equal(note.kind,'context-note');
 if(note.kind==='context-note') assert.deepEqual(note.undoReceipts,['rc_exact']);
 assert.deepEqual(works(turn.blocks)[0].notes,[]);
});
