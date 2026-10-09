import test from 'node:test';
import assert from 'node:assert/strict';
import { inboxFocus } from './inboxFocus.ts';
import { oldestFirst } from './background.ts';

test('a focus request is answered once and notifies listeners', () => {
  let heard = 0;
  const off = inboxFocus.subscribe(() => heard++);
  assert.equal(inboxFocus.take(), false);
  inboxFocus.request();
  assert.equal(heard, 1);
  assert.equal(inboxFocus.take(), true);
  assert.equal(inboxFocus.take(), false);
  off();
});

test('timed questions lead, oldest first; untimed ones keep their order behind them', () => {
  const out = oldestFirst([{ id: 'x' }, { id: 'new', asked: 20 }, { id: 'y' }, { id: 'old', asked: 10 }]);
  assert.deepEqual(out.map(i => i.id), ['old', 'new', 'x', 'y']);
});
