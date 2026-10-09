import test from 'node:test';
import assert from 'node:assert/strict';
import { earlierLabel, isFolded, jumpIndex, splitEarlier } from './folding.ts';
import type { TurnV2 } from './types.ts';

const turn = (id: string, state: TurnV2['state'] = 'done'): TurnV2 => ({ id, user: id, attachments: [], steer: [], blocks: [], state, digest: '' });

test('a settled turn folds unless it is the latest', () => {
  assert.equal(isFolded(turn('a'), 0, 2, {}), true);
  assert.equal(isFolded(turn('b'), 1, 2, {}), false);
  assert.equal(isFolded(turn('c', 'working'), 0, 2, {}), false);
  assert.equal(isFolded(turn('d', 'failed'), 0, 2, {}), false);
});

test('the reader choice beats the default either way', () => {
  assert.equal(isFolded(turn('a'), 0, 2, { a: false }), false);
  assert.equal(isFolded(turn('b'), 1, 2, { b: true }), true);
});

test('only turns beyond twelve fall into the group', () => {
  const turns = Array.from({ length: 30 }, (_, i) => turn(`t${i}`));
  const { earlier, recent } = splitEarlier(turns);
  assert.equal(earlier.length, 18);
  assert.equal(recent.length, 12);
  assert.equal(recent[0].id, 't18');
  assert.equal(splitEarlier(turns.slice(0, 12)).earlier.length, 0);
});

test('the group label counts its turns', () => {
  assert.equal(earlierLabel(18), '18 earlier turns');
  assert.equal(earlierLabel(1), '1 earlier turn');
});

test('jumps step to the previous and next turn around the offset', () => {
  assert.equal(jumpIndex([-300, -20, 72, 400], -1), 1);
  assert.equal(jumpIndex([-300, -20, 72, 400], 1), 3);
  assert.equal(jumpIndex([100, 400], -1), -1);
  assert.equal(jumpIndex([-300, 72], 1), -1);
});
