import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { EngineTaskRow } from '../../chat/engine-client';
import { noticeActions, type TaskCommands } from './taskMenu.ts';

const commands = (): TaskCommands & { opened: string[]; paused: string[]; resumed: string[]; stopped: string[] } => {
  const opened: string[] = [];
  const paused: string[] = [];
  const resumed: string[] = [];
  const stopped: string[] = [];
  return {
    opened, paused, resumed, stopped,
    onOpenTask: (id) => opened.push(id),
    onPause: (id) => paused.push(id),
    onResume: (id) => resumed.push(id),
    onStop: (id) => stopped.push(id),
  };
};

const labels = (rows: EngineTaskRow[], taskId: string, api = commands()) => noticeActions(taskId, rows, api).map((entry) => entry.label);

test('a running child notice offers Open in new tab, Pause and Stop', () => {
  const api = commands();
  const rows: EngineTaskRow[] = [{ ID: '2.2', Title: 'Port the form fields', Status: 'running', Parent: '2' }];
  assert.deepEqual(labels(rows, '2.2', api), ['Open in new tab', 'Pause', 'Stop']);
  noticeActions('2.2', rows, api).find((entry) => entry.id === 'pause')?.onSelect();
  assert.deepEqual(api.paused, ['2.2']);
});

test('a paused child offers Resume instead of Pause, and a finished task offers only Open in new tab', () => {
  const child: EngineTaskRow = { ID: '2.2', Title: 'Port the form fields', Status: 'paused', Parent: '2' };
  const done: EngineTaskRow = { ID: '2.1', Title: 'Read the current screen', Status: 'done', Parent: '2' };
  assert.deepEqual(labels([child], '2.2'), ['Open in new tab', 'Resume', 'Stop']);
  assert.deepEqual(labels([done], '2.1'), ['Open in new tab']);
});

test('a running root offers Stop and not Pause, matching the row', () => {
  const root: EngineTaskRow = { ID: '2', Title: 'Migrate the settings screen', Status: 'running' };
  assert.deepEqual(labels([root], '2'), ['Open in new tab', 'Stop']);
});

test('without a plan row the menu is only Open in new tab, and a later row wins', () => {
  const api = commands();
  assert.deepEqual(labels([], '2', api), ['Open in new tab']);
  noticeActions('2', [], api)[0].onSelect();
  assert.deepEqual(api.opened, ['2']);
  const stale: EngineTaskRow = { ID: '2.2', Title: 'Port the form fields', Status: 'running', Parent: '2' };
  const live: EngineTaskRow = { ID: '2.2', Title: 'Port the form fields', Status: 'paused', Parent: '2' };
  assert.deepEqual(labels([stale, live], '2.2'), ['Open in new tab', 'Resume', 'Stop']);
});
