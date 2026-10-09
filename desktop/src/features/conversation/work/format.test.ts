import assert from 'node:assert/strict';
import { test } from 'node:test';
import { duration, elapsed, summaryParts } from './format.ts';

test('duration reads naturally at every scale', () => {
  assert.equal(duration(3200), '3.2s');
  assert.equal(duration(42_000), '42s');
  assert.equal(duration(134_000), '2m 14s');
  assert.equal(duration(3_900_000), '1h 5m');
});

test('summary lists only the parts that exist', () => {
  const full = { seconds: 42, thoughtSeconds: 6, steps: 4, calls: 9, failed: 1 };
  assert.equal(summaryParts(full).map((part) => part.text).join(' · '), 'Worked 42s · thought 6s · 4 steps · 9 calls · 1 failed');
  assert.equal(summaryParts({ steps: 1, calls: 1, failed: 0 }).map((part) => part.text).join(' · '), 'Worked · 1 step · 1 call');
});

test('summary marks failures and speaks present tense while live', () => {
  const parts = summaryParts({ seconds: 5, steps: 1, calls: 2, failed: 1 }, true);
  assert.equal(parts[0].text, 'Working 5s');
  assert.equal(parts[parts.length - 1].tone, 'warning');
});

test('elapsed needs a start', () => {
  assert.equal(elapsed(undefined, 10), undefined);
  assert.equal(elapsed(1000, 13_000), '12s');
});
