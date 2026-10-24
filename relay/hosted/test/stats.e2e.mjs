// /v1/store/stats survives the relay's process: phase "put" counts some requests and waits for the
// flush; the runner then stops and restarts the relay over the same storage; phase "check" reads
// the counts back. Usage: node stats.e2e.mjs put|check <state file>.
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync } from 'node:fs';
import { newIdentity, newDevice } from './party.js';
import { frame, rid } from './helpers.js';
import { call } from './client.js';

const BASE = process.env.RELAY_PERSIST ?? 'http://127.0.0.1:18793';
const [phase, file] = process.argv.slice(2);
const jwk = (key) => crypto.subtle.exportKey('jwk', key);
const load = (j, usage) => crypto.subtle.importKey('jwk', j, { name: 'Ed25519' }, true, [usage]);
const pubOf = (j) => Uint8Array.from(Buffer.from(j.x, 'base64url'));

if (phase === 'put') {
  const identity = await newIdentity();
  const dev = await newDevice(identity);
  const f = frame([rid(1), rid(2)]);
  assert.equal((await call(dev, 'POST', '/v1/store/frames', f.bytes, { base: BASE })).status, 200);
  await call(dev, 'GET', '/v1/store/objects/' + rid(1), undefined, { base: BASE });
  await call(dev, 'POST', '/v1/store/has', { rids: [rid(1)] }, { base: BASE });
  const live = (await call(dev, 'GET', '/v1/store/stats', undefined, { base: BASE })).json;
  assert.equal(live.puts, 1);
  await new Promise((r) => setTimeout(r, 1500)); // longer than the flush interval this relay runs with
  writeFileSync(file, JSON.stringify({ identity: await jwk(identity.pair.privateKey), device: await jwk(dev.pair.privateKey), cert: dev.cert, expect: live }));
} else {
  const s = JSON.parse(readFileSync(file));
  const idPriv = await load(s.identity, 'sign');
  const identity = { pair: { privateKey: idPriv }, pub: pubOf(s.identity) };
  const dev = { identity, cert: s.cert, pair: { privateKey: await load(s.device, 'sign') } };
  const after = (await call(dev, 'GET', '/v1/store/stats', undefined, { base: BASE })).json;
  assert.deepEqual(after, s.expect, 'the counts the relay held before it stopped');
  assert.equal((await call(dev, 'GET', '/v1/store/objects/' + rid(2), undefined, { base: BASE })).status, 200, 'and the objects, found by the index in SQLite');
  assert.equal((await call(dev, 'GET', '/v1/store/stats', undefined, { base: BASE })).json.gets, s.expect.gets + 1, 'and it counts on from there');
}
console.log('stats', phase, 'ok');
