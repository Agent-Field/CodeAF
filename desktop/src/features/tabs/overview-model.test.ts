import assert from 'node:assert/strict';
import { test } from 'node:test';
import { moveCursor, overviewOrder, overviewSections, searchPlaceholder, sectionPosition } from './overview-model.ts';
import type { Tab } from './types.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const groups = [{ id: 'g', title: 'Trailing commas', collapsed: false }, { id: 'h', title: 'Empty', collapsed: false }];
const tabs = [tab('a'), tab('p', { pinned: true }), tab('b', { groupId: 'g' }), tab('c', { groupId: 'g' }), tab('d')];
const hay = (t: Tab) => t.title;

test('sections run pinned, groups, other; empty sections drop', () => {
  const s = overviewSections(tabs, groups, '', hay);
  assert.deepEqual(s.map(x => [x.title, x.tabs.map(t => t.id)]), [['Pinned', ['p']], ['Trailing commas', ['b', 'c']], ['Other tabs', ['a', 'd']]]);
  assert.deepEqual(overviewOrder(s).map(t => t.id), ['p', 'b', 'c', 'a', 'd']);
});

test('the filter keeps matching tabs and drops sections left empty', () => {
  const s = overviewSections(tabs, groups, ' C ', hay);
  assert.deepEqual(s.map(x => [x.title, x.tabs.map(t => t.id)]), [['Trailing commas', ['c']]]);
  assert.deepEqual(overviewSections(tabs, groups, 'zzz', hay), []);
});

test('placeholder, cursor and position', () => {
  assert.equal(searchPlaceholder(18), 'Search 18 tabs');
  assert.equal(searchPlaceholder(1), 'Search 1 tab');
  const ids = ['a', 'b', 'c'];
  assert.equal(moveCursor(ids, 'c', 1), 'a');
  assert.equal(moveCursor(ids, 'a', -1), 'c');
  assert.equal(moveCursor(ids, 'gone', 1), 'a');
  assert.equal(moveCursor([], 'a', 1), undefined);
  const s = overviewSections(tabs, groups, '', hay);
  assert.equal(sectionPosition(s, 'c'), 'Trailing commas · 2 of 2');
  assert.equal(sectionPosition(s, 'zz'), '');
});

test('card state uses the running count when the engine sent it, and a task step only when that task sent one', async () => {
  const { overviewCardState, liveCommandLines } = await import('./overview-model.ts');
  const conversation = tab('a');
  const quiet = { title: '', firstLine: '', digest: '' };
  assert.deepEqual(overviewCardState(conversation, { a: { ...quiet, mark: 'working', running: 4 } }), { dot: 'accent', words: '4 running' });
  assert.deepEqual(overviewCardState(conversation, { a: { ...quiet, mark: 'working' } }), { dot: 'accent', words: 'Working' });
  assert.deepEqual(overviewCardState(conversation, { a: { ...quiet, mark: 'waiting' } }), { lead: 'amber', words: 'Needs you' });
  assert.deepEqual(overviewCardState(conversation, { a: { ...quiet, mark: 'failed' } }), { lead: 'danger', words: 'Failed' });
  assert.deepEqual(overviewCardState(conversation, {}), {});
  const task = tab('t', { kind: 'task', route: { taskId: 't7', back: [''], forward: [] } });
  assert.deepEqual(overviewCardState(task, { t: { ...quiet, taskState: { t7: 'Running' }, taskLive: { t7: { step: 7, command: 'go test ./internal/parse' } } } }), { dot: 'accent', words: 'step 7' });
  assert.deepEqual(overviewCardState(task, { t: { ...quiet, taskState: { t7: 'Done' }, taskLive: { t7: { step: 7 } } } }), { words: 'Done' });
  assert.deepEqual(liveCommandLines(task, { t: { ...quiet, taskLive: { t7: { command: 'go test ./internal/parse' } } } }), [{ text: '$ go test ./internal/parse', tone: 'ink' }]);
  assert.deepEqual(liveCommandLines(task, { t: quiet }), []);
  assert.deepEqual(liveCommandLines(conversation, { a: { ...quiet, taskLive: { t7: { command: 'go test' } } } }), []);
});

test('a background press is a middle press or a click with the platform primary modifier', async () => {
  const { isBackgroundPress } = await import('./overview-model.ts');
  const press = (over: Record<string, unknown> = {}) => ({ button: 0, metaKey: false, ctrlKey: false, ...over });
  assert.equal(isBackgroundPress(press(), true), false);
  assert.equal(isBackgroundPress(press({ metaKey: true }), true), true);
  assert.equal(isBackgroundPress(press({ ctrlKey: true }), true), false);
  assert.equal(isBackgroundPress(press({ ctrlKey: true }), false), true);
  assert.equal(isBackgroundPress(press({ metaKey: true }), false), false);
  assert.equal(isBackgroundPress(press({ button: 1 }), true), true);
  assert.equal(isBackgroundPress(press({ button: 1 }), false), true);
});
