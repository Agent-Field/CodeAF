import test from 'node:test';
import assert from 'node:assert/strict';
import { composerTarget } from './composerTarget.ts';
import type { Question } from './form.ts';

const review: Question = { kind: 'landing', id: 3, head: 'Parser', options: [{ key: 'a', label: 'Accept' }, { key: 'n', label: 'Not right' }, { key: 's', label: 'Tell it' }] };
test('review text is the Not right reason, with canonical identity', () => {
  const target = composerTarget({ ...review, ref: 'task-3' })!;
  assert.equal(target.placeholder, 'Tell it what to change');
  assert.deepEqual(target.answer(' Keep ABI '), { kind: 'landing', id: 3, ref: 'task-3', key: 'n', picked: ['n'], change: 'Keep ABI' });
});
test('clarifications and Other expose a word target; button-only cards do not', () => {
  assert.equal(composerTarget({ kind: 'ask', id: 4, head: 'Which region?' })!.answer('East').change, 'East');
  const other = composerTarget({ kind: 'ask', id: 5, head: 'Pick', options: [{ key: 'e', label: 'East' }, { key: 'other', label: 'Other' }] })!;
  assert.equal(other.answer('West').key, 'other');
  assert.equal(composerTarget({ kind: 'consent', id: 6, head: 'Allow?', options: [{ key: 'y', label: 'Allow' }, { key: 'n', label: 'Deny', safe: true }] }), null);
  assert.equal(composerTarget(undefined), null);
});
