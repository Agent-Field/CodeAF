import assert from 'node:assert/strict';
import test from 'node:test';
import { DECIDED_CAP, DECIDED_PREVIEW, decidedCount, decidedLabel, decidedShowsAll, decidedText, orderedDecided, visibleDecided, type DecidedItem } from './decidedModel.ts';

const row = (id: string, at?: string, title = id): DecidedItem => ({ id, title, at, detail: `${id} detail`, age: '1h' });

test('the label is the 12a sentence, and a blank field is nothing', () => {
  assert.equal(decidedLabel(9), 'Decided automatically · 9');
  assert.equal(decidedText('  1h  '), '1h');
  assert.equal(decidedText('   '), undefined);
  assert.equal(decidedText(undefined), undefined);
});

test('three rows show, All is the rest, and the list stops at 200 newest', () => {
  const items = Array.from({ length: 5 }, (_, index) => row(`r${index}`, `2026-10-10T0${index}:00:00Z`));
  assert.equal(DECIDED_PREVIEW, 3);
  assert.deepEqual(visibleDecided(items, false).map(item => item.id), ['r4', 'r3', 'r2']);
  assert.deepEqual(visibleDecided(items, true).map(item => item.id), ['r4', 'r3', 'r2', 'r1', 'r0']);
  assert.equal(decidedShowsAll(items), true);
  assert.equal(decidedShowsAll(items.slice(0, 3)), false);

  const stamped = Array.from({ length: DECIDED_CAP + 5 }, (_, index) => row(`n${index}`, new Date(Date.UTC(2026, 0, 1, 0, 0, index)).toISOString()));
  const expanded = visibleDecided(stamped, true);
  assert.equal(expanded.length, DECIDED_CAP);
  assert.equal(expanded[0].id, `n${DECIDED_CAP + 4}`);
  assert.equal(expanded.at(-1)?.id, 'n5');
});

test('without a time the caller order stands, blank titles drop, and the count never undercuts the rows', () => {
  const items = [row('a'), { id: 'blank', title: '  ' }, row('b')];
  assert.deepEqual(orderedDecided(items).map(item => item.id), ['a', 'b']);
  assert.equal(decidedCount(items), 2);
  assert.equal(decidedCount(items, 9), 9);
  assert.equal(decidedCount(items, 1), 2);
  assert.equal(visibleDecided([], false).length, 0);
});
