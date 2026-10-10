import assert from 'node:assert/strict';
import { test } from 'node:test';
import { beginPost, holdSend, restoreFront, type HeldSend } from './outbox.ts';

test('plain text is held in send order and a file or a blank is not', () => {
  const first = holdSend([], { text: 'one', mode: 'submit' });
  assert.equal(first.accepted, true);
  assert.deepEqual(first.held, [{ text: 'one', mode: 'submit' }]);

  const second = holdSend(first.held, { text: 'two', mode: 'steer' });
  assert.equal(second.accepted, true);
  assert.deepEqual(second.held, [
    { text: 'one', mode: 'submit' },
    { text: 'two', mode: 'steer' },
  ]);

  const queued = holdSend(second.held, { text: 'three', mode: 'queue' });
  assert.equal(queued.held[2].mode, 'queue');

  const withFile = holdSend(queued.held, { text: 'with a file', mode: 'submit', files: [{ name: 'a.txt' }] });
  assert.equal(withFile.accepted, false);
  assert.equal(withFile.held, queued.held);

  const blank = holdSend(queued.held, { text: '   ', mode: 'submit' });
  assert.equal(blank.accepted, false);
  assert.equal(blank.held, queued.held);

  // Holding must not rewrite the queue the caller already has.
  assert.deepEqual(first.held, [{ text: 'one', mode: 'submit' }]);
});

test('posts leave oldest first, and a send put back stays in front of the rest', () => {
  let held: readonly HeldSend[] = [];
  held = holdSend(held, { text: 'one', mode: 'submit' }).held;
  held = holdSend(held, { text: 'two', mode: 'queue' }).held;
  held = holdSend(held, { text: 'three', mode: 'steer' }).held;

  const first = beginPost(held);
  assert.deepEqual(first.posting, { text: 'one', mode: 'submit' });
  assert.deepEqual(first.held.map((item) => item.text), ['two', 'three']);

  const second = beginPost(first.held);
  assert.equal(second.posting?.text, 'two');
  assert.equal(second.posting?.mode, 'queue');

  const restored = restoreFront(second.held, second.posting!);
  assert.deepEqual(restored.map((item) => item.text), ['two', 'three']);
  assert.equal(restored[0].mode, 'queue');

  assert.equal(beginPost([]).posting, undefined);
  assert.deepEqual(beginPost([]).held, []);
});
