// worker.test.js drives the Worker's routes against an in-memory KV stub.

import test from 'node:test';
import assert from 'node:assert/strict';

import worker from '../src/worker.js';

// KV is the smallest store the Worker needs: get, put and one list page.
class KV {
  constructor() {
    this.map = new Map();
  }
  async get(key) {
    return this.map.has(key) ? this.map.get(key) : null;
  }
  async put(key, value) {
    this.map.set(key, value);
  }
  async list({ prefix = '', cursor } = {}) {
    void cursor;
    const keys = [...this.map.keys()]
      .filter((k) => k.startsWith(prefix))
      .sort()
      .map((name) => ({ name }));
    return { keys, list_complete: true, cursor: undefined };
  }
}

const INSTALL = '0123456789abcdef0123456789abcdef';

function payload(overrides = {}) {
  return {
    schema: 1, metric: 'role_quality', role: 'worker',
    model: 'z-ai/glm-5.3-flash', score: 50,
    judge: 'anthropic/claude-opus-5', door: 'task', size: 'M',
    day: '2026-09-17', ...overrides,
  };
}

function line(overrides = {}) {
  return JSON.stringify({
    schema: 1, day: '2026-09-17', nonce: '0123456789abcdef0123456789abcdef',
    payload: payload(overrides),
  });
}

// One Ed25519 pair for the whole file, exported as standard base64 the way the
// secret and the var are held.
const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']);
const privJwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
const pubJwk = await crypto.subtle.exportKey('jwk', pair.publicKey);

function env(extra = {}) {
  return {
    POOL: new KV(),
    POOL_SIGNING_KEY: fromBase64Url(privJwk.d),
    POOL_PUBLIC_KEY: fromBase64Url(pubJwk.x),
    MIN_INSTALLS: '3',
    ROWS_PER_INSTALL_PER_DAY: '500',
    ...extra,
  };
}

function fromBase64Url(text) {
  const base = text.replace(/-/g, '+').replace(/_/g, '/');
  return base + '='.repeat((4 - (base.length % 4)) % 4);
}

function post(body, headers = {}) {
  return new Request('https://codeaf.agentfield.ai/pool/v1/rows', {
    method: 'POST', headers, body,
  });
}

test('healthz answers ok and other paths answer an empty 404', async () => {
  const e = env();
  const ok = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/healthz'), e);
  assert.equal(ok.status, 200);
  assert.equal(await ok.text(), 'ok');
  for (const path of ['/other', '/', '/pool', '/pool/nope']) {
    const res = await worker.fetch(new Request(`https://codeaf.agentfield.ai${path}`), e);
    assert.equal(res.status, 404, path);
    assert.equal(await res.text(), '');
  }
});

test('submit requires the install header', async () => {
  const res = await worker.fetch(post(line()), env());
  assert.equal(res.status, 400);
});

test('submit refuses a bad line, naming its number, and stores nothing', async () => {
  const e = env();
  const body = line() + '\n' + '{"schema":1}' + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 400);
  assert.match((await res.json()).error, /line 2/);
  assert.equal(e.POOL.map.size, 0);
});

test('submit refuses an oversized body and too many lines', async () => {
  const e = env();
  const big = await worker.fetch(post('x'.repeat((256 << 10) + 1), { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(big.status, 413);
  const many = line() + '\n';
  const body = many.repeat(201);
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 413);
});

test('submit folds a batch into sheets and answers 202', async () => {
  const e = env();
  const body = line({ score: 50 }) + '\n' + line({ score: 60 }) + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 202);
  assert.deepEqual(await res.json(), { accepted: 2 });
  const stored = JSON.parse(e.POOL.map.get(
    `sheet/${INSTALL}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M`,
  ));
  assert.deepEqual(stored, { n: 2, s: 110, s2: 6100 });
  assert.equal(e.POOL.map.get(`quota/${INSTALL}/2026-09-17`), '2');
});

test('submit refuses a batch past the daily quota and stores nothing', async () => {
  const e = env({ ROWS_PER_INSTALL_PER_DAY: '1' });
  const body = line() + '\n' + line() + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 429);
  assert.equal(e.POOL.map.size, 0);
});

test('the index is 404 before the first publication', async () => {
  const e = env();
  const res = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e);
  assert.equal(res.status, 404);
});

test('scheduled publishes a signed index, served with a version ETag and 304', async () => {
  const e = env();
  for (let i = 0; i < 3; i++) {
    const name = `sheet/install${i}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M`;
    await e.POOL.put(name, JSON.stringify({ n: 4, s: 200 + i * 4, s2: 10000 }));
  }
  await worker.scheduled({}, e);

  const docRes = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e);
  assert.equal(docRes.status, 200);
  const etag = docRes.headers.get('ETag');
  assert.equal(docRes.headers.get('Cache-Control'), 'public, max-age=300');
  const doc = await docRes.text();
  const parsed = JSON.parse(doc);
  assert.equal(Number.isInteger(parsed.version), true);
  assert.equal(parsed.cells.length, 1);
  assert.equal(parsed.cells[0].installs, 3);

  const sigRes = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json.sig'), e);
  assert.equal(sigRes.status, 200);
  const sig = Uint8Array.from(atob((await sigRes.text()).trim()), (c) => c.charCodeAt(0));
  const pub = await crypto.subtle.importKey(
    'jwk', { kty: 'OKP', crv: 'Ed25519', x: pubJwk.x }, { name: 'Ed25519' }, false, ['verify'],
  );
  const verified = await crypto.subtle.verify(
    { name: 'Ed25519' }, pub, sig, new TextEncoder().encode(doc),
  );
  assert.equal(verified, true);

  const notModified = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json', {
    headers: { 'If-None-Match': etag },
  }), e);
  assert.equal(notModified.status, 304);
});

test('scheduled with no cells still publishes an empty index', async () => {
  const e = env();
  await worker.scheduled({}, e);
  const res = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e);
  assert.equal(res.status, 200);
  assert.deepEqual(JSON.parse(await res.text()).cells, []);
});
