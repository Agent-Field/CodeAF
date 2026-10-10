import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { design } from '../../scripts/design-output.mjs';

// Iteration 2 geometry is theme-independent; its colours are var() references to
// Foundations tokens, so each key must reach both the light and dark :root output.
const keys = Object.keys(design.foundation).filter((k) => k.startsWith('i2-'));
const css = readFileSync(new URL('../styles/tokens.css', import.meta.url), 'utf8');

test('Iteration 2 tokens are defined', () => assert.ok(keys.length >= 50));
test('every Iteration 2 token is emitted and resolves in light and dark', () => {
  for (const key of keys) assert.match(css, new RegExp(`--${key}:`), key);
  for (const theme of ['light', 'dark']) {
    const names = Object.keys(design.themes[theme]);
    for (const key of keys) {
      for (const ref of design.foundation[key].matchAll(/var\(--([\w-]+)\)/g)) {
        assert.ok(names.includes(ref[1]) || ref[1] in design.foundation, `${theme}: ${key} -> ${ref[1]}`);
      }
    }
  }
});
