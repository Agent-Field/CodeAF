import assert from 'node:assert/strict';
import { test } from 'node:test';
import { countLines, encodePasted, isLongPaste, plainMessage, splitPasted } from './pastedText.ts';

const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n');

test('twelve lines stay in the field, thirteen become a card', () => {
  assert.equal(isLongPaste(lines(12)), false);
  assert.equal(isLongPaste(lines(13)), true);
});

test('a trailing newline is not a line', () => {
  assert.equal(countLines(`${lines(3)}\n`), 3);
});

test('a paste and the typed text round-trip', () => {
  const sent = encodePasted([lines(214)], 'Why does nested fail?');
  const { pastes, rest } = splitPasted(sent);
  assert.equal(pastes.length, 1);
  assert.equal(pastes[0].lines, 214);
  assert.equal(pastes[0].text, lines(214));
  assert.equal(rest, 'Why does nested fail?');
});

test('a paste alone has no remaining text, and plain text has no paste', () => {
  assert.equal(splitPasted(encodePasted([lines(20)], '')).rest, '');
  assert.deepEqual(splitPasted('hello'), { pastes: [], rest: 'hello' });
});

test('a message reads without its pasted tags', () => {
  assert.equal(plainMessage(encodePasted([lines(20)], 'Why does nested fail?')), 'Why does nested fail?');
  assert.equal(plainMessage(encodePasted([lines(20), lines(5)], '')), 'Pasted text · 25 lines');
  assert.equal(plainMessage('plain words'), 'plain words');
});
