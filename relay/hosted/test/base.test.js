// The base path: the prefix is stripped once for routing, put back for the signature, and a request
// outside it is a 404.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { prefixOf, signedUri, stripBase } from '../src/base.js';

const at = (path, init) => new Request('https://h.example' + path, init);

test('prefixOf normalises the variable', () => {
  assert.equal(prefixOf({}), '');
  assert.equal(prefixOf({ BASE_PATH: '' }), '');
  assert.equal(prefixOf({ BASE_PATH: '/fabric' }), '/fabric');
  assert.equal(prefixOf({ BASE_PATH: '/fabric/' }), '/fabric');
  assert.equal(prefixOf({ BASE_PATH: 'fabric' }), '/fabric');
});

test('no prefix leaves the request as it came', () => {
  const r = at('/v1/store/stats');
  assert.equal(stripBase(r, {}), r);
});

test('the prefix is removed, with query and method kept', async () => {
  const out = stripBase(at('/fabric/v1/store/has?x=1', { method: 'POST', body: 'hi' }), { BASE_PATH: '/fabric' });
  const url = new URL(out.url);
  assert.equal(url.pathname + url.search, '/v1/store/has?x=1');
  assert.equal(out.method, 'POST');
  assert.equal(await out.text(), 'hi');
});

test('paths outside the prefix, and lookalikes, are outside', () => {
  const env = { BASE_PATH: '/fabric' };
  for (const p of ['/v1/store/stats', '/fabricx/v1/store/stats', '/other']) assert.equal(stripBase(at(p), env), null, p);
});

test('the signed uri is the client path, prefix included', () => {
  const url = new URL('https://h.example/v1/dir/list?a=b');
  assert.equal(signedUri({ BASE_PATH: '/fabric' }, url), '/fabric/v1/dir/list?a=b');
  assert.equal(signedUri({}, url), '/v1/dir/list?a=b');
});

