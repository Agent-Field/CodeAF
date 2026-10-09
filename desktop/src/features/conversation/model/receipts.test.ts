import test from 'node:test';
import assert from 'node:assert/strict';
import type { TurnBlock } from '../types.ts';
import type { QuestionOutcome } from './entry.ts';
import { projectTurnsV2 } from './project.ts';
import { final, narrate, snap, tool, user } from './testkit.ts';

const receipts = (blocks: TurnBlock[]) => blocks.flatMap((b) => (b.kind === 'receipt' ? [b] : []));

const consent = (extra: object = {}) => ({
  id: 4,
  kind: 'consent',
  ask: 'permission',
  head: 'Allow `rm -rf build`?',
  subject: { kind: 'call', callId: 'c1' },
  ...extra,
});

const entries = [
  user('clean'),
  narrate('Removing the build.'),
  tool('bash', 'c1', { command: 'rm -rf build' }, { Answered: false }),
  user('and then?'),
  final('later'),
];

test('a waiting question puts a receipt in the turn that asked, matched by call', () => {
  const { turns } = projectTurnsV2(snap(entries, { questions: [consent()] as never }));
  assert.equal(receipts(turns[1].blocks).length, 0);
  assert.deepEqual(receipts(turns[0].blocks).map((r) => [r.state, r.text, r.questionKey]), [
    ['waiting', 'Waiting on you: Allow `rm -rf build`?', 'consent:4'],
  ]);
  assert.equal(turns[0].blocks[turns[0].blocks.length - 1].kind, 'receipt');
});

test('a question about no call lands on the last turn', () => {
  const q = consent({ subject: { kind: 'account' }, id: 9, kind: 'connect' });
  const { turns } = projectTurnsV2(snap(entries, { questions: [q] as never }));
  assert.equal(receipts(turns[1].blocks).length, 1);
});

test('a withdrawn question says why, in the engine words', () => {
  const q = consent({ withdrawn: { reason: 'the turn moved on without it' } });
  const { turns } = projectTurnsV2(snap(entries, { questions: [q] as never }));
  assert.deepEqual(receipts(turns[0].blocks).map((r) => [r.state, r.text]), [
    ['withdrawn', 'No longer needed — the turn moved on without it'],
  ]);
});

test('a recent outcome replaces the waiting line where this window saw it wait', () => {
  const at = new Date(2026, 8, 9, 14, 2).toISOString();
  const places = new Map<string, string>();
  projectTurnsV2(snap(entries, { questions: [consent()] as never }), undefined, places);
  const outcome: QuestionOutcome = { kind: 'consent', token: '4', outcome: 'decided', words: 'Allow once', by: 'person', at };
  const s = { ...snap(entries), recentOutcomes: [outcome] };
  const { turns } = projectTurnsV2(s, undefined, places);
  assert.deepEqual(receipts(turns[0].blocks).map((r) => [r.state, r.text]), [['decided', 'Allow once · you · 14:02']]);
});

test('an outcome stays out of the flow when this window never saw its question', () => {
  const outcome: QuestionOutcome = { kind: 'consent', token: '4', outcome: 'decided', words: 'Allow once', by: 'person' };
  const { turns } = projectTurnsV2({ ...snap(entries), recentOutcomes: [outcome] } as never);
  assert.equal(turns.flatMap((t) => receipts(t.blocks)).length, 0);
});

test('a replayed outcome that names its call lands in the turn that asked', () => {
  const at = new Date(2026, 8, 9, 9, 25).toISOString();
  const outcome: QuestionOutcome = { kind: 'consent', token: '4', outcome: 'decided', words: 'Allow once', by: 'person', at, callId: 'c1' };
  const { turns } = projectTurnsV2({ ...snap(entries), recentOutcomes: [outcome] } as never);
  assert.deepEqual(receipts(turns[0].blocks).map((r) => [r.state, r.text]), [['decided', 'Allow once · you · 09:25']]);
  assert.equal(receipts(turns[1].blocks).length, 0);
});

test('live and replay put the same outcome in the same turn', () => {
  const outcome: QuestionOutcome = { kind: 'consent', token: '4', outcome: 'decided', words: 'Allow once', by: 'person', callId: 'c1' };
  const places = new Map<string, string>();
  projectTurnsV2(snap(entries, { questions: [consent()] as never }), undefined, places);
  const s = { ...snap(entries), recentOutcomes: [outcome] } as never;
  const live = projectTurnsV2(s, undefined, places).turns.map((t) => receipts(t.blocks).length);
  const replay = projectTurnsV2(s).turns.map((t) => receipts(t.blocks).length);
  assert.deepEqual(replay, live);
});

test('a withdrawn outcome speaks the engine words, or the plain wording without them', () => {
  const places = new Map([['fuel:2', 'f.jsonl:3'], ['ask:a1', 'f.jsonl:3']]);
  const outcomes: QuestionOutcome[] = [
    { kind: 'fuel', token: '2', outcome: 'withdrawn' },
    { kind: 'ask', token: 'a1', outcome: 'withdrawn', words: 'No longer needed — the task ended' },
  ];
  const { turns } = projectTurnsV2({ ...snap(entries), recentOutcomes: outcomes } as never, undefined, places);
  assert.deepEqual(receipts(turns[1].blocks).map((r) => r.text), ['No longer needed — the turn moved on', 'No longer needed — the task ended']);
});

test('a waiting call shows its step as waiting', () => {
  const { turns } = projectTurnsV2(snap(entries, { questions: [consent()] as never, running: true }));
  const work = turns[0].blocks[0];
  assert.equal(work.kind === 'work' && work.steps[0].state, 'waiting');
});
