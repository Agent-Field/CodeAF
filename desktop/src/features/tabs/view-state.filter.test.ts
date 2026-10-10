import test from 'node:test';
import assert from 'node:assert/strict';
import { cleanView } from './view-state.ts';
import { sharedOf, compose, emptyLocal } from '../workspace-sync/shared.ts';
import { readWorkspace } from './model.ts';

test('only the four task filter values survive view validation and canonical composition', () => {
  for (const tasksFilter of ['all', 'needs', 'running', 'done'] as const) {
    assert.equal(cleanView({ tasksFilter }).tasksFilter, tasksFilter);
    const initial = readWorkspace();
    initial.tabs[0].tasksFilter = tasksFilter;
    assert.equal(compose(sharedOf(initial), emptyLocal()).tabs[0].tasksFilter, tasksFilter);
  }
  for (const tasksFilter of ['', 'queued', true, 2, null, {}, ['needs']]) assert.equal(cleanView({ tasksFilter }).tasksFilter, undefined);
});

test('saved task filters remain per tab and invalid stored values are dropped', () => {
  const before = globalThis.localStorage;
  const initial = readWorkspace();
  initial.tabs[0].tasksFilter = 'needs';
  initial.tabs.push({ ...initial.tabs[0], id: 'other', tasksFilter: 'running' });
  globalThis.localStorage = { getItem: () => JSON.stringify(initial) } as Storage;
  try {
    assert.deepEqual(readWorkspace().tabs.map(tab => tab.tasksFilter), ['needs', 'running']);
    (initial.tabs[1] as unknown as { tasksFilter: unknown }).tasksFilter = 'wrong';
    assert.deepEqual(readWorkspace().tabs.map(tab => tab.tasksFilter), ['needs', undefined]);
  } finally { globalThis.localStorage = before; }
});
