// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import { rowAge, tokensText, waitPrefix } from './rowText.ts';
import { parseTime, shortAge, spanText } from './taskClock.ts';

const NOW = Date.parse('2026-10-09T10:05:00Z');
const row = (over = {}) => ({ ID: 'a', Title: 'A', Status: 'running', Started: '2026-10-09T10:04:20Z', ...over });

test('ages are coarse and never invented', () => {
  assert.equal(shortAge(40_000), '40s');
  assert.equal(shortAge(130_000), '2m');
  assert.equal(shortAge(3 * 3_600_000), '3h');
  assert.equal(shortAge(-1), '');
  assert.equal(shortAge(Number.NaN), '');
  assert.equal(spanText(134_000), '2m 14s');
});

test('the Go zero time means never', () => {
  assert.ok(Number.isNaN(parseTime('0001-01-01T00:00:00Z')));
  assert.ok(Number.isNaN(parseTime(undefined)));
  assert.equal(rowAge(row({ Started: '0001-01-01T00:00:00Z' }), 'running', NOW), '');
});

test('a running row counts up, an ended row shows how long it took, a queued row is blank', () => {
  assert.equal(rowAge(row(), 'running', NOW), '40s');
  assert.equal(rowAge(row({ Status: 'done', Ended: '2026-10-09T10:06:20Z' }), 'done', NOW), '2m');
  assert.equal(rowAge(row({ Status: 'pending' }), 'queued', NOW), '');
  assert.equal(rowAge(row({ Status: 'paused' }), 'yourcall', NOW), '');
});

test('queued rows lead their title with what they wait on', () => {
  assert.equal(waitPrefix('queued', ['Split by loader']), 'Split by loader /');
  assert.equal(waitPrefix('queued', ['A', 'B']), 'A, B /');
  assert.equal(waitPrefix('queued', []), '');
  assert.equal(waitPrefix('running', ['A']), '');
});

test('token counts are compact', () => {
  assert.equal(tokensText(0), '');
  assert.equal(tokensText(940), '940');
  assert.equal(tokensText(9900), '9.9k');
  assert.equal(tokensText(12_000), '12k');
  assert.equal(tokensText(1_240_000), '1.2M');
});
