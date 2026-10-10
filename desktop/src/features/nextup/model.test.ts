import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { AttentionItem, AttentionStakes } from '../chat/world-client.ts';
import { nextUpProgressText, nextUpQueue, type NextUpInput } from './model.ts';

const t0 = '2026-10-10T00:00:00.000Z';
const t1 = '2026-10-10T00:01:00.000Z';
const t2 = '2026-10-10T00:02:00.000Z';
const t3 = '2026-10-10T00:03:00.000Z';
const t4 = '2026-10-10T00:04:00.000Z';

const suggestion = { key: 'use-a', label: 'Use A', percent: 92, reason: 'matches the last answer' };

function item(key: string, over: Partial<AttentionItem> = {}): AttentionItem {
  return {
    key,
    session: 'elsewhere',
    kind: 'ask',
    text: key,
    sourceFolders: [],
    answerable: true,
    ...over,
    key,
  };
}

const keys = (input: NextUpInput) => nextUpQueue(input).items.map(row => row.key);

test('order table: blocking first, irreversible last, oldest first inside a band', () => {
  // Listed in the order the feed happened to send, which is not the walk order.
  const items = [
    item('irrev-block', { stakes: 'irreversible', blocking: { turn: true, tasks: ['wipe'] }, asked: t0 }),
    item('mid-late', { stakes: 'reversible', blocking: { turn: false, tasks: [] }, asked: t3, suggestion }),
    item('block-late', { stakes: 'costly', blocking: { turn: true, tasks: [] }, asked: t2 }),
    item('block-early', { stakes: 'reversible', blocking: { turn: false, tasks: ['port'] }, asked: t0, suggestion }),
    item('mid-early', { blocking: { turn: false, tasks: [] }, asked: t1 }),
    item('block-tied', { blocking: { turn: true, tasks: [] }, asked: t0 }),
    item('legacy', { asked: t1 }),
    item('block-notime', { blocking: { turn: true, tasks: [] } }),
    item('block-badtime', { blocking: { turn: true, tasks: [] }, asked: 'not-a-time' }),
    item('irrev-late', { stakes: 'irreversible', blocking: { turn: false, tasks: [] }, asked: t4 }),
    item('blank-task', { blocking: { turn: false, tasks: [''] }, asked: t0, stakes: 'reversible' }),
  ];
  const queue = nextUpQueue({ items, conversationKey: 'here' });
  assert.deepEqual(keys({ items }), [
    'block-early', // t0, names a task, reversible
    'block-tied', // same instant, later in the feed, so it stays second
    'legacy', // an older feed omits blocking and is read as blocking the turn
    'block-late',
    'block-notime', // no asked time follows every timed blocking question
    'block-badtime', // a time that does not parse is the same as no time
    'blank-task', // a blank task name does not count as blocking; t0 puts it first in the middle
    'mid-early',
    'mid-late',
    'irrev-block', // blocking and irreversible still comes last
    'irrev-late',
  ]);
  assert.equal(queue.count, 11);
  assert.deepEqual(queue.progress, { index: 1, total: 11 });
  assert.equal(queue.progressText, '1 of 11');
  assert.equal(nextUpProgressText(queue.progress), '1 of 11');
  // Accept N is reversible and already has a suggestion, in walk order.
  assert.deepEqual(queue.acceptable.map(row => row.key), ['block-early', 'mid-late']);
});

test('a costly suggestion and a reversible question with no suggestion are not acceptable', () => {
  const items = [
    item('costly', { stakes: 'costly', suggestion, blocking: { turn: false, tasks: [] }, asked: t0 }),
    item('bare', { stakes: 'reversible', blocking: { turn: false, tasks: [] }, asked: t1 }),
    item('empty-key', {
      stakes: 'reversible',
      blocking: { turn: false, tasks: [] },
      asked: t2,
      suggestion: { key: '', label: 'Use A', percent: 90, reason: 'because' },
    }),
    item('irrev', { stakes: 'irreversible', suggestion, blocking: { turn: false, tasks: [] }, asked: t3 }),
    item('yes', { stakes: 'reversible' as AttentionStakes, suggestion, blocking: { turn: true, tasks: [] }, asked: t0 }),
  ];
  const queue = nextUpQueue({ items });
  assert.deepEqual(queue.acceptable.map(row => row.key), ['yes']);
});

