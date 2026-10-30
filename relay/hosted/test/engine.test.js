import { test } from 'node:test';
import assert from 'node:assert/strict';
import { nameFor, validName } from '../src/engine/names.js';
import { MAX_PAYLOAD, frame, readFrame, registrationProof, verdict } from '../src/engine/carrier.js';

const hex = (text) => Uint8Array.from(text.match(/../g), (b) => parseInt(b, 16));
const toHex = (bytes) => [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
const run = (from, n = 32) => Uint8Array.from({ length: n }, (_, i) => (from + i) & 0xff);

// Computed by internal/relay's NameFor; the port must derive the same name from the same key.
test('a machine name is the one the Go side derives from the same key', async () => {
  for (const [key, name] of [
    ['000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f', 'lantern-amber-86'],
    ['0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20', 'jasmine-dragon-58'],
    ['7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e', 'quartz-donkey-28'],
    ['ff000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e', 'lavender-rhubarb-13'],
  ]) {
    assert.equal(await nameFor(hex(key)), name);
    assert.ok(validName(name));
  }
  for (const bad of ['otter-lamp-4', 'Otter-lamp-42', 'otter-lamp', 'otter-lamp-42-x', 'otterlampotter-x-42']) assert.ok(!validName(bad), bad);
});

// Computed by internal/relay's registrationProof over the same shared secret, nonce, name and key.
test('the possession proof is the one the Go side computes', async () => {
  const key = run(200);
  const proof = await registrationProof(run(0), run(100), 'bluff-meadow-35', key);
  assert.equal(toHex(proof), '858616a2eaed21cdcae7e1c815c02ec366f7cc550f63ccf189881aa641feff82');
});

test('a frame is one message and a message that is not exactly one frame is refused', () => {
  const sent = frame(2, '7', new Uint8Array([1, 2, 3]));
  assert.deepEqual([...sent.subarray(0, 13)], [2, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 3]);
  const got = readFrame(sent);
  assert.equal(got.op, 2);
  assert.equal(got.stream, '7');
  assert.deepEqual([...got.payload], [1, 2, 3]);
  assert.equal(readFrame(sent.subarray(0, 15)), null);
  assert.equal(readFrame(new Uint8Array([...sent, 9])), null);
  const huge = new Uint8Array(13);
  new DataView(huge.buffer).setUint32(9, MAX_PAYLOAD + 1);
  assert.equal(readFrame(huge), null);
});

test('a verdict is ready, or refused with its sentence', () => {
  assert.deepEqual([...verdict()], [1, 0]);
  const said = verdict('no');
  assert.deepEqual([...said], [2, 2, 110, 111]);
});
