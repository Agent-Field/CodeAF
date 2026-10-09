import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { EngineTaskRow } from '../chat/engine-client';
import { buildTaskTree, taskCounts } from './taskTree.ts';

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

test('counts leaves only', () => {
  const rows = [row('p', 'running'), row('x', 'done', { Parent: 'p' }), row('y', 'failed', { Parent: 'p' }), row('z', 'done')];
  assert.deepEqual(taskCounts(rows), { done: 2, total: 3 });
});
