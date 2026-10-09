import test from 'node:test';
import assert from 'node:assert/strict';
import { receiptOutcomes } from './outcomes.ts';
import type { QuestionOutcome, RichSnapshot } from './entry.ts';
import { snap } from './testkit.ts';

// Shapes from the snapshot's recentOutcomes (desktopbridge OutcomeWire), newest first.
const startedItself = (token: string): QuestionOutcome => ({
  kind: 'task',
  token,
  head: `Start task ${token}?`,
  outcome: 'withdrawn',
  words: 'No longer needed — it started on its own, as the card said it would',
  by: 'engine',
  at: '2026-10-09T13:24:10Z',
});
const picked: QuestionOutcome = { kind: 'ask', token: '9', outcome: 'decided', words: 'src/c.txt', by: 'person', at: '2026-10-09T13:25:00Z' };

const outcomes = (list: QuestionOutcome[]) =>
  (receiptOutcomes(snap([], { recentOutcomes: list } as never)) as RichSnapshot).recentOutcomes;

test('a proposal that went away leaves no receipt: the task notice and panel already name it', () => {
  assert.deepEqual(outcomes([startedItself('1'), startedItself('2'), picked]), [picked]);
});

test('each question is drawn once', () => {
  assert.deepEqual(outcomes([picked, { ...picked }]), [picked]);
});

test('a snapshot without outcomes is left as it is', () => {
  const plain = snap([]);
  assert.equal(receiptOutcomes(plain), plain);
});
