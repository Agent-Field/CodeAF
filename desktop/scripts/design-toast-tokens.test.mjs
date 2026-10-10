import assert from 'node:assert/strict';
import { test } from 'node:test';
import { css, design } from './design-output.mjs';

test('shared toasts own the rise, narrow clamp and deletion undo duration', () => {
  const expected = {
    'toast-rise': '8px',
    'motion-rise-toast': 'var(--toast-rise)',
    'toast-gutter': '8px',
    'toast-narrow-gutter': 'var(--toast-gutter)',
    'toast-max-width': 'calc(100vw - var(--toast-gutter) * 2)',
  };
  for (const [key, value] of Object.entries(expected)) {
    assert.equal(design.foundation[key], value, key);
    assert.ok(css.includes(`--${key}: ${value};`), key);
  }
  assert.equal(design.interaction.toastUndoDuration, 10000);
  assert.equal(design.interaction.toastDuration, 6000);
  assert.equal(Object.keys(design.foundation).filter(key => /^(archive|history)-toast-/.test(key)).length, 0);
});
