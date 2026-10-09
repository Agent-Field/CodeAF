// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import { costText, factsOf, progressText, startedText, stepOf } from './taskDetail.ts';

const NOW = Date.parse('2026-10-09T10:05:00Z');
const row = (over = {}) => ({ ID: 'a', Title: 'A', Status: 'running', ...over });

test('a bare row has no facts: nothing is invented', () => {
  assert.deepEqual(factsOf(row()), []);
  assert.equal(costText(row({ USD: 0, Tokens: 0 })), '');
  assert.equal(startedText('0001-01-01T00:00:00Z'), '');
});

test('the facts come in the designer order and only when provided', () => {
  const facts = factsOf(row({ Model: 'm', Steps: 12, USD: 0.14, Tokens: 41000, Started: '2026-10-09T10:03:00Z' }));
  assert.deepEqual(facts.map((fact) => fact.label), ['Model', 'Steps', 'Cost', 'Started']);
  assert.equal(facts[2].value, '$0.14 · 41k tokens');
  assert.deepEqual(factsOf(row({ Tokens: 900 })).map((fact) => fact.value), ['900 tokens']);
});

test('the live step wins over the recorded count', () => {
  assert.equal(stepOf(row({ Steps: 3, Live: { Step: 5 } })), 5);
  assert.equal(stepOf(row({ Steps: 3 })), 3);
  assert.equal(stepOf(row()), 0);
});

test('progress is step then age, each only when known', () => {
  const started = '2026-10-09T10:03:00Z';
  assert.equal(progressText(row({ Steps: 12, Started: started }), 'running', NOW), 'step 12 · 2m');
  assert.equal(progressText(row({ Started: started }), 'running', NOW), '2m');
  assert.equal(progressText(row(), 'queued', NOW), '');
});
