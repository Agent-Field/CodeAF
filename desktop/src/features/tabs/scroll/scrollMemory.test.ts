import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_PANES, MAX_SPOTS_PER_PANE, ScrollMemory } from './scrollMemory.ts';

test('a spot is kept per pane and key, rounded, and read back as a copy', () => {
  const memory = new ScrollMemory();
  memory.set('a', 'root', { top: 120.6, end: false });
  const copy = memory.get('a');
  copy.clear();
  assert.deepEqual([...memory.get('a')], [['root', { top: 121, end: false }]]);
  assert.equal(memory.get('b').size, 0);
});

test('memory is bounded in panes and in spots per pane, dropping the least recently touched', () => {
  const memory = new ScrollMemory();
  for (let i = 0; i < MAX_PANES + 5; i++) memory.set(`p${i}`, 'root', { top: i, end: false });
  assert.equal(memory.size, MAX_PANES);
  assert.equal(memory.get('p0').size, 0);
  assert.equal(memory.get(`p${MAX_PANES + 4}`).size, 1);
  for (let i = 0; i < MAX_SPOTS_PER_PANE + 3; i++) memory.set('busy', `k${i}`, { top: i, end: false });
  assert.equal(memory.get('busy').size, MAX_SPOTS_PER_PANE);
  assert.equal(memory.get('busy').has('k0'), false);
});

test('closing a tab forgets its panes only', () => {
  const memory = new ScrollMemory();
  memory.set('keep', 'root', { top: 1, end: false });
  memory.set('gone', 'root', { top: 2, end: true });
  assert.equal(memory.prune(new Set(['keep'])), true);
  assert.equal(memory.get('gone').size, 0);
  assert.equal(memory.prune(new Set(['keep'])), false);
  assert.equal(memory.get('keep').size, 1);
});

test('a save round-trips and a malformed one costs only its bad entries', () => {
  const memory = new ScrollMemory();
  memory.set('a', 'root', { top: 40, end: true });
  assert.deepEqual([...ScrollMemory.parse(memory.serialize()).get('a')], [['root', { top: 40, end: true }]]);
  assert.equal(ScrollMemory.parse('not json').size, 0);
  assert.equal(ScrollMemory.parse('[1,2]').size, 0);
  const mixed = ScrollMemory.parse(JSON.stringify({ a: { good: { top: 5, end: false }, neg: { top: -1, end: false }, nan: { top: 'x', end: false }, flag: { top: 1 } }, b: 7 }));
  assert.deepEqual([...mixed.get('a').keys()], ['good']);
  assert.equal(mixed.get('b').size, 0);
});
