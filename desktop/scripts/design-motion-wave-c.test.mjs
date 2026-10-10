import assert from 'node:assert/strict';
import { test } from 'node:test';
import { css, design } from './design-output.mjs';

test('wave C provides foundation motion and keeps existing shimmer names as aliases', () => {
 const expected = { 'motion-rise-row': '4px', 'motion-rise-send': '6px', 'motion-rise-toast': 'var(--toast-rise)', 'motion-place-slide': '6px', 'motion-scale-popover': '.98', 'motion-scale-quicklook': '.96', 'motion-fold-open': 'var(--motion-ask-open)', 'motion-chevron-open': '180deg', 'breathe-duration': '2.8s', 'breathe-from': '2px', 'breathe-to': '4.5px', 'cf-shimmer-duration': '2.4s', 'motion-tab-collapse': 'var(--dur-base)' };
 for (const [key, value] of Object.entries(expected)) assert.equal(design.foundation[key], value, key);
 for (const key of ['shimmer-duration', 'latest-shimmer-duration', 'task-view-shimmer-duration']) assert.equal(design.foundation[key], 'var(--cf-shimmer-duration)');
 assert.equal(design.interaction.copiedFeedbackMs, 1200);
});

test('reduced motion disables every rise, scale, breathe distance and component duration', () => {
 const reduced = css.slice(css.indexOf('@media (prefers-reduced-motion: reduce)'));
 for (const key of Object.keys(design.foundation)) {
  if (key.startsWith('motion-rise-') || ['motion-place-slide', 'breathe-from', 'breathe-to'].includes(key)) assert.ok(reduced.includes(`--${key}: var(--motion-rest);`), key);
  if (key.startsWith('motion-scale-') && key !== 'motion-scale-rest') assert.ok(reduced.includes(`--${key}: var(--motion-scale-rest);`), key);
  // The next-up banner fade is the one duration that stays. Zeroing it snaps the fold.
  if (key === 'i2-banner-duration') { assert.equal(reduced.includes('--i2-banner-duration:'), false); continue; }
  if (key.endsWith('-duration') || key === 'motion-tab-collapse') assert.ok(reduced.includes(`--${key}: var(--duration-none);`), key);
 }
});
