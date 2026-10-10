import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { windowFrameTint } from './useWindowTint.ts';

const place = 'pl_0123456789abcdef';

test('Now and the root are graphite before the graph arrives', () => {
  assert.equal(windowFrameTint('now', undefined), 'graphite');
  assert.equal(windowFrameTint('root', undefined), 'graphite');
  assert.equal(windowFrameTint('now', 'rose'), 'graphite');
  assert.equal(windowFrameTint('root', 'iris'), 'graphite');
});

test('a graph place wears its effective tint, including an inherited one and graphite', () => {
  for (const tint of ['tide', 'iris', 'rose', 'sand', 'sage', 'graphite']) {
    assert.equal(windowFrameTint(place, tint), tint);
  }
});

test('an unknown place draws nothing', () => {
  assert.equal(windowFrameTint(place, undefined), undefined);
  assert.equal(windowFrameTint(place, ''), undefined);
  assert.equal(windowFrameTint(place, 'purple'), undefined);
  assert.equal(windowFrameTint(undefined, 'iris'), undefined);
  assert.equal(windowFrameTint('', 'iris'), undefined);
});

test('a place switch replaces the tint in the same call', () => {
  assert.equal(windowFrameTint(place, 'rose'), 'rose');
  assert.equal(windowFrameTint('now', undefined), 'graphite');
  assert.equal(windowFrameTint(place, 'tide'), 'tide');
  assert.equal(windowFrameTint('root', undefined), 'graphite');
});

test('the tint module does not call the native window', () => {
  const src = readFileSync(new URL('./useWindowTint.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(src, /from ['"]@tauri-apps/);
  assert.doesNotMatch(src, /\b(setEffects|setDecorations|setTitleBarStyle|windowEffects|setTheme)\b/);
  assert.match(src, /useLayoutEffect/);
  assert.doesNotMatch(src, /transition|setTimeout|requestAnimationFrame/);
});
