import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { encodeCanonical, decodeCanonical, uint64 } from '../src/raft/codec.js';

const fixtures = JSON.parse(readFileSync(new URL('../../../internal/clusterstate/testdata/canonical.json', import.meta.url)));

for (const c of fixtures) {
  test(`R14 canonical Go/JS round trip: ${c.name}`, () => {
    const value = decodeCanonical(c.canonical);
    assert.equal(encodeCanonical(value), c.canonical);
    assert.equal(value.schema, 1);
    if (c.kind === 'command') {
      assert.equal(uint64(value.sequence), (1n << 64n) - 1n);
      assert.equal(uint64(value.change.acquire.expect.fence), 1n << 53n);
      assert.deepEqual(Buffer.from(value.signature, 'base64'), Buffer.from(Array.from({ length: 256 }, (_, i) => i)));
    }
    if (value.cell) {
      assert.equal(uint64(value.cell.size), (1n << 64n) - 1n);
      assert.equal(value.cell.keys.key.fence, '9007199254740993');
    }
  });
}

test('R14 full-width counters never pass through Number', () => {
  for (const n of [0n, 1n, (1n << 53n) - 1n, 1n << 53n, (1n << 64n) - 1n]) {
    assert.equal(uint64(decodeCanonical(encodeCanonical(n))), n);
  }
  for (const value of [1, -1n, 1n << 64n, '', '-1', '+1', '01', '1e3', '1.0', '18446744073709551616']) {
    assert.throws(() => uint64(value));
  }
});

test('R14 ambiguous JSON and unsafe integers are refused', () => {
  for (const value of [1.5, Number.MAX_SAFE_INTEGER + 1, NaN, Infinity, -0, undefined, '\ud800']) {
    assert.throws(() => encodeCanonical(value));
  }
  for (const text of ['{"x":1,"x":2}', '{"x":1} ', '{"x":1e3}', '{"x":-0}', '{"x":9007199254740992}', '"\\ud800"']) {
    assert.throws(() => decodeCanonical(text));
  }
});

test('R15 opaque strings are preserved even when named like counters', () => {
  const value = { fence: 'not a counter: <>&\u2028\u2029', title: 'AAEC/w==', name: 'sealed' };
  assert.deepEqual(decodeCanonical(encodeCanonical(value)), value);
});
