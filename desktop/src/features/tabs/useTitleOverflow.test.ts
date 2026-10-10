import assert from 'node:assert/strict';
import test from 'node:test';
import { titleOverflows } from './useTitleOverflow.ts';

const box = { clientWidth: 100, scrollWidth: 100, textWidth: 40, fade: 20 };

test('an icon-only tab overflows: the title is not on the chip', () => {
  assert.equal(titleOverflows(null, true), true);
  assert.equal(titleOverflows(box, true), true);
});

test('a title wider than its box overflows', () => {
  assert.equal(titleOverflows({ ...box, scrollWidth: 140, textWidth: 140 }, false), true);
});

test('text that runs into the end fade overflows, even when the box itself holds it', () => {
  assert.equal(titleOverflows({ ...box, textWidth: 90 }, false), true);
});

test('a title clear of the fade, an unmasked title, and an unmeasured box do not overflow', () => {
  assert.equal(titleOverflows(box, false), false);
  assert.equal(titleOverflows({ ...box, textWidth: 90, fade: 0 }, false), false);
  assert.equal(titleOverflows({ ...box, clientWidth: 0 }, false), false);
  assert.equal(titleOverflows(null, false), false);
});
