import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { EngineTaskRow } from '../chat/engine-client';
import { branchCounts, buildTaskTree, foldDefault, holdsTask, splitFinished, taskCounts, taskProgress, taskTrail, waitTitles } from './taskTree.ts';

const row = (ID: string, Status: string, extra: Partial<EngineTaskRow> = {}): EngineTaskRow => ({
  ID,
  Title: ID,
  Status,
  ...extra,
});

test('nests children under their parent in row order', () => {
  const tree = buildTaskTree([row('a', 'running'), row('b', 'done', { Parent: 'a' }), row('c', 'pending', { Parent: 'a' })]);
  assert.equal(tree.length, 1);
  assert.deepEqual(tree[0].children.map((n) => n.row.ID), ['b', 'c']);
});

test('keeps ended-run rows, promotes rows with missing parents', () => {
  const tree = buildTaskTree([row('a', 'done', { Archived: true }), row('b', 'done', { Parent: 'z' })]);
  assert.deepEqual(tree.map((n) => n.row.ID), ['a', 'b']);
});

test('a repeated id shows once, as its newest record', () => {
  const rows = buildTaskTree([row('a', 'cancelled', { Archived: true }), row('a', 'running')]);
  assert.deepEqual(rows.map((n) => n.row.Status), ['running']);
});

test('a cycle does not hide rows or recurse forever', () => {
  const tree = buildTaskTree([row('a', 'ready', { Parent: 'b' }), row('b', 'ready', { Parent: 'a' })]);
  const ids = tree.flatMap((n) => [n.row.ID, ...n.children.map((c) => c.row.ID)]);
  assert.deepEqual(ids.sort(), ['a', 'b']);
});

test('counts every visible row once, parents included', () => {
  const rows = [row('p', 'running'), row('x', 'done', { Parent: 'p' }), row('y', 'failed', { Parent: 'p' }), row('z', 'done')];
  assert.deepEqual(taskCounts(rows), { done: 2, total: 4 });
  assert.deepEqual(taskCounts([...rows, row('old', 'done', { Archived: true })]), { done: 3, total: 5 });
  assert.deepEqual(taskCounts([row('z', 'done', { Archived: true }), ...rows]), { done: 2, total: 4 });
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

test('families with running work come first, at every level; otherwise store order', () => {
  const rows = [
    row('a', 'done'),
    row('b', 'ready'),
    row('b1', 'done', { Parent: 'b' }),
    row('b2', 'running', { Parent: 'b' }),
    row('c', 'running'),
  ];
  const tree = buildTaskTree(rows);
  assert.deepEqual(tree.map((n) => n.row.ID), ['b', 'c', 'a']);
  assert.deepEqual(tree[0].children.map((n) => n.row.ID), ['b2', 'b1']);
});

test('progress groups done, running, waiting on you, queued, failed', () => {
  const rows = [
    row('a', 'done'),
    row('b', 'running', { Tokens: 4000 }),
    row('c', 'pending'),
    row('d', 'paused'),
    row('e', 'failed'),
    row('f', 'running', { Stopped: true, Tokens: 500 }),
  ];
  assert.deepEqual(taskProgress(rows), { done: 1, running: 1, waiting: 1, queued: 1, failed: 2, total: 6, tokens: 4500 });
});

test('a parent counts what is done below it, and only whole done families leave for the fold', () => {
  const tree = buildTaskTree([
    row('p', 'running'),
    row('x', 'done', { Parent: 'p' }),
    row('y', 'running', { Parent: 'p' }),
    row('q', 'done'),
    row('q1', 'done', { Parent: 'q' }),
    row('r', 'done'),
    row('r1', 'failed', { Parent: 'r' }),
  ]);
  assert.deepEqual(branchCounts(tree[0]), { done: 1, total: 2 });
  const { live, finished, finishedCount } = splitFinished(tree);
  assert.deepEqual(live.map((n) => n.row.ID), ['p', 'r']);
  assert.deepEqual(finished.map((n) => n.row.ID), ['q']);
  assert.equal(finishedCount, 2);
  assert.equal(holdsTask(finished[0], 'q1'), true);
  assert.equal(holdsTask(finished[0], undefined), false);
});

test('a queued row names only the unfinished tasks it waits on', () => {
  const rows = [row('a', 'done', { Title: 'Parse config' }), row('b', 'running', { Title: 'Load schema' }), row('c', 'pending', { Waits: ['a', 'b', 'gone'] })];
  assert.deepEqual(waitTitles(rows, rows[2]), ['Load schema']);
});

test('the Finished fold opens by default only when nothing is live or it holds the open task', () => {
  const allDone = splitFinished(buildTaskTree([row('a', 'done'), row('b', 'done')]));
  assert.equal(foldDefault(allDone.live.length, false), true);
  const mixed = splitFinished(buildTaskTree([row('a', 'done'), row('b', 'running')]));
  assert.equal(foldDefault(mixed.live.length, false), false);
  assert.equal(foldDefault(mixed.live.length, true), true);
});
