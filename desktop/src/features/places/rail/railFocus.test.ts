import assert from 'node:assert/strict';
import test from 'node:test';
import { railShortcutVisible, shownPlaces, stepRail } from './railFocus.ts';

test('arrows move one row and stop at the ends', () => {
  assert.equal(stepRail(4, 0, 'ArrowDown'), 1);
  assert.equal(stepRail(4, 1, 'ArrowUp'), 0);
  assert.equal(stepRail(4, 0, 'ArrowUp'), 0);
  assert.equal(stepRail(4, 3, 'ArrowDown'), 3);
  assert.equal(stepRail(0, 0, 'ArrowDown'), 0);
  assert.equal(stepRail(3, -1, 'ArrowDown'), -1);
});

test('the All places shortcut is hidden only after a read that found no live places', () => {
  assert.equal(railShortcutVisible(undefined), true);
  assert.equal(railShortcutVisible(0), false);
  assert.equal(railShortcutVisible(2), true);
});

test('archived places are dropped before a section header is considered', () => {
  const rows = shownPlaces([
    { id: 'live', archived: false },
    { id: 'old', archived: true },
  ]);
  assert.deepEqual(rows.map(row => row.id), ['live']);
  assert.equal(shownPlaces([{ id: 'old', archived: true }]).length, 0);
});
