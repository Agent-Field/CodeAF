import test from 'node:test';
import assert from 'node:assert/strict';
import { diffRows } from './diffRows.ts';

const hunk = (newStart: number, newLines: number, oldLines: number, lines: { kind: 'context' | 'add' | 'del'; old?: number; new?: number; text: string }[]) => ({ header: `@@ ${newStart} @@`, oldStart: newStart, oldLines, newStart, newLines, lines });

test('folds the unchanged gaps before, between and after hunks', () => {
  const diff = { lines: 100, hunks: [hunk(11, 2, 1, [{ kind: 'add', new: 11, text: 'a' }, { kind: 'add', new: 12, text: 'b' }]), hunk(50, 1, 1, [{ kind: 'context', old: 49, new: 50, text: 'c' }])] };
  const rows = diffRows(diff);
  assert.deepEqual(rows.map(r => r.type), ['fold', 'hunk', 'line', 'line', 'fold', 'hunk', 'line', 'fold']);
  assert.deepEqual(rows.filter(r => r.type === 'fold').map(r => r.type === 'fold' && r.count), [10, 37, 50]);
});

test('no fold between touching hunks or at the start of a file', () => {
  const diff = { lines: 4, hunks: [hunk(1, 4, 0, [{ kind: 'add', new: 1, text: 'x' }])] };
  assert.deepEqual(diffRows(diff).map(r => r.type), ['hunk', 'line']);
});

test('a pure deletion leaves the line it sits after unchanged', () => {
  const diff = { lines: 10, hunks: [hunk(5, 0, 2, [{ kind: 'del', old: 6, text: 'x' }, { kind: 'del', old: 7, text: 'y' }])] };
  const folds = diffRows(diff).filter(r => r.type === 'fold');
  assert.deepEqual(folds.map(r => r.type === 'fold' && r.count), [5, 5]);
});

test('an open fold becomes context lines numbered in both files', () => {
  const diff = { lines: 6, hunks: [hunk(4, 2, 1, [{ kind: 'add', new: 4, text: 'p' }, { kind: 'context', old: 4, new: 5, text: 'q' }])] };
  const text = ['l1', 'l2', 'l3', 'p', 'q', 'l6'];
  const rows = diffRows(diff, { 'before-0': true, after: true }, text);
  const lines = rows.filter(r => r.type === 'line').map(r => r.type === 'line' && [r.line.old, r.line.new, r.line.text]);
  assert.deepEqual(lines, [[1, 1, 'l1'], [2, 2, 'l2'], [3, 3, 'l3'], [undefined, 4, 'p'], [4, 5, 'q'], [5, 6, 'l6']]);
  assert.equal(diffRows(diff, { 'before-0': true }).some(r => r.type === 'fold' && r.id === 'before-0'), true, 'stays closed until the text is known');
});
