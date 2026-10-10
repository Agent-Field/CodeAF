import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineEvent } from '../../chat/engine-client.ts';
import type { TurnBlock, WorkBlock } from '../types.ts';
import { emptyLive, reduceLive, type LiveOverlayV2 } from './live.ts';
import { projectTurnsV2 } from './project.ts';
import { event, final, narrate, snap, tool, toolEvent, user } from './testkit.ts';

const play = (events: EngineEvent[], entryCount = 2, start = emptyLive(), t0 = 1_000): LiveOverlayV2 =>
  events.reduce((o, e, i) => reduceLive(o, e, entryCount, t0 + i * 1000), start);

const text = (s: string) => event('text', { text: s });
const works = (blocks: TurnBlock[]) => blocks.filter((b): b is WorkBlock => b.kind === 'work');
const running = (entries: ReturnType<typeof user>[], live: LiveOverlayV2) =>
  projectTurnsV2(snap(entries, { running: true }), live).turns[0];

test('text before a tool becomes the narration of its step, not an answer', () => {
  const live = play([text('Let me run the tests. '), toolEvent('toolForming', 'c1'), toolEvent('toolBegin', 'c1')]);
  assert.equal(live.text, '');
  assert.equal(live.calls[0].narration, 'Let me run the tests. ');
  const turn = running([user('go')], live);
  const step = works(turn.blocks)[0].steps[0];
  assert.deepEqual([step.title, step.state, turn.state], ['Let me run the tests', 'running', 'working']);
});

test('forming and announced calls read as preparing', () => {
  const turn = running([user('go')], play([toolEvent('toolForming', 'c1'), toolEvent('toolAnnounced', 'c1')]));
  assert.equal(works(turn.blocks)[0].steps[0].state, 'preparing');
});

test('one call across forming, begin, finished, end is one row', () => {
  const live = play([
    toolEvent('toolForming', 'c1'),
    toolEvent('toolAnnounced', 'c1', { Args: '{"command":"ls"}' }),
    toolEvent('toolBegin', 'c1'),
    toolEvent('toolFinished', 'c1', { Took: 2_500_000_000 }),
    toolEvent('toolEnd', 'c1', { Output: 'a b' }),
  ]);
  assert.equal(live.calls.length, 1);
  assert.deepEqual([live.calls[0].phase, live.calls[0].tookMs, live.calls[0].output], ['done', 2500, 'a b']);
});

test('calls that begin together are one step; a call after they settle opens the next', () => {
  const live = play([
    toolEvent('toolBegin', 'a'),
    toolEvent('toolBegin', 'b'),
    toolEvent('toolEnd', 'a'),
    toolEvent('toolEnd', 'b'),
    text('Now the second. '),
    toolEvent('toolBegin', 'c'),
  ]);
  const steps = works(running([user('go')], live).blocks)[0].steps;
  assert.deepEqual(steps.map((s) => s.calls.length), [2, 1]);
  assert.equal(steps[1].title, 'Now the second');
});

test('a caption event titles the step when no narration came first', () => {
  const live = play([toolEvent('toolBegin', 'c1'), event('caption', { text: 'Checking the suite', raw: { CallID: 'c1', Category: 'test' } })]);
  const step = works(running([user('go')], live).blocks)[0].steps[0];
  assert.deepEqual([step.title, step.titleSource, step.category], ['Checking the suite', 'caption', 'test']);
});

test('a failed call fails its step', () => {
  const live = play([toolEvent('toolBegin', 'c1'), toolEvent('toolFailed', 'c1', { Err: 'exit 2' })]);
  const step = works(running([user('go')], live).blocks)[0].steps[0];
  assert.deepEqual([step.state, step.calls[0].output], ['failed', 'exit 2']);
});

test('thinking is timed and collapses to seconds at the first non-reasoning event', () => {
  const thinking = play([event('thinking'), event('reasoning', { text: 'hmm ' }), event('reasoning', { text: 'ok' })]);
  assert.equal(thinking.thinking.text, 'hmm ok');
  assert.equal(thinking.thinking.seconds, undefined);
  let block = works(running([user('go')], thinking).blocks)[0];
  assert.equal(block.thinking?.streaming, true);
  const done = reduceLive(thinking, text('Answer'), 2, 1_000 + 7_000);
  block = works(running([user('go')], done).blocks)[0];
  assert.deepEqual([block.thinking?.streaming, block.thinking?.seconds, block.summary.thoughtSeconds], [false, 7, 7]);
});

test('streaming answer text appears as a streaming answer and the turn streams', () => {
  const turn = running([user('go')], play([text('Hel'), text('lo')]));
  const last = turn.blocks[turn.blocks.length - 1];
  assert.deepEqual([last.kind, last.kind === 'answer' && last.text, turn.state], ['answer', 'Hello', 'streaming']);
});

