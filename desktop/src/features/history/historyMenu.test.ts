import assert from 'node:assert/strict';
import { test } from 'node:test';
import { rowMenu } from './historyMenu.ts';
import type { HistoryItem } from './types.ts';

const item: HistoryItem = { id: 'chat', sessionFile: '/chat/transcript.jsonl', title: 'Chat', at: '', messages: 0, tasks: 0, tasksRunning: 0, files: [], fileCount: 0, decisions: 0, state: 'idle', open: true, archived: false };
const noop = () => {};
const labels = (archived: boolean) => rowMenu({ ...item, archived }, noop, noop, noop, noop).map(entry => entry.kind === 'separator' ? 'separator' : entry.label);

test('archived rows offer Unarchive and Delete…, open rows offer Archive', () => {
  assert.deepEqual(labels(false), ['Continue', 'Read', 'separator', 'Archive']);
  assert.deepEqual(labels(true), ['Continue', 'Read', 'separator', 'Unarchive', 'Delete…']);
});

test('each menu action receives its row and Delete… requests confirmation', () => {
  const archived = { ...item, archived: true };
  const calls: unknown[] = [];
  const entries = rowMenu(archived, (row, press) => calls.push(['continue', row, press]), row => calls.push(['read', row]), row => calls.push(['archive', row]), row => calls.push(['confirm', row]));
  for (const entry of entries) if (entry.kind !== 'separator' && entry.kind !== 'submenu') entry.onSelect();
  assert.deepEqual(calls, [['continue', archived, { newTab: false }], ['read', archived], ['archive', archived], ['confirm', archived]]);
});

test('absent capabilities leave no empty separator or unsupported action', () => {
  assert.deepEqual(rowMenu(item, noop).map(entry => entry.id), ['continue']);
  assert.deepEqual(rowMenu({ ...item, archived: true }, noop, noop, noop).map(entry => entry.id), ['continue', 'read', 'sep', 'archive']);
});

test('running and waiting rows keep Archive disabled', () => {
  for (const state of ['working', 'needs-you'] as const) {
    const entry = rowMenu({ ...item, state }, noop, noop, noop).find(entry => entry.id === 'archive');
    assert.equal(entry && 'disabled' in entry && entry.disabled, true);
  }
});
