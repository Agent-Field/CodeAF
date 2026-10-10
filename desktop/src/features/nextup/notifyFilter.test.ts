import test from 'node:test';
import assert from 'node:assert/strict';
import { shouldNotify, totalNeedsYou, notificationAttention } from './notifyFilter.ts';
import { createNotificationWalk } from './notificationWalk.ts';

const item = (key: string, session = 'chat', extra = {}) => ({ key, session, kind: 'consent', id: 2, text: 'Allow?', sourceFolders: [], answerable: true, ...extra });

test('only blocking questions and failures notify; suggestions and decided items stay quiet', () => {
  for (const value of [item('old'), item('turn', 'chat', { blocking: { turn: true, tasks: [] } }), item('task', 'chat', { blocking: { turn: false, tasks: ['t1'] } }), { kind: 'failed', blocking: false }]) assert.equal(shouldNotify(value), true);
  for (const value of [item('suggestion', 'chat', { blocking: { turn: false, tasks: [] }, suggestion: { key: 'yes' } }), item('decided', 'chat', { decidedBy: 'Release' }), { kind: 'done' }, { kind: 'running' }, { kind: 'suggestion' }, { kind: 'approval', blocking: false }, { kind: 'ask', blocking: { turn: false } }]) assert.equal(shouldNotify(value), false);
});

test('dock total includes this conversation, nonblocking questions and disk-only needs, once each', () => {
  const items = [item('here', 'here'), item('here', 'here'), item('elsewhere', 'elsewhere', { blocking: { turn: false, tasks: [] } }), item('decided', 'here', { decidedBy: 'Release' }), item('archived', 'archived')];
  assert.equal(totalNeedsYou(items, [{ session: 'here', needsYou: true }, { session: 'elsewhere', needsYou: true }, { session: 'disk', needsYou: true }, { session: 'archived', needsYou: true, archived: true }]), 3);
  assert.equal(totalNeedsYou([], []), 0);
});

test('native adapter filters before posting and preserves failure identity', () => {
  assert.deepEqual(notificationAttention([item('q'), item('quiet', 'chat', { blocking: { turn: false, tasks: [] } })], [{ session: 'chat', failed: 1, failure: { at: 'landed' } }]).map(item => item.id), ['q', 'failed:chat:landed']);
});

test('notification starts at its item, includes the current chat, and advances when answered', () => {
  const walk = createNotificationWalk();
  const items = [item('first', 'other'), item('clicked', 'here')];
  walk.start({ chatId: 'here', itemId: 'clicked' }, items);
  assert.equal(walk.current()?.key, 'clicked');
  assert.deepEqual(walk.progress(), { index: 1, total: 2 });
  walk.update([items[0]]);
  assert.equal(walk.current()?.key, 'first');
  assert.deepEqual(walk.progress(), { index: 2, total: 2 });
  walk.update([]);
  assert.equal(walk.progress(), undefined);
  walk.start({ chatId: 'here', itemId: 'answered' }, items);
  assert.equal(walk.current(), undefined);
});

test('an asynchronous place read cannot label an older notification list with a newer cursor', async () => {
  const { createAttentionNotifications } = await import('../../lib/native/notify.ts');
  let seq = 4;
  let release;
  let began;
  const reading = new Promise(resolve => { began = resolve; });
  const directory = new Promise(resolve => { release = resolve; });
  const calls = [];
  const controller = createAttentionNotifications({
    attention: () => [{ id: 'q', chatId: 'chat', kind: 'question' }],
    rows: () => [{ chatId: 'chat', needsYou: 1, archived: false }],
    cursor: () => ({ seq, epoch: 'engine' }),
    lastPlaces: () => { began(); return directory; },
    subscribe: () => () => {},
  }, {
    focused: async () => true, permission: async () => true,
    post: async (_items, cursor) => { calls.push(cursor); },
    badge: async () => {},
  });
  const update = controller.update();
  await reading;
  seq = 5;
  release(undefined);
  await update;
  assert.deepEqual(calls, [4]);
  controller.stop();
});
