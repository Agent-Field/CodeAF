import assert from 'node:assert/strict';
import { test } from 'node:test';
import { css, design } from './design-output.mjs';

test('shell coarse-pointer geometry is emitted from the foundation tokens', () => {
 const expected = { 'hit-coarse-close': '24px', 'hit-coarse-row': '40px', 'pane-resize-hit-coarse': '16px' };
 for (const [key, value] of Object.entries(expected)) {
  assert.equal(design.foundation[key], value, key);
  assert.ok(css.includes(`--${key}: ${value};`), key);
 }
 assert.equal(design.foundation['pane-resize-hit'], '8px');
 assert.equal(design.interaction.longPressDelay, 500);
});
