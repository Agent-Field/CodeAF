import test from 'node:test';
import assert from 'node:assert/strict';
import { projectTurnsV2 } from './project.ts';
import { final, narrate, snap, tool, user } from './testkit.ts';

const entries = [user('clean'), narrate('Removing the build.'), tool('bash', 'c1', { command: 'rm -rf build' }), tool('read', 'c2', { path: 'a.go' }), final('done')];
const outcome = (callId: string, words: string, outcomeKind = 'decided') => ({ kind: 'consent', token: callId, outcome: outcomeKind, words, by: 'person', callId });
const callsOf = (extra: object) => {
  const { turns } = projectTurnsV2(snap(entries, extra as never));
  const work = turns[0].blocks.flatMap((b) => (b.kind === 'work' ? b.steps.flatMap((s) => s.calls) : []));
  return Object.fromEntries(work.map((c) => [c.callId, c.decision]));
};

test('a call the person decided on carries the decision in the past tense', () => {
  assert.deepEqual(callsOf({ recentOutcomes: [outcome('c1', 'Allow once')] }), { c1: 'allowed once', c2: undefined });
  assert.deepEqual(callsOf({ recentOutcomes: [outcome('c1', 'Always')] }), { c1: 'always allowed', c2: undefined });
  assert.deepEqual(callsOf({ recentOutcomes: [outcome('c1', 'Deny')] }), { c1: 'denied', c2: undefined });
});

test('words with no known past tense, withdrawn questions and outcomes about no call say nothing', () => {
  assert.deepEqual(callsOf({ recentOutcomes: [outcome('c1', 'Pick the second one')] }), { c1: undefined, c2: undefined });
  assert.deepEqual(callsOf({ recentOutcomes: [outcome('c1', 'Allow once', 'withdrawn')] }), { c1: undefined, c2: undefined });
  assert.deepEqual(callsOf({ recentOutcomes: [{ ...outcome('c1', 'Allow once'), callId: undefined }] }), { c1: undefined, c2: undefined });
});