test('text is hidden once the record holds it', () => {
  const live = play([text('Hello there')], 1);
  const turn = running([user('go'), final('Hello there, friend.')], live);
  assert.equal(turn.blocks.filter((b) => b.kind === 'answer').length, 1);
});

test('a text delta after assistantDone starts a fresh message', () => {
  const live = play([text('First'), event('assistantDone'), text('Second')]);
  assert.equal(live.text, 'Second');
});

test('recorded calls are never drawn twice; the overlay moves them forward', () => {
  const entries = [user('go'), narrate('A'), tool('bash', 'c1', { command: 'ls' }, { Answered: false })];
  const live = play([toolEvent('toolBegin', 'c1'), toolEvent('toolEnd', 'c1', { Output: 'x' })], 1);
  const turn = running(entries, live);
  const steps = works(turn.blocks).flatMap((w) => w.steps);
  assert.equal(steps.length, 1);
  assert.deepEqual([steps[0].calls[0].state, steps[0].calls[0].output], ['done', 'x']);
});

test('live calls join the recorded work block, after its steps', () => {
  const entries = [user('go'), narrate('A'), tool('bash', 'c1', { command: 'ls' })];
  const turn = running(entries, play([toolEvent('toolBegin', 'c2')], 1));
  const [block] = works(turn.blocks);
  assert.deepEqual(block.steps.map((s) => s.calls[0].id), ['c1', 'c2']);
  assert.equal(block.live, true);
  assert.equal(block.summary.calls, 2);
});

test('steer events: accepted, consumed, fell through', () => {
  const steer = (kind: string, landing = '') => event(kind, { raw: { Steer: { ID: 'st1', Words: 'also tests', Landing: landing } } });
  const accepted = play([steer('steerAccepted', 'waiting for the running step')]);
  assert.deepEqual(accepted.steers, [{ id: 'st1', text: 'also tests', landing: 'waiting for the running step', consumed: false }]);
  const pending = running([user('go')], accepted);
  assert.equal(pending.steer[0].landing, 'waiting for the running step');
  const consumed = reduceLive(accepted, steer('steerConsumed'), 2);
  assert.deepEqual([consumed.steers[0].consumed, consumed.steers[0].landing], [true, undefined]);
  assert.equal(reduceLive(accepted, steer('steerFellThrough'), 2).steers.length, 0);
});

test('a recorded steer is not added again from the overlay', () => {
  const steerEntry = user('also tests', { Steer: { Consumed: true } });
  const live = play([event('steerAccepted', { raw: { Steer: { ID: 's', Words: 'also tests' } } })], 2);
  const turn = running([user('go'), narrate('A'), tool('bash', 'c1', {}), steerEntry], live);
  assert.equal(turn.steer.length, 1);
});

test('retrying discards drawn text and calls, and says so', () => {
  const drawn = play([text('half a rep'), toolEvent('toolBegin', 'c1')]);
  const retried = reduceLive(drawn, event('retrying', { text: 'the provider was busy' }), 2);
  assert.deepEqual([retried.text, retried.calls.length], ['', 0]);
  const block = works(running([user('go')], retried).blocks)[0];
  assert.equal(block.notes[0].kind === 'note' && block.notes[0].text, 'Retrying — the provider was busy');
  assert.equal(reduceLive(retried, text('again'), 2).retry, undefined);
});

test('retrying keeps what the record holds', () => {
  const entries = [user('go'), narrate('A'), tool('bash', 'c1', { command: 'ls' })];
  const retried = reduceLive(play([text('x')], 1), event('retrying'), 1);
  const turn = running(entries, retried);
  assert.equal(works(turn.blocks)[0].steps.length, 1);
});

test('errors become an inline error block', () => {
  const turn = running([user('go')], play([event('error', { error: 'provider fell over' })]));
  const last = turn.blocks[turn.blocks.length - 1];
  assert.deepEqual([last.kind, last.kind === 'error' && last.text], ['error', 'provider fell over']);
});

test('turnDone clears the overlay; the base entry count is taken when the overlay starts', () => {
  const live = play([text('hi')], 5);
  assert.equal(live.baseEntries, 5);
  const cleared = reduceLive(live, event('turnDone'), 7);
  assert.deepEqual([cleared.text, cleared.baseEntries, cleared.calls.length], ['', 7, 0]);
});

test('numeric other-kinds from the bridge map to the new names', () => {
  const other = (n: number, raw: Record<string, unknown>) => event('other', { raw: { Kind: n, ...raw } });
  const live = play([other(15, { CallID: 'c1', Tool: 'bash' }), other(14, { CallID: 'c1' })]);
  assert.deepEqual([live.calls.length, live.calls[0].phase], [1, 'announced']);
});

test('the overlay never touches a settled snapshot', () => {
  const settled = projectTurnsV2(snap([user('go'), final('done')]), play([text('late')]));
  assert.equal(settled.turns[0].blocks.length, 1);
});

