import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  FOCUS_HISTORY_CAP,
  backFocus,
  canBackFocus,
  canForwardFocus,
  currentFocus,
  emptyFocusHistory,
  forwardFocus,
  recordFocus,
  restoreFocusHistory,
  selfStartedFor,
  type FocusCause,
  type FocusEntry,
  type FocusHistory,
  type FocusMove,
  type FocusScrollSpot,
} from './model.ts';

const alive = (...ids: string[]) => new Set(ids);
const spot = (top: number, key = 'conversation'): FocusScrollSpot => ({ key, top, left: 0, end: false });

const move = (tabId: string, over: Partial<FocusMove> = {}): FocusMove => ({
  reason: 'tab',
  cause: 'own',
  windowPlace: 'now',
  tabId,
  ...over,
});

const record = (history: FocusHistory, tabId: string, over: Partial<FocusMove> = {}) => recordFocus(history, move(tabId, over));

const ids = (history: FocusHistory) => history.entries.map(entry => entry.tabId);

test('a tab change, a place switch and a drill-in are each a step', () => {
  let history = record(emptyFocusHistory(), 'home');
  history = record(history, 'chat');
  history = recordFocus(history, move('home', { reason: 'place', windowPlace: 'pl_aaaaaaaaaaaaaaaa', tabId: 'place-home' }));
  history = recordFocus(history, move('chat', { reason: 'drill', windowPlace: 'pl_aaaaaaaaaaaaaaaa', tabId: 'chat', drillPath: ['task-1'] }));
  assert.deepEqual(ids(history), ['home', 'chat', 'place-home', 'chat']);
  assert.deepEqual(currentFocus(history)?.drillPath, ['task-1']);
  assert.equal(currentFocus(history)?.windowPlace, 'pl_aaaaaaaaaaaaaaaa');
  assert.equal(history.cursor, 3);
});

test('opening in the background and restoring on relaunch do not record', () => {
  const history = record(emptyFocusHistory(), 'home');
  const background = recordFocus(history, move('quietly', { reason: 'background' }));
  const relaunch = recordFocus(history, move('restored', { reason: 'relaunch', windowPlace: 'pl_aaaaaaaaaaaaaaaa' }));
  assert.equal(background, history);
  assert.equal(relaunch, history);
  assert.deepEqual(ids(history), ['home']);
});

test('a new step truncates the forward tail and back returns along what remains', () => {
  let history = record(emptyFocusHistory(), 'a');
  history = record(history, 'b');
  history = record(history, 'c');
  const open = alive('a', 'b', 'c');
  history = backFocus(history, open);
  history = backFocus(history, open);
  assert.equal(currentFocus(history)?.tabId, 'a');
  assert.equal(canForwardFocus(history, open), true);
  history = record(history, 'd');
  assert.deepEqual(ids(history), ['a', 'd']);
  assert.equal(history.cursor, 1);
  assert.equal(canForwardFocus(history, open), false);
  assert.equal(forwardFocus(history, open), history);
});

test('the stack keeps 100 steps and drops the oldest', () => {
  let history = emptyFocusHistory();
  for (let i = 0; i < FOCUS_HISTORY_CAP; i++) history = record(history, `t${i}`);
  assert.equal(history.entries.length, FOCUS_HISTORY_CAP);
  assert.equal(history.entries[0].tabId, 't0');
  history = record(history, 'overflow');
  assert.equal(history.entries.length, FOCUS_HISTORY_CAP);
  assert.equal(history.entries[0].tabId, 't1');
  assert.equal(currentFocus(history)?.tabId, 'overflow');
  assert.equal(history.cursor, FOCUS_HISTORY_CAP - 1);
});

test('going back and recording drops the forward tail before the cap is applied', () => {
  let history = emptyFocusHistory();
  for (let i = 0; i < FOCUS_HISTORY_CAP; i++) history = record(history, `t${i}`);
  const open = alive(...history.entries.map(entry => entry.tabId));
  while (canBackFocus(history, open)) history = backFocus(history, open);
  history = record(history, 'fresh');
  assert.deepEqual(ids(history), ['t0', 'fresh']);
});

test('back and forward skip an entry whose tab is gone and leave it in the stack', () => {
  let history = record(emptyFocusHistory(), 'a');
  history = record(history, 'b');
  history = record(history, 'c');
  history = record(history, 'd');
  const open = alive('a', 'd');
  history = backFocus(history, open);
  assert.equal(currentFocus(history)?.tabId, 'a');
  assert.deepEqual(ids(history), ['a', 'b', 'c', 'd']);
  assert.equal(canForwardFocus(history, open), true);
  history = forwardFocus(history, open);
  assert.equal(currentFocus(history)?.tabId, 'd');
  // Focus may be sitting on a tab that has since closed. One back still leaves it for the nearest open tab.
  let closed = record(emptyFocusHistory(), 'a');
  closed = record(closed, 'b');
  closed = record(closed, 'c');
  assert.equal(currentFocus(backFocus(closed, alive('a')))?.tabId, 'a');
});

test('back does nothing when every earlier tab is gone', () => {
  let history = record(emptyFocusHistory(), 'gone');
  history = record(history, 'here');
  const stayed = backFocus(history, alive('here'));
  assert.equal(stayed, history);
  assert.equal(canBackFocus(history, alive('here')), false);
  assert.equal(forwardFocus(history, alive('here', 'gone')), history);
});

