import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createNextUpWalk, type WalkOrigin } from './useNextUpWalk.ts';
import type { AttentionItem } from '../chat/world-client.ts';

const origin: WalkOrigin = { place: 'now', tabId: 'start', paneId: 'start', label: 'Config stack', conversation: 'here', draft: 'Keep my draft', scroll: new Map([['conversation', { top: 120, left: 0, end: false }]]) };
const item = (key: string, session = key, extra: Partial<AttentionItem> = {}): AttentionItem => ({ key, session, kind: 'choice', id: 1, text: key, sourceFolders: [], answerable: true, ...extra });
const items = [item('here', 'here'), item('a', 'a', { placeIds: ['marketing'] }), item('b', 'b', { placeIds: ['software'] }), item('c')];

test('pill and shortcut start elsewhere; targeted popover, banner and notification start at their item', () => {
 for (const key of [undefined, 'b', 'here']) {
  const walk = createNextUpWalk();
  walk.start(origin, items, key);
  assert.equal(walk.getSnapshot().item?.key, key ?? 'a');
  assert.deepEqual(walk.getSnapshot().progress, { index: 1, total: key === 'here' ? 4 : 3 });
  assert.equal(walk.getSnapshot().origin, origin);
 }
});

test('acknowledged answer advances across places, ignores stale feed and counts each answer once', () => {
 const walk = createNextUpWalk();
 walk.start(origin, items);
 walk.acknowledge('a', { kind: 'choice', id: 1 }, items);
 assert.equal(walk.getSnapshot().item?.placeIds?.[0], 'software');
 assert.deepEqual(walk.getSnapshot().progress, { index: 2, total: 3 });
 assert.equal(walk.getSnapshot().answered, 1);
 walk.update(items);
 walk.acknowledge('a', { kind: 'choice', id: 1 }, items);
 assert.equal(walk.getSnapshot().answered, 1);
 walk.acknowledge('b', { kind: 'choice', id: 1 }, items);
 walk.acknowledge('c', { kind: 'choice', id: 1 }, items);
 assert.equal(walk.getSnapshot().phase, 'clear');
 assert.equal(walk.getSnapshot().answered, 3);
});

test('Skip always moves current to the back, including a second skip of the same question', () => {
 const walk = createNextUpWalk();
 walk.start(origin, items);
 for (const key of ['b', 'c', 'a', 'b']) {
  walk.skip(items);
  assert.equal(walk.getSnapshot().item?.key, key);
 }
 assert.equal(walk.getSnapshot().answered, 0);
});

test('vanished or automatically decided items drop in one update with no empty intermediate state', () => {
 const walk = createNextUpWalk();
 walk.start(origin, items);
 const seen: string[] = [];
 walk.subscribe(() => seen.push(walk.getSnapshot().item?.key ?? 'clear'));
 walk.update([item('c'), item('b', 'b', { decidedBy: 'place' })]);
 assert.deepEqual(seen, ['c']);
 assert.equal(walk.getSnapshot().answered, 0);
 walk.update([]);
 assert.deepEqual(seen, ['c', 'clear']);
});

test('feed removal before answer acknowledgement still records the real answer', () => {
 const walk = createNextUpWalk();
 walk.start(origin, [item('a')]);
 walk.update([]);
 walk.acknowledge('a', { kind: 'choice', id: 1 }, []);
 assert.equal(walk.getSnapshot().answered, 1);
 assert.equal(walk.getSnapshot().phase, 'clear');
});

test('exit preserves the original place, tab, scroll and draft through jumps and resets only after return', () => {
 const walk = createNextUpWalk();
 walk.start(origin, items);
 walk.start({ ...origin, place: 'marketing', tabId: 'a' }, items, 'b');
 walk.exit();
 assert.equal(walk.getSnapshot().origin, origin);
 assert.equal(walk.getSnapshot().phase, 'returning');
 walk.update([]);
 assert.equal(walk.getSnapshot().phase, 'returning');
 walk.returned();
 assert.deepEqual(walk.getSnapshot(), { phase: 'idle', answered: 0 });
});

test('stale target and zero queue do not navigate or flash an end card', () => {
 const walk = createNextUpWalk();
 walk.start(origin, items, 'gone');
 assert.equal(walk.getSnapshot().phase, 'idle');
 walk.start(origin, [item('here', 'here')]);
 assert.equal(walk.getSnapshot().phase, 'idle');
});
