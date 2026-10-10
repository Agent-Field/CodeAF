import test from 'node:test';
import assert from 'node:assert/strict';
import { projectTurnsV2 } from './project.ts';
import { entry, event, snap, user } from './testkit.ts';
import { emptyLive, reduceLive } from './live.ts';

test('structured decision, plan and remember asides retain record order on replay', () => {
  const decision = { kind: 'decision-receipt', text: 'Allowed', why: { by: 'Software', reversible: true } };
  const plan = { id: 'p1', steps: [], reachesBeyond: true };
  const snapshot = snap([user('Coordinate'),
    entry({ Role: 'aside', AsideKind: 'decision-receipt', Decision: decision }),
    entry({ Role: 'aside', AsideKind: 'plan', Plan: plan }),
    entry({ Role: 'aside', AsideKind: 'remember-line', Text: 'Remembered the rule', UndoReceipts: ['u1'] }),
  ]);
  const blocks = projectTurnsV2(snapshot).turns[0].blocks;
  assert.deepEqual(blocks.map(b => b.kind), ['decision-receipt', 'plan', 'remember-line']);
  assert.deepEqual(projectTurnsV2(JSON.parse(JSON.stringify(snapshot))).turns[0].blocks, blocks);
});

test('older asides without structured payload keep their literal note', () => {
  const { turns } = projectTurnsV2(snap([user('Go'), entry({ Role: 'aside', AsideKind: 'plan', Text: 'Plan unavailable' })]));
  assert.equal(turns[0].blocks[0].kind, 'work');
});


test('the canonical live plan event reaches the transcript once', () => {
  const plan = { id: 'p-live', steps: [{ kind: 'hold', target: { chat: 'c1' }, text: 'Hold launch' }], reachesBeyond: true };
  const live = reduceLive(emptyLive(), event('plan', { raw: { plan } }), 1, 100);
  const blocks = projectTurnsV2(snap([user('Coordinate')], { running: true }), live).turns[0].blocks;
  assert.deepEqual(blocks.filter(b => b.kind === 'plan').map(b => b.plan), [plan]);
  assert.equal(reduceLive(emptyLive(), event('plan', { raw: { plan: {} } }), 1, 100).plan, undefined);
});