test('Next up, a notification and a cross-place link are not self-started', () => {
  const causes: FocusCause[] = ['own', 'next-up', 'notification', 'cross-place'];
  let history = emptyFocusHistory();
  for (const cause of causes) history = record(history, cause, { cause });
  assert.deepEqual(history.entries.map(entry => entry.selfStarted), causes.map(selfStartedFor));
  assert.deepEqual(history.entries.map(entry => entry.selfStarted), [true, false, false, false]);
});

test('a repeat of the current step refreshes scroll and draft and keeps the forward tail', () => {
  let history = record(emptyFocusHistory(), 'a', { scroll: [spot(10)], draftKey: 'a' });
  history = record(history, 'b');
  history = record(history, 'c');
  history = backFocus(history, alive('a', 'b', 'c'));
  const refreshed = record(history, 'b', { scroll: [spot(40)], draftKey: 'b:task' });
  assert.equal(refreshed.cursor, history.cursor);
  assert.deepEqual(ids(refreshed), ['a', 'b', 'c']);
  assert.deepEqual(currentFocus(refreshed)?.scroll, [spot(40)]);
  assert.equal(currentFocus(refreshed)?.draftKey, 'b:task');
  assert.equal(currentFocus(refreshed)?.selfStarted, true);
  const noted = record(refreshed, 'b', { scroll: [spot(80)] });
  assert.equal(currentFocus(noted)?.draftKey, 'b:task');
  assert.equal(currentFocus(noted)?.scroll[0].top, 80);
  const cleared = record(noted, 'b', { draftKey: null });
  assert.equal(currentFocus(cleared)?.draftKey, null);
  assert.equal(record(cleared, 'b', { scroll: [spot(80)] }), cleared);
});

test('a jump the person did not start stays marked after the step is current', () => {
  let history = record(emptyFocusHistory(), 'a');
  history = record(history, 'question', { cause: 'next-up', reason: 'tab' });
  assert.equal(currentFocus(history)?.selfStarted, false);
  history = record(history, 'question', { scroll: [spot(4)] });
  assert.equal(currentFocus(history)?.selfStarted, false);
  assert.equal(history.entries.length, 2);
});

test('an empty draft key and an unrestorable scroll spot store nothing', () => {
  const history = record(emptyFocusHistory(), 'a', {
    draftKey: '',
    scroll: [spot(Number.NaN), { key: '', top: 1, left: 0, end: false }, spot(-1), spot(12, 'task-page')],
  });
  const current = currentFocus(history);
  assert.equal(current?.draftKey, null);
  assert.deepEqual(current?.scroll, [spot(12, 'task-page')]);
});

test('the caller cannot change a stored path or spot afterwards', () => {
  const drillPath = ['task-1'];
  const scroll = [spot(3)];
  const history = record(emptyFocusHistory(), 'a', { reason: 'drill', drillPath, scroll });
  drillPath.push('later');
  scroll[0].top = 99;
  assert.deepEqual(currentFocus(history)?.drillPath, ['task-1']);
  assert.equal(currentFocus(history)?.scroll[0].top, 3);
});

test('a move with no tab or no place is not a step', () => {
  const history = record(emptyFocusHistory(), 'a');
  assert.equal(recordFocus(history, move('', { windowPlace: 'now' })), history);
  assert.equal(recordFocus(history, move('b', { windowPlace: '' })), history);
});

test('restore replaces the stack and a relaunch record still adds nothing', () => {
  const prior = record(record(emptyFocusHistory(), 'old'), 'older');
  const saved = {
    cursor: 1,
    entries: [
      { windowPlace: 'now', tabId: 'home', drillPath: [], scroll: [], draftKey: null, selfStarted: true },
      { windowPlace: 'pl_aaaaaaaaaaaaaaaa', tabId: 'chat', drillPath: ['task-1'], scroll: [spot(8)], draftKey: 'chat', selfStarted: false },
    ],
  };
  const restored = restoreFocusHistory(saved);
  assert.notEqual(restored, prior);
  assert.deepEqual(ids(restored), ['home', 'chat']);
  assert.equal(restored.cursor, 1);
  assert.equal(currentFocus(restored)?.selfStarted, false);
  assert.equal(currentFocus(restored)?.draftKey, 'chat');
  assert.equal(recordFocus(restored, move('nope', { reason: 'relaunch' })), restored);
});

test('restore drops a step that cannot be read, caps at the newest 100, and refuses a non-stack', () => {
  const entries: FocusEntry[] = [];
  for (let i = 0; i < FOCUS_HISTORY_CAP + 5; i++) {
    entries.push({ windowPlace: 'now', tabId: `t${i}`, drillPath: [], scroll: [], draftKey: null, selfStarted: true });
  }
  const restored = restoreFocusHistory({
    cursor: 2,
    entries: [entries[0], { tabId: 'missing-place' }, ...entries.slice(1)],
  });
  assert.equal(restored.entries.length, FOCUS_HISTORY_CAP);
  assert.equal(restored.entries[0].tabId, 't5');
  assert.equal(restored.cursor, 0);
  assert.deepEqual(restoreFocusHistory(null), emptyFocusHistory());
  assert.deepEqual(restoreFocusHistory({ entries: 'no' }), emptyFocusHistory());
  assert.deepEqual(restoreFocusHistory({ entries: [{ windowPlace: 'now', tabId: 'a', selfStarted: 'yes' }] }), emptyFocusHistory());
});
