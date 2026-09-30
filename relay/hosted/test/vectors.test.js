import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { decode, BadFrame } from '../src/frame.js';
import { verify, Refusal } from '../src/verify.js';
import * as rules from '../src/rules.js';

const vectors = JSON.parse(readFileSync(process.env.VECTORS ?? new URL('../vectors.json', import.meta.url)));
const bytes = (b64) => new Uint8Array(Buffer.from(b64, 'base64'));
const H = { identity: 'Codeaf-Identity', cert: 'Codeaf-Cert', time: 'Codeaf-Time', sig: 'Codeaf-Sig' };

for (const c of vectors.frames) {
  test(`frame: ${c.name}`, () => {
    if (!c.ok || c.js_stricter) return assert.throws(() => decode(bytes(c.frame)), BadFrame);
    const { cellKeyId, objects } = decode(bytes(c.frame));
    assert.equal(cellKeyId, c.cell_key_id);
    assert.deepEqual(objects.map((o) => o.rid), c.rids);
    assert.deepEqual(objects.map((o) => o.bytes.length), c.lens);
  });
}

for (const c of vectors.requests) {
  test(`request: ${c.name}`, async () => {
    const headers = Object.fromEntries(Object.entries(H).flatMap(([k, h]) => (c.headers[h] ? [[k, c.headers[h]]] : [])));
    const run = () => verify({ method: c.method, uri: c.uri, headers }, bytes(c.body), c.now_ms);
    if (c.refusal) return assert.rejects(run, (e) => e instanceof Refusal && e.kind === c.refusal);
    assert.deepEqual(await run(), { identity: c.identity, device: c.device });
  });
}

const apply = { acquire: (c, r) => rules.acquire(r.cell, r.device, r.now, r.arg?.force === true),
  heartbeat: (c, r) => rules.heartbeat(r.cell, r.device, r.arg, r.now),
  publish: (c, r) => rules.publishTo(r.cell, r.device, r.arg, r.now),
  release: (c, r) => rules.releaseOf(r.cell, r.device, r.arg.fence),
  create: (c, r) => rules.created(r.arg, r.device, r.now) };

for (const c of vectors.rules) {
  test(`rule: ${c.name}`, () => {
    if (c.err) return assert.throws(() => apply[c.op](null, c), (e) => e.code === c.err);
    assert.deepEqual(apply[c.op](null, c), c.want);
  });
}
