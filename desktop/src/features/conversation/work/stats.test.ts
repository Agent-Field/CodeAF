import assert from 'node:assert/strict';
import { test } from 'node:test';
import { diffRows, diffStat, splitLines } from './diff.ts';
import { argPath, bashExit, editPairs, editStat, listCount, readLines, writeLines } from './stats.ts';

test('bashExit reads the failure footer only', () => {
  assert.equal(bashExit('boom\n\nCommand exited with code 2'), 2);
  assert.equal(bashExit('fine'), undefined);
});

test('readLines prefers the engine footer', () => {
  assert.equal(readLines('x\n[Showing lines 1-200 of 900]'), 200);
  assert.equal(readLines('a\nb\n'), 2);
  assert.equal(readLines(''), undefined);
});

test('listCount says zero for an honest empty result and nothing for no output', () => {
  assert.equal(listCount('No files found'), 0);
  assert.equal(listCount('a.ts\nb.ts'), 2);
  assert.equal(listCount(''), undefined);
});

test('splitLines treats a trailing newline as the end of the last line', () => {
  assert.deepEqual(splitLines('a\nb\n'), ['a', 'b']);
  assert.deepEqual(splitLines(''), []);
});

test('diffStat counts changed lines, not moved text', () => {
  assert.deepEqual(diffStat(['a', 'b', 'c'], ['a', 'x', 'y', 'c']), { added: 2, removed: 1 });
  assert.deepEqual(diffStat([], ['a']), { added: 1, removed: 0 });
});

test('editPairs reads the edits array', () => {
  const args = JSON.stringify({ path: 'a.ts', edits: [{ oldText: 'a', newText: 'b' }, { oldText: 'c', newText: 'd' }] });
  assert.equal(editPairs(args).length, 2);
});

test('editPairs reads an edits array sent as a JSON string', () => {
  const edits = JSON.stringify([{ oldText: 'a', newText: 'b' }]);
  assert.deepEqual(editPairs(JSON.stringify({ edits })), [{ oldText: 'a', newText: 'b' }]);
});

test('editPairs reads both legacy top-level spellings', () => {
  assert.deepEqual(editPairs('{"oldText":"a","newText":"b"}'), [{ oldText: 'a', newText: 'b' }]);
  assert.deepEqual(editPairs('{"old_string":"a","new_string":"b"}'), [{ oldText: 'a', newText: 'b' }]);
  assert.deepEqual(editPairs('{"edits":[{"old_string":"a","new_string":"b"}]}'), [{ oldText: 'a', newText: 'b' }]);
});

test('editPairs is empty for unparseable or empty args', () => {
  assert.deepEqual(editPairs('not json'), []);
  assert.deepEqual(editPairs(''), []);
});

test('editStat sums pairs', () => {
  const args = JSON.stringify({ edits: [{ oldText: 'a\nb', newText: 'a\nb\nc' }, { oldText: 'x', newText: 'y' }] });
  assert.deepEqual(editStat(args), { added: 2, removed: 1, capped: false });
});

test('editStat drops the cut marker and says it was capped', () => {
  const args = JSON.stringify({ edits: [{ oldText: 'a', newText: 'a\nb… (5000 more bytes)' }] });
  assert.deepEqual(editStat(args), { added: 1, removed: 0, capped: true });
});

test('writeLines counts content lines', () => {
  assert.equal(writeLines('{"path":"x","content":"a\\nb\\nc\\n"}'), 3);
  assert.equal(writeLines('{"path":"x"}'), undefined);
});

test('argPath finds the path', () => {
  assert.equal(argPath('{"path":"src/a.ts"}'), 'src/a.ts');
  assert.equal(argPath('{"file_path":"b.ts"}'), 'b.ts');
});

test('diffRows keeps two lines of context and folds the rest into a gap', () => {
  const before = ['1', '2', '3', '4', '5', '6', '7', '8', '9'].join('\n');
  const after = ['1', '2', '3', '4', 'five', '6', '7', '8', '9'].join('\n');
  const rows = diffRows([{ oldText: before, newText: after }]);
  assert.deepEqual(
    rows.map((row) => row.kind),
    ['gap', 'context', 'context', 'remove', 'add', 'context', 'context', 'gap'],
  );
});
