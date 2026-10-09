import test from 'node:test';
import assert from 'node:assert/strict';
import { createOverviewPinch, OVERVIEW_PINCH_SCALE } from './overviewGesture.ts';
test('an outward scale opens once until the gesture ends', () => {
 const p = createOverviewPinch();
 assert.equal(p.scale(1.05), false); assert.equal(p.scale(0.8), false);
 assert.equal(p.scale(OVERVIEW_PINCH_SCALE), true); assert.equal(p.scale(1.5), false);
 p.reset(); assert.equal(p.scale(1.2), true);
});
test('small wheel spreads accumulate only within one sequence', () => {
 const p = createOverviewPinch();
 assert.equal(p.wheel(-4, 0), false); assert.equal(p.wheel(-4, 20), false);
 assert.equal(p.wheel(-4, 40), true); assert.equal(p.wheel(-20, 50), false);
 assert.equal(p.wheel(-4, 400), false);
});
test('inward movement cancels earlier outward noise', () => {
 const p = createOverviewPinch();
 assert.equal(p.wheel(-8, 0), false); assert.equal(p.wheel(2, 20), false);
 assert.equal(p.wheel(-4, 40), false); assert.equal(p.wheel(-8, 60), true);
});
test('non-finite values never trigger', () => {
 const p = createOverviewPinch();
 for (const v of [NaN, Infinity, -Infinity]) {
  assert.equal(p.scale(v), false); assert.equal(p.wheel(v, 0), false); assert.equal(p.wheel(-20, v), false);
 }
});
