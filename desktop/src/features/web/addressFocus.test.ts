import test from 'node:test';
import assert from 'node:assert/strict';

import { addressPane } from './addressFocus.ts';

test('a named pane is focused only when it is on screen', () => {
  assert.equal(addressPane(['a', 'b'], 'b'), 'b');
  assert.equal(addressPane(['a', 'b'], 'gone'), undefined);
});

test('without a pane the first address on screen is the one', () => {
  assert.equal(addressPane(['a', 'b']), 'a');
  assert.equal(addressPane([]), undefined);
});
