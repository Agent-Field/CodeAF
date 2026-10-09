import test from 'node:test';
import assert from 'node:assert/strict';
import { createQuestionFocus, QUESTION_FOCUS_MAX, QUESTION_FOCUS_TTL_MS } from './questionFocus.ts';

const questions = [{ kind: 'consent', id: 7, ask: 'Run rm?' }, { kind: 'choice', id: 3, ask: 'Which branch?' }];

test('a clicked question is handed to its own conversation once, and to no other', () => {
  const focus = createQuestionFocus(() => 0);
  let heard = 0;
  const off = focus.subscribe(() => heard++);
  focus.request('s1', { kind: 'choice', id: 3 });
  assert.equal(heard, 1);
  assert.equal(focus.take('s2', questions), undefined, 'another conversation never takes it');
  assert.deepEqual(focus.take('s1', questions), questions[1]);
  assert.equal(focus.take('s1', questions), undefined, 'a click is spent once');
  off();
});

test('the request waits for the question to arrive from the engine', () => {
  const focus = createQuestionFocus(() => 0);
  focus.request('s1', { kind: 'consent', id: 7 });
  assert.equal(focus.take('s1', []), undefined);
  assert.equal(focus.take('s1', [{ kind: 'consent', id: 8 }]), undefined, 'the same kind with another id is another question');
  assert.deepEqual(focus.take('s1', questions), questions[0]);
});

test('an untaken request expires', () => {
  let now = 0;
  const focus = createQuestionFocus(() => now);
  focus.request('s1', { kind: 'consent', id: 7 });
  now = QUESTION_FOCUS_TTL_MS;
  assert.equal(focus.take('s1', questions), undefined);
});

test('a request for a conversation that never mounts expires without anyone asking for it', () => {
  let now = 0;
  const focus = createQuestionFocus(() => now);
  focus.request('never', { kind: 'consent', id: 1 });
  now = QUESTION_FOCUS_TTL_MS - 1;
  focus.request('s1', { kind: 'consent', id: 7 });
  assert.equal(focus.size(), 2);
  now = QUESTION_FOCUS_TTL_MS;
  // Only s1 is ever queried; the request for `never` goes anyway.
  assert.equal(focus.take('s1', []), undefined);
  assert.equal(focus.size(), 1);
  now = 2 * QUESTION_FOCUS_TTL_MS;
  focus.request('s2', { kind: 'choice', id: 3 });
  assert.equal(focus.size(), 1, 'a request sweeps the expired ones too');
});

test('requests are bounded: the oldest conversation waiting goes first, and a repeated click counts as new', () => {
  let now = 0;
  const focus = createQuestionFocus(() => now);
  for (let i = 0; i < QUESTION_FOCUS_MAX; i++) { now = i; focus.request(`c${i}`, { kind: 'consent', id: 1 }); }
  now = QUESTION_FOCUS_MAX;
  focus.request('c0', { kind: 'consent', id: 2 });
  focus.request('extra', { kind: 'consent', id: 1 });
  assert.equal(focus.size(), QUESTION_FOCUS_MAX);
  assert.equal(focus.take('c1', [{ kind: 'consent', id: 1 }]), undefined, 'the oldest untouched request was dropped');
  assert.deepEqual(focus.take('c0', [{ kind: 'consent', id: 2 }]), { kind: 'consent', id: 2 }, 'the repeated click survived with its newer question');
  assert.deepEqual(focus.take('extra', [{ kind: 'consent', id: 1 }]), { kind: 'consent', id: 1 });
});
