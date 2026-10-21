// The nameplate allocator: quarantine after a mailbox ends, and the length chosen over occupied plates.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULTS, limitsOf } from '../src/limits.js';
import { digitsFor, Plates } from '../src/pair/plates.js';
import { memorySql } from './sql.js';

const TTL = 10_000;
const limits = { ...DEFAULTS, pairTtlMs: TTL, plateQuarantineMs: TTL, pairMaxBoxes: 50 };

// An allocator that draws from a script, then from a counter, so a test names the plate each draw lands on.
function plates(script = [], over = {}) {
  const queue = [...script];
  let next = 0;
  return new Plates(memorySql(), { ...limits, ...over }, () => queue.shift() ?? next++);
}

const refusal = (fn) => {
  try {
    fn();
    return null;
  } catch (e) {
    return [e.code, e.status].join(' ');
  }
};

test('the length grows with the occupied plates, 2 to 4 digits', () => {
  assert.deepEqual([0, 9, 10, 99, 100, 999, 1000, 5000].map(digitsFor), [2, 2, 3, 3, 4, 4, 4, 4]);
});

test('a released plate is not drawn again within the quarantine', () => {
  const p = plates([7, 7, 7, 8]);
  assert.equal(p.allocate(0), '07');
  p.release('07', 1_000);
  // The draws that land on 07 are skipped for as long as it is quarantined; the next free draw is 08.
  assert.equal(p.allocate(1_000 + TTL - 1), '08');
});

test('a released plate is drawn again once the quarantine has passed', () => {
  const p = plates([7, 7]);
  assert.equal(p.allocate(0), '07');
  p.release('07', 1_000);
  assert.equal(p.allocate(1_000 + TTL), '07');
});

test('a plate whose mailbox expired is quarantined from its end, not from its creation', () => {
  const p = plates([7, 7, 5, 7]);
  assert.equal(p.allocate(0), '07');
  assert.equal(p.allocate(TTL), '05', 'at the expiry it is still held: the draw of 07 is skipped');
  assert.equal(p.allocate(2 * TTL), '07', 'one quarantine after the expiry it is free');
});

test('releasing twice, or releasing a plate that already ended, does not lengthen the hold', () => {
  const p = plates([7, 7]);
  p.allocate(0);
  p.release('07', 1_000);
  p.release('07', 5_000);
  assert.equal(p.allocate(1_000 + TTL), '07');
});

test('a live plate is never drawn, and quarantined plates do not count against the live cap', () => {
  const p = plates([], { pairMaxBoxes: 2 });
  p.allocate(0);
  p.allocate(0);
  assert.equal(refusal(() => p.allocate(0)), 'full 503', 'two live boxes fill a relay of two');
  p.release('00', 10);
  assert.equal(p.allocate(20), '02', 'a released box makes room at once, under a new plate');
});

test('quarantined plates count as occupied when the length is chosen', () => {
  const p = plates([], { pairMaxBoxes: 1_000 });
  for (let i = 0; i < 10; i++) {
    p.allocate(i);
    p.release(String(i).padStart(2, '0'), i);
  }
  // Ten plates are quarantined and none is live; ten of 100 is a tenth, so the next plate has 3 digits.
  assert.equal(p.allocate(100).length, 3);
});

test('a full space is refused, not looped on', () => {
  const stuck = new Plates(memorySql(), { ...limits, pairMaxBoxes: 1_000 }, () => 3);
  assert.equal(stuck.allocate(0), '03');
  assert.equal(refusal(() => stuck.allocate(1)), 'full 503', 'every draw lands on the held plate');
});

test('the plates of a relay from before quarantine are adopted and the old table is dropped', () => {
  const sql = memorySql();
  sql.exec('CREATE TABLE plates (np TEXT PRIMARY KEY, expires INTEGER NOT NULL)');
  sql.exec('INSERT INTO plates VALUES (?,?)', '42', 5_000);
  const p = new Plates(sql, limits, () => 42);
  assert.equal(refusal(() => p.allocate(1_000)), 'full 503', '42 is still live under its old expiry');
  assert.equal(sql.exec("SELECT COUNT(*) AS n FROM sqlite_master WHERE name = 'plates'").one().n, 0);
});

test('the quarantine defaults to one pairing life, and a deployment may set it', () => {
  assert.equal(limitsOf({}).plateQuarantineMs, DEFAULTS.pairTtlMs);
  assert.equal(limitsOf({ CAF_LIMITS: '{"pairTtlMs":4000}' }).plateQuarantineMs, 4000);
  assert.equal(limitsOf({ CAF_LIMITS: '{"pairTtlMs":4000,"plateQuarantineMs":99}' }).plateQuarantineMs, 99);
});
