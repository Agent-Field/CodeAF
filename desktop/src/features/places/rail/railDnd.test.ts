import assert from 'node:assert/strict';
import test from 'node:test';
import { keyboardMoveTarget, movedAnnouncement, pinnedDropIndex } from './railDnd.ts';

const pins = ['a', 'b', 'c', 'd'];

test('a drop below the dragged row shifts up because the row leaves first', () => {
  assert.equal(pinnedDropIndex(pins, 'a', 3), 2);
  assert.equal(pinnedDropIndex(pins, 'a', 1), 0);
});

test('a drop above the dragged row, or from outside Pinned, keeps the slot', () => {
  assert.equal(pinnedDropIndex(pins, 'd', 1), 1);
  assert.equal(pinnedDropIndex(pins, 'x', 2), 2);
});

test('slots outside the list clamp to its ends', () => {
  assert.equal(pinnedDropIndex(pins, 'x', 9), 4);
  assert.equal(pinnedDropIndex(pins, 'x', -3), 0);
});

test('Alt+arrows move one slot and stop at the ends', () => {
  assert.equal(keyboardMoveTarget(pins, 'b', 'ArrowUp'), 0);
  assert.equal(keyboardMoveTarget(pins, 'b', 'ArrowDown'), 2);
  assert.equal(keyboardMoveTarget(pins, 'a', 'ArrowUp'), undefined);
  assert.equal(keyboardMoveTarget(pins, 'd', 'ArrowDown'), undefined);
  assert.equal(keyboardMoveTarget(pins, 'x', 'ArrowUp'), undefined);
});

test('the announcement is one-based', () => {
  assert.equal(movedAnnouncement(0, 4), 'Moved to position 1 of 4');
});
