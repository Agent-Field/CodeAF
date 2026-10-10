import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createScrollSaveThrottle } from './saveThrottle.ts';
import { canRestore, restoredTop } from './restoreRules.ts';

test('continuous scrolling saves the latest position every 500ms without resetting the timer', context => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  let position = 10;
  const saved: number[] = [];
  const throttle = createScrollSaveThrottle(() => saved.push(position));
  throttle.schedule();
  context.mock.timers.tick(499);
  position = 20;
  throttle.schedule();
  assert.deepEqual(saved, []);
  context.mock.timers.tick(1);
  assert.deepEqual(saved, [20]);
  position = 30;
  throttle.schedule();
  context.mock.timers.tick(500);
  assert.deepEqual(saved, [20, 30]);
  context.mock.timers.tick(1000);
  assert.deepEqual(saved, [20, 30], 'idle scrolling does not write');
});

test('leaving the window flushes a pending save once and allows the next batch', context => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  let writes = 0;
  const throttle = createScrollSaveThrottle(() => writes++);
  throttle.flush();
  assert.equal(writes, 0);
  throttle.schedule();
  context.mock.timers.tick(20);
  throttle.flush();
  throttle.flush();
  context.mock.timers.tick(500);
  assert.equal(writes, 1);
  throttle.schedule();
  context.mock.timers.tick(500);
  assert.equal(writes, 2);
});

test('mid-transcript offsets wait for replay, while bottom following restores the new end', () => {
  const middle = { top: 900, left: 0, end: false };
  const bottom = { ...middle, end: true };
  assert.equal(canRestore(middle, 400, 0), false);
  assert.equal(canRestore(middle, 1000, 0), true);
  assert.equal(restoredTop(middle, 1500), 900);
  assert.equal(canRestore(bottom, 400, 0), true);
  assert.equal(restoredTop(bottom, 1500), 1500);
  assert.equal(canRestore({ ...middle, left: 80 }, 1000, 40), false);
});
