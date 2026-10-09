import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineEntry, EngineSnapshot } from '../chat/engine-client.ts';
import { emptyOverlay, projectConversation, reduceLiveEvent } from './transcript.ts';

const entry = (e: Partial<EngineEntry> & { Role: EngineEntry['Role'] }): EngineEntry =>
  ({ Text: '', ...e }) as EngineEntry;

const snap = (entries: EngineEntry[], running = false, tasks: unknown[] = []): EngineSnapshot =>
  ({ id: 's1', sessionFile: 'f.jsonl', entries, running, tasks, title: 'T', questions: [] }) as unknown as EngineSnapshot;

const call = (CallID: string, Answered: boolean, extra: Partial<EngineEntry> = {}) =>
  entry({ Role: 'tool', Tool: 'bash', Hint: `run ${CallID}`, CallID, Answered, ...extra });

const event = (kind: string, text = '', tool = '', hint = '') => ({ kind, text, tool, hint, raw: {} });
const user = entry({ Role: 'user', Text: 'go' });
const stepsOf = (items: ReturnType<typeof projectConversation>['turns'][0]['items']) =>
  items.flatMap((i) => (i.kind === 'tools' ? i.steps : []));

test('streaming text never hides the recorded tool group', () => {
  const entries = [user, call('a', true), call('b', true)];
  const overlay = reduceLiveEvent(emptyOverlay(), event('text', 'Looking'), entries.length);
  const turn = projectConversation(snap(entries, true), overlay).turns[0];
  assert.deepEqual(turn.items.map((i) => i.kind), ['tools', 'text']);
  assert.equal(stepsOf(turn.items).length, 2);
});

test('a live tool joins the recorded group as one running row', () => {
  const entries = [user, call('a', true)];
  const overlay = reduceLiveEvent(emptyOverlay(), event('toolBegin', '', 'bash', 'run b'), entries.length);
  const turn = projectConversation(snap(entries, true), overlay).turns[0];
  assert.equal(turn.items.filter((i) => i.kind === 'tools').length, 1);
  const steps = stepsOf(turn.items);
  assert.equal(steps.length, 2);
  assert.equal(steps.filter((s) => s.state === 'running').length, 1);
});

test('a recorded running call is not drawn twice by the live tool', () => {
  const entries = [user, call('a', true), call('b', false)];
  const overlay = reduceLiveEvent(emptyOverlay(), event('toolBegin', '', 'bash', 'run b'), entries.length);
  const steps = stepsOf(projectConversation(snap(entries, true), overlay).turns[0].items);
  assert.equal(steps.length, 2);
  assert.equal(steps.filter((s) => s.state === 'running').length, 1);
});

test('a live tool the snapshot already recorded is not repeated', () => {
  const entries = [user, call('a', true)];
  const overlay = reduceLiveEvent(emptyOverlay(), event('toolBegin', '', 'bash', 'run a'), 1);
  const steps = stepsOf(projectConversation(snap(entries, true), overlay).turns[0].items);
  assert.equal(steps.length, 1);
});

test('live text with recorded tools and a live tool keeps every recorded item', () => {
  const entries = [user, call('a', true)];
  let overlay = reduceLiveEvent(emptyOverlay(), event('text', 'Hmm'), 2);
  overlay = reduceLiveEvent(overlay, event('toolBegin', '', 'bash', 'run b'), 2);
  const turn = projectConversation(snap(entries, true), overlay).turns[0];
  assert.deepEqual(turn.items.map((i) => i.kind), ['tools', 'text', 'tools']);
});

const aside = (Text: string, extra: object = {}) => entry({ Role: 'aside', Text, ...extra } as Partial<EngineEntry> & { Role: 'aside' });

test('aside kinds choose their row; unlabelled long notes fold', () => {
  const long = 'x'.repeat(300);
  const entries = [user, aside('raw model note', { AsideKind: 'job', AsideTitle: 'build' }), aside('w', { AsideKind: 'watch' }), aside('r', { AsideKind: 'resume' }), aside(long)];
  const items = projectConversation(snap(entries)).turns[0].items;
  assert.deepEqual(items.map((i) => i.kind), ['aside', 'aside', 'note', 'note']);
  const job = items[0];
  assert.equal(job.kind === 'aside' && job.title, 'build');
  assert.equal(items[1].kind === 'aside' && items[1].title, '');
  assert.equal(items[2].kind === 'note' && items[2].long, undefined);
  assert.equal(items[3].kind === 'note' && items[3].long, true);
});

test('interrupted entries stop the turn and the step is stopped, not failed', () => {
  const entries = [user, call('a', false, { Failed: true, Interrupted: true })];
  const turn = projectConversation(snap(entries)).turns[0];
  assert.equal(turn.state, 'stopped');
  assert.equal(stepsOf(turn.items)[0].state, 'stopped');
});

