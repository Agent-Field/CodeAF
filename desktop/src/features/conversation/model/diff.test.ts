import test from 'node:test';
import assert from 'node:assert/strict';
import { argsOf } from './args.ts';
import { callStat, editPairs, editStat } from './diff.ts';

const json = (value: unknown) => JSON.stringify(value);

test('edit stat counts changed lines, not whole blocks', () => {
  const args = json({ path: 'a.go', edits: [{ oldText: 'a\nb\nc', newText: 'a\nB\nc\nd' }] });
  assert.deepEqual(editStat(argsOf(args)), { added: 2, removed: 1, capped: false });
});

test('a moved line is not a change twice over when it is kept', () => {
  const stat = editStat(argsOf(json({ edits: [{ oldText: 'x\ny', newText: 'x\ny\n' }] })));
  assert.deepEqual(stat, { added: 0, removed: 0, capped: false });
});

test('several replacements add up', () => {
  const edits = [
    { oldText: 'one', newText: 'uno' },
    { oldText: 'two\nthree', newText: 'dos' },
  ];
  assert.deepEqual(editStat(argsOf(json({ edits }))), { added: 2, removed: 3, capped: false });
});

test('legacy top-level pairs, both spellings', () => {
  assert.equal(editPairs(argsOf(json({ oldText: 'a', newText: 'b' }))).length, 1);
  assert.deepEqual(editPairs(argsOf(json({ old_string: 'a', new_string: 'b' }))), [{ old: 'a', next: 'b' }]);
});

test('edits sent as a JSON string are unwrapped once', () => {
  const edits = json([{ old_string: 'a', new_string: 'b\nc' }]);
  assert.deepEqual(editStat(argsOf(json({ path: 'x', edits }))), { added: 2, removed: 1, capped: false });
});

test('a malformed edits string yields no pairs and does not throw', () => {
  assert.deepEqual(editPairs(argsOf(json({ edits: 'not json' }))), []);
});

test('the cap sentence is not counted as a line and raises the capped flag', () => {
  const next = 'l1\nl2\nl3… (5000 more bytes)';
  const stat = editStat(argsOf(json({ edits: [{ oldText: '', newText: next }] })));
  assert.deepEqual(stat, { added: 3, removed: 0, capped: true });
});

test('write counts the lines of content; a trailing newline opens no line', () => {
  assert.deepEqual(callStat('write', json({ path: 'a', content: 'x\ny\nz\n' })), { added: 3, removed: 0, capped: false });
  assert.deepEqual(callStat('write', json({ path: 'a', content: '' })), { added: 0, removed: 0, capped: false });
});

test('write content cut by the engine is flagged capped', () => {
  const stat = callStat('write', json({ path: 'a', content: 'a\nb… (900 more bytes)' }));
  assert.deepEqual(stat, { added: 2, removed: 0, capped: true });
});

test('unparseable args are zero, not an error', () => {
  assert.deepEqual(callStat('edit', '{broken'), { added: 0, removed: 0, capped: false });
});
