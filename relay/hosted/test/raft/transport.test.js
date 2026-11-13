import assert from 'node:assert/strict';
import { createPublicKey, createHash, verify } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

// R14 checks independently authored Node signatures against the exact wire
// fixture also decoded by native Go; no Worker or hosted service is contacted.
test('R14/R16 native peer envelopes preserve bytes, integers and signature domains', () => {
  const text = readFileSync(new URL('../../../../internal/rafttransport/testdata/exchange-v1.json', import.meta.url), 'utf8');
  const fixture = JSON.parse(text);
  assert.equal(JSON.stringify(fixture), text);
  const key = createPublicKey({ key: Buffer.concat([Buffer.from('302a300506032b6570032100', 'hex'), Buffer.from(fixture.public_key, 'base64')]), format: 'der', type: 'spki' });
  const check = (value, domain) => {
    const signature = Buffer.from(value.signature, 'base64');
    const unsigned = { ...value, signature: null };
    assert(verify(null, Buffer.from(domain + JSON.stringify(unsigned)), key, signature));
    assert(!verify(null, Buffer.from('wrong-domain\n' + JSON.stringify(unsigned)), key, signature));
  };
  check(fixture.request, 'codeaf/raft/exchange/1\n');
  const envelope = fixture.request.messages[0];
  check(envelope, 'codeaf/raft/envelope/1\n');
  const payload = Buffer.from(envelope.payload, 'base64');
  assert.equal(createHash('sha256').update(payload).digest('hex'), envelope.digest);
  assert.deepEqual(payload.subarray(-256), Buffer.from(Array.from({ length: 256 }, (_, i) => i)));
  assert.equal(BigInt(envelope.sequence), (1n << 64n) - 1n);
  assert.equal(BigInt(fixture.request.ack[0]), 1n << 53n);
  assert.equal(BigInt(envelope.scope.epoch), (1n << 64n) - 1n);
  assert.equal(BigInt(envelope.incarnation), 1n << 53n);
  const altered = { ...envelope, to: '3', signature: null };
  assert(!verify(null, Buffer.from('codeaf/raft/envelope/1\n' + JSON.stringify(altered)), key, Buffer.from(envelope.signature, 'base64')));
});
