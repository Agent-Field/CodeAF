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

test('a recent outcome replaces the waiting line with what was decided', () => {
  const at = new Date(2026, 8, 9, 14, 2).toISOString();
  const outcome: QuestionOutcome = { kind: 'consent', id: 4, callId: 'c1', state: 'decided', label: 'Allowed once', decidedBy: 'person', at };
  const s = { ...snap(entries, { questions: [consent()] as never }), recentOutcomes: [outcome] };
  const { turns } = projectTurnsV2(s);
  assert.deepEqual(receipts(turns[0].blocks).map((r) => [r.state, r.text]), [['decided', 'Allowed once · you · 14:02']]);
});

test('a withdrawn outcome without a reason gets the plain wording', () => {
  const outcome: QuestionOutcome = { kind: 'fuel', id: 2, state: 'withdrawn' };
  const { turns } = projectTurnsV2({ ...snap(entries), recentOutcomes: [outcome] } as never);
  assert.equal(receipts(turns[1].blocks)[0].text, 'No longer needed — the turn moved on');
});

test('a waiting call shows its step as waiting', () => {
  const { turns } = projectTurnsV2(snap(entries, { questions: [consent()] as never, running: true }));
  const work = turns[0].blocks[0];
  assert.equal(work.kind === 'work' && work.steps[0].state, 'waiting');
});
