import test from 'node:test';
import assert from 'node:assert/strict';
import { createAttentionNotifications, dispatchAttentionFocus } from './notify.ts';

function harness(focused = false) {
 let items = [], rows = [], places;
 let listener;
 const posted = [], badges = [], requests = [];
 let granted = true;
 let permissionWait;
 let asked;
 const permissionRequested = new Promise(resolve => { asked = resolve; });
 const source = {
  attention: () => items, rows: () => rows, lastPlaces: () => places,
  cursor: () => ({ seq: 4, epoch: 'engine' }),
  subscribe: callback => { listener = callback; return () => { listener = undefined; }; },
 };
 const controller = createAttentionNotifications(source, {
  focused: async () => focused,
  permission: async ask => { requests.push(ask); asked(); return permissionWait ?? granted; },
  post: async pending => { posted.push(...pending.filter(item => !item.silent)); },
  badge: async count => { badges.push(count); },
 });
 return {
  controller, posted, badges, requests,
  update: async (next, count = next.filter(item => ['question', 'approval'].includes(item.kind)).length) => {
   items = next; rows = [{ chatId: 'chat', needsYou: count, archived: false }];
   await controller.update();
  },
  setFocus: value => { focused = value; },
  setPermission: value => { granted = value; },
  setPermissionWait: value => { permissionWait = value; },
  permissionRequested,
  setPlaces: value => { places = value; },
  subscribed: () => Boolean(listener),
 };
}
const item = (id = 'question', kind = 'question') => ({ id, kind, chatId: 'chat', title: 'Ship desktop', head: 'Which branch?' });

test('a focused app posts nothing and old questions stay quiet after blur', async () => {
 const h = harness(true);
 await h.update([item()]);
 h.setFocus(false);
 await h.update([item()]);
 assert.deepEqual(h.posted, []);
 assert.deepEqual(h.requests, []);
});
test('each new question or failure posts once, including after withdrawal and replay', async () => {
 const h = harness();
 await Promise.all([h.update([item('a'), item('b', 'failed')]), h.update([item('a'), item('b', 'failed')])]);
 await h.update([]);
 await h.update([item('a'), item('b', 'failed'), item('c', 'approval')]);
 assert.deepEqual(h.posted.map(value => value.id), ['a', 'b', 'c']);
});
test('done, running and nonblocking questions never notify', async () => {
 const h = harness();
 await h.update([item('done', 'done'), item('run', 'running'), { ...item('ask'), blocking: false }]);
 assert.deepEqual(h.posted, []);
 assert.deepEqual(h.requests, []);
});
test('badge counts engine needs-you totals, excludes failures and clears at zero', async () => {
 const h = harness();
 await h.update([item(), item('failed', 'failed')], 7);
 await h.update([item('failed', 'failed')], 0);
 assert.deepEqual(h.badges, [7, 0]);
});
test('permission is requested once on first background need', async () => {
 const h = harness(); h.setPermission(false);
 await h.update([item()]);
 await h.update([item('another')]);
 assert.deepEqual(h.requests, [false, true]);
 assert.deepEqual(h.posted, []);
});
test('an unplaced chat is grouped under Now and a failure keeps that place', async () => {
 const unplaced = harness();
 await unplaced.update([item('q'), item('failed:chat', 'failed')]);
 assert.equal(unplaced.posted[0].placeId, 'now');
 assert.equal(unplaced.posted[0].placeName, 'Now');
 assert.equal(unplaced.posted[1].placeName, 'Now');
 const placed = harness();
 placed.setPlaces({
  members: [{ chatId: 'chat', placeId: 'pl_1' }, { chatId: 'chat', placeId: 'pl_2' }],
  nodes: [{ id: 'pl_1', name: 'Release', archived: true }, { id: 'pl_2', name: 'Marketing' }],
 });
 await placed.update([item('q'), item('failed:chat', 'failed')]);
 assert.equal(placed.posted[0].placeId, 'pl_2');
 assert.equal(placed.posted[0].placeName, 'Marketing');
 assert.equal(placed.posted[1].placeName, 'Marketing');
});
test('place identity and real words are handed to native delivery without invented content', async () => {
 const h = harness();
 h.setPlaces({ members: [{ chatId: 'chat', placeId: 'pl_1' }], nodes: [{ id: 'pl_1', name: 'Release' }] });
 await h.update([item()]);
 assert.equal(h.posted[0].placeId, 'pl_1');
 assert.equal(h.posted[0].placeName, 'Release');
 assert.equal(h.posted[0].chatTitle, 'Ship desktop');
 assert.equal(h.posted[0].text, 'Which branch?');
});
test('stop disconnects and cancels queued native effects', async () => {
 const h = harness(); h.controller.start();
 assert.equal(h.subscribed(), true);
 h.controller.stop();
 await h.update([item()]);
 assert.equal(h.subscribed(), false);
 assert.deepEqual(h.posted, []);
});

test('a notification click dispatches the exact attention item identity', () => {
 const target = new EventTarget();
 let detail;
 target.addEventListener('codeaf:focus-attention', event => { detail = event.detail; });
 dispatchAttentionFocus('chat:approval:7', target);
 assert.deepEqual(detail, { itemId: 'chat:approval:7' });
});

test('a withdrawn question stays quiet while permission is pending', async () => {
 const h = harness();
 let release;
 h.setPermissionWait(new Promise(resolve => { release = resolve; }));
 const initial = h.update([item()]);
 await h.permissionRequested;
 const withdrawn = h.update([]);
 release(true);
 await Promise.all([initial, withdrawn]);
 assert.deepEqual(h.posted, []);
});