test('Working survives intermediate text, assistantDone and the next tool until canonical turn completion', () => {
  const entries = [user('investigate')];
  let overlay = emptyLive(1);
  const events = [event('reasoning', { text: 'Trace the path.' }), toolEvent('toolBegin', 'a'), toolEvent('toolEnd', 'a'), text('I found the first cause. '), event('assistantDone'), event('reasoning', { text: 'Check the second path.' }), toolEvent('toolBegin', 'b'), toolEvent('toolEnd', 'b'), text('Here is the complete result.')];
  for (const next of events) {
    overlay = reduceLive(overlay, next, entries.length, 1000);
    const turn = running(entries, overlay);
    const blocks = works(turn.blocks);
    assert.ok(blocks.length);
    assert.equal(blocks.at(-1)?.live, true, next.kind);
    assert.ok(turn.state === 'working' || turn.state === 'streaming');
  }
  const recorded = [...entries, tool('bash', 'a', {}), final('Intermediate answer'), tool('bash', 'b', {})];
  const active = projectTurnsV2(snap(recorded, { running: true })).turns[0];
  assert.equal(works(active.blocks).at(-1)?.live, true, 'snapshot-only handoff');
  const done = projectTurnsV2(snap([...recorded, final('Done')])).turns[0];
  assert.equal(done.state, 'done');
  assert.ok(works(done.blocks).every(block => !block.live));
});

test('elapsed origins are observed once, survive retry, and are never invented for reloaded work', () => {
  let overlay = reduceLive(emptyLive(), toolEvent('toolForming', 'a'), 1, 1200);
  assert.equal(overlay.startedAt, 1200);
  assert.equal(overlay.calls[0].startedAt, undefined);
  overlay = reduceLive(overlay, toolEvent('toolBegin', 'a'), 1, 2400);
  overlay = reduceLive(overlay, toolEvent('toolEnd', 'a'), 1, 4000);
  assert.equal(overlay.calls[0].startedAt, 2400);
  assert.equal(works(running([user('go')], overlay).blocks)[0].startedAt, 1200);
  overlay = reduceLive(overlay, event('retrying'), 1, 4500);
  assert.equal(overlay.startedAt, 1200);
  assert.equal(reduceLive(overlay, event('turnDone'), 3, 5000).startedAt, undefined);
  const reloaded = projectTurnsV2(snap([user('go'), tool('bash', 'a', {})], { running: true })).turns[0];
  assert.equal(works(reloaded.blocks)[0].startedAt, undefined);
});

test('task-only questions cannot pause foreground tools even when a worker reuses a call id', () => {
  const overlay = play([toolEvent('toolBegin', 'c')], 1);
  const q = { id: 7, kind: 'choice', head: 'Background question', ask: 'Proceed?', subject: { callId: 'c' }, asker: { kind: 'task' }, blocking: { turn: false, tasks: ['background'] } };
  const project = (question: object) => projectTurnsV2(snap([user('go')], { running: true, questions: [question] as never }), overlay).turns[0];
  assert.equal(works(project(q).blocks)[0].steps[0].state, 'running');
  assert.equal(works(project({ ...q, blocking: { tasks: ['background'] } }).blocks)[0].steps[0].state, 'running');
  assert.equal(works(project({ ...q, blocking: { turn: true } }).blocks)[0].steps[0].state, 'waiting');
  assert.equal(works(project({ ...q, blocking: { turn: true }, withdrawn: { reason: 'resolved' } }).blocks)[0].steps[0].state, 'running');
  assert.equal(works(project({ id: 7, kind: 'choice', head: 'Older question', ask: 'Proceed?', subject: { callId: 'c' } }).blocks)[0].steps[0].state, 'waiting');
});

test('a retry shows the delay the engine sent, and nothing when it sent none', () => {
  const withDelay = reduceLive(emptyLive(), event('retrying', { text: 'busy', raw: { Retry: { DelaySeconds: 3.2 } } }), 1);
  const note = works(running([user('go')], withDelay).blocks)[0].notes[0];
  assert.deepEqual(note.kind === 'note' && [note.text, note.time], ['Retrying — busy', '4s']);
  const without = works(running([user('go')], reduceLive(emptyLive(), event('retrying', { text: 'busy' }), 1)).blocks)[0].notes[0];
  assert.equal(without.kind === 'note' && without.time, undefined);
});

test('compacted inserts the summarized divider live; compacting draws nothing', () => {
  const compacting = reduceLive(emptyLive(), event('compacting'), 1);
  assert.equal(works(running([user('go')], compacting).blocks).length, 0);
  const done = reduceLive(compacting, event('compacted'), 1);
  const note = works(running([user('go')], done).blocks)[0].notes[0];
  assert.deepEqual(note.kind === 'note' && [note.text, note.tone], ['Earlier messages summarized', 'compaction']);
  assert.equal(reduceLive(done, event('retrying'), 1).compacted, true);
});
