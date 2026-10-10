import test from 'node:test';
import assert from 'node:assert/strict';
import { groupNotifications, UNPLACED_THREAD_ID, UNPLACED_THREAD_TITLE, type GroupAttention, type NotifyDirectory } from './notifyGroup.ts';

const question = (id: string, chatId = 'chat', extra: Partial<GroupAttention> = {}): GroupAttention =>
  ({ id, chatId, kind: 'question', ...extra });

const places = (members: NotifyDirectory['members'], nodes: NotifyDirectory['nodes']): NotifyDirectory =>
  ({ members, nodes });

const release = { id: 'pl_release', name: 'Release' };
const marketing = { id: 'pl_marketing', name: 'Marketing' };

test('one notification per question, sharing the place thread, with failures included', () => {
  const directory = places(
    [{ chatId: 'a', placeId: 'pl_release' }, { chatId: 'b', placeId: 'pl_release' }],
    [release],
  );
  const grouped = groupNotifications([
    question('a:1', 'a'),
    question('a:1', 'a'),
    question('a:2', 'a'),
    { id: 'failed:a', chatId: 'a', kind: 'failed' },
    { id: 'b:1', chatId: 'b', kind: 'approval' },
    { id: 'run', chatId: 'a', kind: 'running' },
    { id: 'done', chatId: 'a', kind: 'done' },
  ], directory);
  assert.deepEqual(grouped.map(notice => notice.id), ['a:1', 'a:2', 'failed:a', 'b:1']);
  assert.deepEqual(grouped.map(notice => notice.kind), ['needsYou', 'needsYou', 'failed', 'needsYou']);
  assert.ok(grouped.every(notice => notice.thread.id === 'pl_release' && notice.thread.title === 'Release'));
});

test('an unplaced chat, a missing graph and an unknown place all use Now', () => {
  const grouped = groupNotifications([
    question('bare'),
    question('unknown', 'other'),
  ], places([{ chatId: 'other', placeId: 'pl_gone' }], [release]));
  assert.deepEqual(grouped.map(notice => notice.thread), [
    { id: UNPLACED_THREAD_ID, title: UNPLACED_THREAD_TITLE },
    { id: UNPLACED_THREAD_ID, title: UNPLACED_THREAD_TITLE },
  ]);
  assert.equal(UNPLACED_THREAD_TITLE, 'Now');
  assert.deepEqual(groupNotifications([question('x')], undefined).map(notice => notice.thread.title), ['Now']);
});

test('the first live membership names the thread', () => {
  const directory = places(
    [
      { chatId: 'chat', placeId: 'pl_archived' },
      { chatId: 'chat', placeId: 'pl_missing' },
      { chatId: 'chat', placeId: 'pl_marketing' },
      { chatId: 'chat', placeId: 'pl_release' },
      { chatId: 'else', placeId: 'pl_release' },
    ],
    [release, marketing, { id: 'pl_archived', name: 'Old', archived: true }],
  );
  const [notice] = groupNotifications([question('q')], directory);
  assert.deepEqual(notice.thread, { id: 'pl_marketing', title: 'Marketing' });
});

test('a blank place name keeps the place id and omits the title', () => {
  const [notice] = groupNotifications(
    [question('q')],
    places([{ chatId: 'chat', placeId: 'pl_blank' }], [{ id: 'pl_blank', name: '   ' }]),
  );
  assert.deepEqual(notice.thread, { id: 'pl_blank' });
});

test('a non-blocking question is left out and an absent blocking flag is included', () => {
  const grouped = groupNotifications([
    question('quiet', 'chat', { blocking: false }),
    question('ask', 'chat'),
    { id: 'failed:chat', chatId: 'chat', kind: 'failed', blocking: false },
    { id: 'home', chatId: 'chat', kind: 'needsYou' },
    { id: '', chatId: 'chat', kind: 'question' },
  ]);
  assert.deepEqual(grouped.map(notice => notice.id), ['ask', 'failed:chat', 'home']);
});

test('a repeated place id keeps the first name', () => {
  const [notice] = groupNotifications(
    [question('q')],
    places([{ chatId: 'chat', placeId: 'pl_release' }], [release, { id: 'pl_release', name: 'Other' }]),
  );
  assert.equal(notice.thread.title, 'Release');
});
