// worker.test.js drives the Worker's routes against an in-memory KV stub.

import test from 'node:test';
import assert from 'node:assert/strict';

import worker from '../src/worker.js';

// KV is the smallest store the Worker needs: get, put and one list page.
// put honours an expirationTtl or expiration option the way the lock needs.
class KV {
  constructor() {
    this.map = new Map();
    this.expiry = new Map();
  }
  async get(key) {
    if (!this.map.has(key)) {
      return null;
    }
    const until = this.expiry.get(key);
    if (until !== undefined && Date.now() >= until) {
      this.map.delete(key);
      this.expiry.delete(key);
      return null;
    }
    return this.map.get(key);
  }
  async put(key, value, options = {}) {
    this.map.set(key, value);
    if (Number.isFinite(options.expirationTtl)) {
      this.expiry.set(key, Date.now() + options.expirationTtl * 1000);
    } else if (Number.isFinite(options.expiration)) {
      this.expiry.set(key, options.expiration * 1000);
    } else {
      this.expiry.delete(key);
    }
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

// Two distinct 32-hex nonces, one per row when a test folds more than one.
const NONCE_A = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const NONCE_B = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';

function payload(overrides = {}) {
  return {
    schema: 1, metric: 'role_quality', role: 'worker',
    model: 'z-ai/glm-5.3-flash', score: 50,
    judge: 'anthropic/claude-opus-5', door: 'task', size: 'M',
    day: '2026-09-17', ...overrides,
  };
}

function line({ nonce = '0123456789abcdef0123456789abcdef', ...overrides } = {}) {
  return JSON.stringify({
    schema: 1, day: '2026-09-17', nonce,
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

// background collects the waitUntil promises a read schedules; drain awaits
// everything the reads scheduled, the way the platform settles them after
// the response.
function background() {
  return { pending: [], waitUntil(promise) { this.pending.push(promise); } };
}
async function drain(ctx) {
  await Promise.all(ctx.pending.splice(0));
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
  const body = line({ nonce: NONCE_A, score: 50 }) + '\n' + line({ nonce: NONCE_B, score: 60 }) + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 202);
  assert.deepEqual(await res.json(), { accepted: 2 });
  const stored = JSON.parse(e.POOL.map.get(
    `sheet/${INSTALL}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M`,
  ));
  assert.deepEqual(stored, { n: 2, s: 110, s2: 6100 });
  assert.equal(e.POOL.map.get(`quota/${INSTALL}/2026-09-17`), '2');
});

test('a retried batch is folded once, charged once, and still answers 202', async () => {
  const e = env();
  const body = line({ nonce: NONCE_A, score: 50 }) + '\n' + line({ nonce: NONCE_B, score: 60 }) + '\n';
  const first = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(first.status, 202);
  assert.deepEqual(await first.json(), { accepted: 2 });

  const second = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(second.status, 202);
  assert.deepEqual(await second.json(), { accepted: 0 });

  const stored = JSON.parse(e.POOL.map.get(
    `sheet/${INSTALL}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M`,
  ));
  assert.deepEqual(stored, { n: 2, s: 110, s2: 6100 });
  assert.equal(e.POOL.map.get(`quota/${INSTALL}/2026-09-17`), '2');
  // The identity survives the retry only as long as its TTL; a week is stored.
  assert.equal(typeof e.POOL.expiry.get(`seen/${INSTALL}/${NONCE_A}`), 'number');
});

test('a batch that repeats a nonce already stored and adds one new row folds only the new row', async () => {
  const e = env();
  const first = await worker.fetch(
    post(line({ nonce: NONCE_A, score: 50 }), { 'X-Codeaf-Install': INSTALL }), e,
  );
  assert.equal(first.status, 202);

  const body = line({ nonce: NONCE_A, score: 90 }) + '\n' + line({ nonce: NONCE_B, score: 60 }) + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 202);
  assert.deepEqual(await res.json(), { accepted: 1 });

  const stored = JSON.parse(e.POOL.map.get(
    `sheet/${INSTALL}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M`,
  ));
  assert.deepEqual(stored, { n: 2, s: 110, s2: 6100 });
  assert.equal(e.POOL.map.get(`quota/${INSTALL}/2026-09-17`), '2');
});

test('a nonce repeated inside one batch folds once', async () => {
  const e = env();
  const body = line({ nonce: NONCE_A, score: 50 }) + '\n' + line({ nonce: NONCE_A, score: 90 })
    + '\n' + line({ nonce: NONCE_B, score: 60 }) + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 202);
  assert.deepEqual(await res.json(), { accepted: 2 });

  const stored = JSON.parse(e.POOL.map.get(
    `sheet/${INSTALL}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M`,
  ));
  assert.deepEqual(stored, { n: 2, s: 110, s2: 6100 });
  assert.equal(e.POOL.map.get(`quota/${INSTALL}/2026-09-17`), '2');
});

test('a row on the exec or run door is accepted and folded, and a row on a door outside the four is refused naming them', async () => {
  const e = env();
  const body = line({ nonce: NONCE_A, door: 'exec' }) + '\n' + line({ nonce: NONCE_B, door: 'run' }) + '\n';
  const res = await worker.fetch(post(body, { 'X-Codeaf-Install': INSTALL }), e);
  assert.equal(res.status, 202);
  assert.deepEqual(await res.json(), { accepted: 2 });
  const cell = `sheet/${INSTALL}/2026-09-17/worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|`;
  assert.deepEqual(JSON.parse(e.POOL.map.get(`${cell}exec|M`)), { n: 1, s: 50, s2: 2500 });
  assert.deepEqual(JSON.parse(e.POOL.map.get(`${cell}run|M`)), { n: 1, s: 50, s2: 2500 });

  const refused = await worker.fetch(post(line({ door: 'shell' }), { 'X-Codeaf-Install': INSTALL }), env());
  assert.equal(refused.status, 400);
  assert.match((await refused.json()).error, /door must be task, do, exec or run/);
});

test('submit refuses a batch past the daily quota and stores nothing', async () => {
  const e = env({ ROWS_PER_INSTALL_PER_DAY: '1' });
  const body = line({ nonce: NONCE_A }) + '\n' + line({ nonce: NONCE_B }) + '\n';
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

test('a read with nothing stored answers 404 once and the next read answers 200 after the background publish', async () => {
  const e = env();
  const ctx = background();
  const first = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e, ctx);
  assert.equal(first.status, 404);
  await drain(ctx);
  const second = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e);
  assert.equal(second.status, 200);
  assert.equal(JSON.parse(await second.text()).cells.length, 0);
});

test('a read of a document older than PUBLISH_EVERY serves the old document and republishes exactly once when two reads race', async () => {
  const e = env();
  const old = Math.floor(Date.now() / 1000) - 7200;
  const stale = JSON.stringify({ version: old, cells: [] });
  await e.POOL.put('index/doc', stale);
  await e.POOL.put('index/sig', 'old-signature');
  await e.POOL.put('index/version', String(old));
  let publishes = 0;
  const put = e.POOL.put.bind(e.POOL);
  e.POOL.put = async (key, value, options) => {
    if (key === 'index/doc') {
      publishes++;
    }
    return put(key, value, options);
  };
  const ctx = background();
  const url = 'https://codeaf.agentfield.ai/pool/index.json';
  const [a, b] = await Promise.all([
    worker.fetch(new Request(url), e, ctx),
    worker.fetch(new Request(url), e, ctx),
  ]);
  assert.equal(a.status, 200);
  assert.equal(b.status, 200);
  assert.equal(await a.text(), stale);
  assert.equal(await b.text(), stale);
  await drain(ctx);
  assert.equal(publishes, 1);
  assert.notEqual(await e.POOL.get('index/publishing'), null);
  const version = parseInt(await e.POOL.get('index/version'), 10);
  assert.equal(version > old, true);
});

test('a read of a fresh document republishes nothing', async () => {
  const e = env();
  await worker.scheduled({}, e);
  const version = await e.POOL.get('index/version');
  const ctx = background();
  const res = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e, ctx);
  assert.equal(res.status, 200);
  await drain(ctx);
  assert.equal(ctx.pending.length, 0);
  assert.equal(await e.POOL.get('index/version'), version);
});

test('scheduled still publishes', async () => {
  const e = env();
  await worker.scheduled({}, e);
  const res = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e);
  assert.equal(res.status, 200);
  assert.equal(Number.isInteger(JSON.parse(await res.text()).version), true);
});

test('scheduled with no cells still publishes an empty index', async () => {
  const e = env();
  await worker.scheduled({}, e);
  const res = await worker.fetch(new Request('https://codeaf.agentfield.ai/pool/index.json'), e);
  assert.equal(res.status, 200);
  assert.deepEqual(JSON.parse(await res.text()).cells, []);
});
