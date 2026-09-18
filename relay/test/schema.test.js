// schema.test.js checks the row contract against the bytes the Go client
// writes (internal/pool/outbox, internal/pool/record).

import test from 'node:test';
import assert from 'node:assert/strict';

import { validateInstall, validateRow } from '../src/schema.js';

// goodLine is one row exactly as the Go client would write it: compact JSON,
// the envelope's field order, and the record.Row payload inside.
function goodLine(overrides = {}) {
  const payload = {
    schema: 1,
    metric: 'role_quality',
    role: 'worker',
    model: 'z-ai/glm-5.3-flash',
    score: 65.6,
    judge: 'anthropic/claude-opus-5',
    door: 'task',
    size: 'M',
    day: '2026-09-17',
    ...overrides.payload,
  };
  const outer = {
    schema: 1,
    day: '2026-09-17',
    nonce: '0123456789abcdef0123456789abcdef',
    payload,
    ...overrides.outer,
  };
  return JSON.stringify(outer);
}

const NOW = new Date('2026-09-17T12:00:00Z');

test('accepts the exact payload the Go side writes', () => {
  const { row, error } = validateRow(goodLine(), NOW);
  assert.equal(error, null);
  assert.equal(row.day, '2026-09-17');
  assert.equal(row.nonce, '0123456789abcdef0123456789abcdef');
  assert.equal(row.payload.role, 'worker');
  assert.equal(row.payload.model, 'z-ai/glm-5.3-flash');
  assert.equal(row.payload.score, 65.6);
  assert.equal(row.payload.judge, 'anthropic/claude-opus-5');
  assert.equal(row.payload.door, 'task');
  assert.equal(row.payload.size, 'M');
});

test('accepts a day one day ahead but rejects two', () => {
  assert.equal(validateRow(goodLine({ outer: { day: '2026-09-18' }, payload: { day: '2026-09-18' } }), NOW).error, null);
  assert.notEqual(validateRow(goodLine({ outer: { day: '2026-09-19' }, payload: { day: '2026-09-19' } }), NOW).error, null);
});

test('rejects each broken envelope field', () => {
  assert.notEqual(validateRow('', NOW).error, null);
  assert.notEqual(validateRow('not json', NOW).error, null);
  assert.notEqual(validateRow('[]', NOW).error, null);
  assert.notEqual(validateRow(goodLine({ outer: { schema: 2 } }), NOW).error, null);
  assert.notEqual(validateRow(goodLine({ outer: { day: '2026/09/17' } }), NOW).error, null);
  assert.notEqual(validateRow(goodLine({ outer: { nonce: 'ABCDEF' } }), NOW).error, null);
  assert.notEqual(validateRow(goodLine({ outer: { nonce: '0123456789abcdef0123456789abcde' } }), NOW).error, null);
  assert.notEqual(validateRow(JSON.stringify({ schema: 1, day: '2026-09-17', nonce: '0123456789abcdef0123456789abcdef' }), NOW).error, null);
  assert.notEqual(validateRow(JSON.stringify({ schema: 1, day: '2026-09-17', nonce: '0123456789abcdef0123456789abcdef', payload: [] }), NOW).error, null);
});

test('rejects each broken payload field', () => {
  const bad = [
    { payload: { schema: 2 } },
    { payload: { metric: 'other' } },
    { payload: { role: 'reflex' } },
    { payload: { model: 'glm-5.3-flash' } },
    { payload: { model: 'z-ai/glm 5.3' } },
    { payload: { score: 101 } },
    { payload: { score: -1 } },
    { payload: { score: '65' } },
    { payload: { judge: 'opus' } },
    { payload: { door: 'cron' } },
    { payload: { size: 'XL' } },
    { payload: { day: 'yesterday' } },
  ];
  for (const override of bad) {
    assert.notEqual(validateRow(goodLine(override), NOW).error, null, JSON.stringify(override));
  }
});

test('validateInstall takes 32 lowercase hex and nothing else', () => {
  assert.equal(validateInstall('0123456789abcdef0123456789abcdef'), true);
  assert.equal(validateInstall('0123456789ABCDEF0123456789abcdef'), false);
  assert.equal(validateInstall('0123'), false);
  assert.equal(validateInstall(undefined), false);
  assert.equal(validateInstall('0123456789abcdef0123456789abcde'), false);
});

test('accepts a graded row: the acceptable metric with the grader in the judge column', () => {
  const { row, error } = validateRow(goodLine({ payload: { metric: 'acceptable', score: 100, judge: 'codeaf/grader' } }), NOW);
  assert.equal(error, null);
  assert.equal(row.payload.metric, 'acceptable');
  assert.equal(row.payload.judge, 'codeaf/grader');
});

test('refuses a metric that is neither the judge\'s nor the grader\'s', () => {
  const { error } = validateRow(goodLine({ payload: { metric: 'role_rating' } }), NOW);
  assert.match(error, /metric must be role_quality or acceptable/);
});
