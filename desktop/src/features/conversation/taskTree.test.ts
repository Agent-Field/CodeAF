import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { EngineTaskRow } from '../chat/engine-client';
import { buildTaskTree, taskCounts, taskTrail } from './taskTree.ts';

const row = (ID: string, Status: string, extra: Partial<EngineTaskRow> = {}): EngineTaskRow => ({
  ID,
  Title: ID,
  Status,
  ...extra,
});

test('nests children under their parent in row order', () => {
  const tree = buildTaskTree([row('a', 'running'), row('b', 'done', { Parent: 'a' }), row('c', 'ready', { Parent: 'a' })]);
  assert.equal(tree.length, 1);
  assert.deepEqual(tree[0].children.map((n) => n.row.ID), ['b', 'c']);
});

test('drops archived rows and promotes rows with missing parents', () => {
  const tree = buildTaskTree([row('a', 'done', { Archived: true }), row('b', 'done', { Parent: 'a' })]);
  assert.deepEqual(tree.map((n) => n.row.ID), ['b']);
});

test('a cycle does not hide rows or recurse forever', () => {
  const tree = buildTaskTree([row('a', 'ready', { Parent: 'b' }), row('b', 'ready', { Parent: 'a' })]);
  const ids = tree.flatMap((n) => [n.row.ID, ...n.children.map((c) => c.row.ID)]);
  assert.deepEqual(ids.sort(), ['a', 'b']);
});

test('counts every visible row, parents included, archived never', () => {
  const rows = [row('p', 'running'), row('x', 'done', { Parent: 'p' }), row('y', 'failed', { Parent: 'p' }), row('z', 'done')];
  assert.deepEqual(taskCounts(rows), { done: 2, total: 4 });
  assert.deepEqual(taskCounts([...rows, row('old', 'done', { Archived: true })]), { done: 2, total: 4 });
  assert.deepEqual(taskCounts([row('p', 'running'), row('c', 'running', { Parent: 'p' })]), { done: 0, total: 2 });
});

test('trail walks parents outermost first and stops on a cycle', () => {
  const rows = [
    { ID: 'a', Title: 'A', Status: 'done', Parent: 'c' },
    { ID: 'b', Title: 'B', Status: 'done', Parent: 'a' },
    { ID: 'c', Title: 'C', Status: 'done', Parent: 'b' },
    { ID: 'd', Title: 'D', Status: 'done' },
    { ID: 'e', Title: 'E', Status: 'done', Parent: 'd' },
  ];
  assert.deepEqual(taskTrail(rows, 'e').map((r) => r.ID), ['d', 'e']);
  assert.equal(taskTrail(rows, 'b').length, 3);
  assert.deepEqual(taskTrail(rows, 'missing'), []);
});
