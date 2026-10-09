import test from 'node:test';
import assert from 'node:assert/strict';
import { createQuestionFocus, QUESTION_FOCUS_TTL_MS } from './questionFocus.ts';

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
