import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { patchProxy } from './local-proxy.js';

const source = readFileSync(new URL('../node_modules/wrangler/wrangler-dist/ProxyWorker.js', import.meta.url), 'utf8');

async function proxyFixture(replies, run) {
  const patched = patchProxy(source, 2);
  const { ProxyWorker } = await import('data:text/javascript;base64,' + Buffer.from(patched).toString('base64'));
  const original = { fetch: globalThis.fetch, timer: globalThis.setTimeout, warn: console.warn, getAll: Headers.prototype.getAll };
  // Workerd's Headers exposes getAll for cookies; the Node fixture supplies
  // that runtime method without changing the proxy code being exercised.
  Headers.prototype.getAll = function (name) {
    assert.equal(name.toLowerCase(), 'set-cookie');
    return this.getSetCookie();
  };
  const requests = [], notices = [];
  globalThis.fetch = async (url, req) => {
    requests.push({ url: String(url), body: await req.text(), signature: req.headers.get('authorization') });
    const reply = replies[requests.length - 1] ?? replies.at(-1);
    if (reply instanceof Error) throw reply;
    return new Response(reply.body, { status: reply.status });
  };
  globalThis.setTimeout = (fn) => { fn(); return 0; };
  console.warn = (line) => notices.push(line);
  try {
    const proxy = new ProxyWorker({}, { PROXY_CONTROLLER: { fetch: async () => new Response(null, { status: 204 }) } });
    proxy.proxyData = { userWorkerUrl: { protocol: 'http:', hostname: 'worker.test', port: '8787' } };
    await run(proxy, requests, notices);
  } finally {
    globalThis.fetch = original.fetch;
    globalThis.setTimeout = original.timer;
    console.warn = original.warn;
    if (original.getAll) Headers.prototype.getAll = original.getAll;
    else delete Headers.prototype.getAll;
  }
}

const request = () => new Request('http://proxy.test/v1/store/frames', { method: 'POST', headers: { authorization: 'same signature' }, body: 'signed bytes' });

// C19/C22 retry a dropped proxy connection for POST with identical signed bytes.
test('C19/C22 local proxy retries only its dropped connection and reports each retry', async () => {
  await proxyFixture([new Error('Network connection lost.'), new Error('Network connection lost.'), { status: 200, body: 'response bytes' }], async (proxy, calls, notices) => {
    const res = await proxy.fetch(request());
    assert.equal(res.status, 200);
    assert.equal(await res.text(), 'response bytes');
    assert.equal(calls.length, 3);
    for (const call of calls) assert.deepEqual({ body: call.body, signature: call.signature }, { body: 'signed bytes', signature: 'same signature' });
    assert.equal(notices.filter((line) => line === 'codeaf local proxy transport retry: Network connection lost.').length, 2);
  });
});

// C19 keeps application responses and every unrelated transport failure red.
test('C19 local proxy preserves application failures and other transport errors', async () => {
  for (const reply of [{ status: 409, body: 'conflict' }, { status: 500, body: 'Network connection lost.' }]) {
    await proxyFixture([reply], async (proxy, calls, notices) => {
      const res = await proxy.fetch(request());
      assert.equal(res.status, reply.status);
      assert.equal(await res.text(), reply.body);
      assert.equal(calls.length, 1);
      assert.equal(notices.length, 0);
    });
  }
  await proxyFixture([new Error('EOF')], async (proxy, calls, notices) => {
    await assert.rejects(proxy.fetch(request()), { message: 'EOF' });
    assert.equal(calls.length, 1);
    assert.equal(notices.length, 0);
  });
});

// C19/C22 bound retry exhaustion and preserve the actual final transport error.
test('C19/C22 local proxy exhausts its retry limit without hiding failure', async () => {
  await proxyFixture([new Error('Network connection lost.')], async (proxy, calls, notices) => {
    await assert.rejects(proxy.fetch(request()), { message: 'Network connection lost.' });
    assert.equal(calls.length, 3);
    assert.equal(notices.filter((line) => line === 'codeaf local proxy transport retry: Network connection lost.').length, 2);
  });
});

// C19/C22 refuse an unknown development runtime rather than silently skipping the policy.
test('C19/C22 local proxy preparation refuses an unknown runtime', () => {
  assert.throws(() => patchProxy('a different runtime', 2), /unsupported local Wrangler proxy structure/);
});
