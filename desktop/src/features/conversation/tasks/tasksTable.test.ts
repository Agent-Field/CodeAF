import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { EngineTaskRow } from '../../chat/engine-client';
import { buildTaskTree } from '../taskTree.ts';
import { costText, filterTree, groupCount, metaParts, stateWord, stripCounts, tabCounts, totals, waitReason } from './tasksTable.ts';

const rows: EngineTaskRow[] = [
  { ID: 'a', Title: 'Ship it', Status: 'running' },
  { ID: 'b', Title: 'Update fixtures', Status: 'running', Parent: 'a', Steps: 7, USD: 0.06, Model: 'deepseek/deepseek-v4.1-flash' },
  { ID: 'c', Title: 'Decide default', Status: 'paused', Parent: 'a' },
  { ID: 'd', Title: 'Write notes', Status: 'pending', Parent: 'a' },
  { ID: 'e', Title: 'Scan', Status: 'done', Parent: 'a' },
];

test('tab counts cover every row once', () => {
  assert.deepEqual(tabCounts(rows), { all: 5, needs: 1, running: 2, done: 1 });
});

test('the strip puts a waiting-on-you task in its own group', () => {
  assert.deepEqual(stripCounts(rows), { done: 1, running: 2, needs: 1, failed: 0, queued: 1 });
});

test('a filter hit keeps its ancestors, and a query narrows by title', () => {
  const tree = filterTree(buildTaskTree(rows), 'needs', '');
  assert.equal(tree[0].row.ID, 'a');
  assert.deepEqual(tree[0].children.map((node) => node.row.ID), ['c']);
  assert.deepEqual(filterTree(buildTaskTree(rows), 'all', 'FIXT')[0].children.map((node) => node.row.ID), ['b']);
  assert.deepEqual(filterTree(buildTaskTree(rows), 'done', 'fixt'), []);
});

test('group count is done of tasks under the head', () => {
  assert.equal(groupCount(buildTaskTree(rows)[0]), '1 of 4');
});

test('cost, steps and model appear only when the engine gave them', () => {
  assert.deepEqual(metaParts(rows[1]), ['deepseek-v4.1-flash', '7 steps', '$0.06']);
  assert.deepEqual(metaParts(rows[3]), []);
  assert.deepEqual(totals(rows), { usd: 0.06, steps: 7 });
  assert.equal(costText(0), '');
});

test('state words', () => {
  assert.equal(stateWord(rows[2]), 'Needs you');
  assert.equal(stateWord(rows[3]), 'Queued');
  assert.equal(waitReason('waits on X'), 'Waits on X');
  assert.equal(waitReason(''), '');
});