test('skip twice sends each front question to the back, in skip order', () => {
  const items = [
    item('a', { asked: t0, blocking: { turn: true, tasks: [] } }),
    item('b', { asked: t1, blocking: { turn: true, tasks: [] } }),
    item('c', { asked: t2, blocking: { turn: true, tasks: [] } }),
  ];
  assert.deepEqual(keys({ items }), ['a', 'b', 'c']);
  const once = nextUpQueue({ items, skipped: ['a'] });
  assert.deepEqual(once.items.map(row => row.key), ['b', 'c', 'a']);
  assert.equal(once.progressText, '1 of 3');
  const twice = nextUpQueue({ items, skipped: ['a', 'b'] });
  assert.deepEqual(twice.items.map(row => row.key), ['c', 'a', 'b']);
  assert.deepEqual(twice.progress, { index: 1, total: 3 });
  // Skip does not take a reversible suggestion out of Accept N. It only moves the walk.
  const suggested = item('a', {
    asked: t0,
    stakes: 'reversible',
    suggestion,
    blocking: { turn: true, tasks: [] },
  });
  const accepted = nextUpQueue({ items: [suggested, items[1], items[2]], skipped: ['a'] });
  assert.deepEqual(accepted.items.map(row => row.key), ['b', 'c', 'a']);
  assert.deepEqual(accepted.acceptable.map(row => row.key), ['a']);
});

test('the conversation on screen is excluded from the count, the walk and Accept N', () => {
  const items = [
    item('here-q', {
      session: 'here',
      asked: t0,
      stakes: 'reversible',
      suggestion,
      blocking: { turn: true, tasks: [] },
    }),
    item('away', { session: 'away', asked: t2, blocking: { turn: false, tasks: [] } }),
    item('also', { session: 'also', asked: t1, stakes: 'reversible', suggestion, blocking: { turn: true, tasks: [] } }),
  ];
  const queue = nextUpQueue({ items, conversationKey: 'here', skipped: ['here-q'] });
  assert.deepEqual(queue.items.map(row => row.key), ['also', 'away']);
  assert.equal(queue.count, 2);
  assert.equal(queue.progressText, '1 of 2');
  assert.deepEqual(queue.acceptable.map(row => row.key), ['also']);
  // An empty key is not a conversation, so nothing is dropped for matching it.
  assert.equal(nextUpQueue({ items, conversationKey: '' }).count, 3);
});

test('zero items is an empty queue and draws no progress', () => {
  const queue = nextUpQueue({ items: [] });
  assert.deepEqual(queue.items, []);
  assert.equal(queue.count, 0);
  assert.equal(queue.progress, undefined);
  assert.equal(queue.progressText, '');
  assert.deepEqual(queue.acceptable, []);
  assert.equal(nextUpProgressText(undefined), '');
  const onlyHere = nextUpQueue({
    items: [item('here-q', { session: 'here' })],
    conversationKey: 'here',
  });
  assert.equal(onlyHere.count, 0);
  assert.equal(onlyHere.progressText, '');
});

test('a question answered elsewhere is dropped, including one that had been skipped', () => {
  const items = [
    item('a', { asked: t0, blocking: { turn: true, tasks: [] } }),
    item('b', { asked: t1, blocking: { turn: true, tasks: [] } }),
    item('c', { asked: t2, stakes: 'reversible', suggestion, blocking: { turn: false, tasks: [] } }),
  ];
  const before = nextUpQueue({ items, skipped: ['a'] });
  assert.deepEqual(before.items.map(row => row.key), ['b', 'c', 'a']);
  // The feed no longer lists b: another window answered it. The skip list still names it.
  const after = nextUpQueue({
    items: items.filter(row => row.key !== 'b'),
    skipped: ['b', 'a'],
  });
  assert.deepEqual(after.items.map(row => row.key), ['c', 'a']);
  assert.equal(after.count, 2);
  assert.equal(after.progressText, '1 of 2');
  assert.deepEqual(after.acceptable.map(row => row.key), ['c']);
  assert.equal(after.items.some(row => row.key === 'b'), false);
});

test('a repeated key keeps the first question, and a blank key is not a question', () => {
  const first = item('a', { session: 'one', asked: t1, blocking: { turn: false, tasks: [] } });
  const again = item('a', { session: 'two', asked: t0, blocking: { turn: true, tasks: [] } });
  const blank = item('kept', { asked: t0 });
  blank.key = '';
  const queue = nextUpQueue({ items: [first, again, blank, item('b', { asked: t0, blocking: { turn: true, tasks: [] } })] });
  assert.equal(queue.items.find(row => row.key === 'a')?.session, 'one');
  assert.deepEqual(queue.items.map(row => row.key), ['b', 'a']);
});

test('building the queue does not reorder the list it was given', () => {
  const items = [
    item('z', { asked: t2, blocking: { turn: true, tasks: [] } }),
    item('a', { asked: t0, blocking: { turn: true, tasks: [] } }),
  ];
  const skipped = ['a'];
  nextUpQueue({ items, skipped });
  assert.deepEqual(items.map(row => row.key), ['z', 'a']);
  assert.deepEqual(skipped, ['a']);
});
