import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_PANES, MAX_RETIRED_PANES, MAX_SPOTS_PER_PANE, namedScrollMemoryKey, publicScrollName, ScrollMemory } from './scrollMemory.ts';

const spot = (top: number, over: Partial<{ left: number; end: boolean }> = {}) => ({ top, left: 0, end: false, ...over });

test('a spot keeps both offsets, rounded, and is read back as a copy', () => {
  const memory = new ScrollMemory();
  memory.set('a', 'root', spot(120.6, { left: 33.4 }));
  const copy = memory.get('a');
  copy.clear();
  assert.deepEqual([...memory.get('a')], [['root', { top: 121, left: 33, end: false }]]);
  assert.equal(memory.get('b').size, 0);
});

test('memory is bounded in panes and in spots per pane, dropping the least recently touched', () => {
  const memory = new ScrollMemory();
  for (let i = 0; i < MAX_PANES + 5; i++) memory.set(`p${i}`, 'root', spot(i));
  assert.equal(memory.size, MAX_PANES);
  assert.equal(memory.get('p0').size, 0);
  for (let i = 0; i < MAX_SPOTS_PER_PANE + 3; i++) memory.set('busy', `k${i}`, spot(i));
  assert.equal(memory.get('busy').size, MAX_SPOTS_PER_PANE);
  assert.equal(memory.get('busy').has('k0'), false);
});

test('a closed tab keeps its places for Reopen, and reopening takes it out of the ring', () => {
  const memory = new ScrollMemory();
  memory.set('keep', 'root', spot(1));
  memory.set('closed', 'root', spot(2, { end: true }));
  assert.equal(memory.prune(new Set(['keep'])), true);
  assert.equal(memory.get('closed').size, 1);
  assert.deepEqual(memory.retiredIds(), ['closed']);
  assert.equal(memory.prune(new Set(['keep'])), false);
  assert.equal(memory.prune(new Set(['keep', 'closed'])), true);
  assert.deepEqual(memory.retiredIds(), []);
  assert.equal(memory.get('closed').size, 1);
});

test('only the oldest retired panes beyond the ring are forgotten', () => {
  const memory = new ScrollMemory();
  for (let i = 0; i < MAX_RETIRED_PANES + 3; i++) memory.set(`c${i}`, 'root', spot(i));
  memory.set('open', 'root', spot(1));
  memory.prune(new Set(['open']));
  assert.equal(memory.retiredIds().length, MAX_RETIRED_PANES);
  assert.equal(memory.get('open').size, 1);
  assert.equal(memory.get('c0').size, 0);
  assert.equal(memory.get(`c${MAX_RETIRED_PANES + 2}`).size, 1);
});

test('when the workspace names its closed ring that ring is authoritative', () => {
  const memory = new ScrollMemory();
  for (const id of ['open', 'ringed', 'expired']) memory.set(id, 'root', spot(5));
  memory.prune(new Set(['open']));
  assert.equal(memory.prune(new Set(['open']), new Set(['ringed'])), true);
  assert.equal(memory.get('ringed').size, 1);
  assert.equal(memory.get('expired').size, 0);
  assert.deepEqual(memory.retiredIds(), ['ringed']);
});

test('a save round-trips, retired panes included, and a malformed one costs only its bad entries', () => {
  const memory = new ScrollMemory();
  memory.set('a', 'root', spot(40, { left: 7, end: true }));
  memory.set('gone', 'root', spot(9));
  memory.prune(new Set(['a']));
  const back = ScrollMemory.parse(memory.serialize());
  assert.deepEqual([...back.get('a')], [['root', { top: 40, left: 7, end: true }]]);
  assert.deepEqual(back.retiredIds(), ['gone']);
  assert.equal(ScrollMemory.parse('not json').size, 0);
  assert.equal(ScrollMemory.parse('[1,2]').size, 0);
  assert.equal(ScrollMemory.parse(JSON.stringify({ a: { root: { top: 1, end: false } } })).size, 0, 'an unversioned save is not read');
  const mixed = ScrollMemory.parse(JSON.stringify({ v: 2, panes: { a: { good: { top: 5, end: false }, neg: { top: -1, left: 0, end: false }, nan: { top: 'x', end: false }, flag: { top: 1 }, badLeft: { top: 1, left: -2, end: false } }, b: 7 }, retired: ['a', 'nope', 3] }));
  assert.deepEqual([...mixed.get('a').entries()], [['good', { top: 5, left: 0, end: false }]]);
  assert.equal(mixed.get('b').size, 0);
  assert.deepEqual(mixed.retiredIds(), ['a']);
});

test('a focus step names a scroller by its data-scroll-key, which is the memory key of the first one', () => {
  assert.equal(namedScrollMemoryKey('conversation'), 'k:conversation#0');
  assert.equal(publicScrollName('k:conversation#0'), 'conversation');
  assert.equal(namedScrollMemoryKey(''), undefined);
  assert.equal(namedScrollMemoryKey('k:conversation'), undefined);
  assert.equal(publicScrollName('k:conversation#1'), undefined);
  assert.equal(publicScrollName('s:div.conversation-scroll:ab#0'), undefined);
});
