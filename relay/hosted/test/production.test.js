// The production configuration's front door: with its own BASE_PATH, the pairing page is served on
// the bare host and every other path outside /fabric is a 404, and its routes name exactly those two.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { dispatch } from '../src/front.js';
import { respond } from '../src/wire.js';

const config = readFileSync(new URL('../production.toml', import.meta.url), 'utf8');
const env = { BASE_PATH: /^BASE_PATH = "([^"]*)"/m.exec(config)[1] };
const fetchAt = (path, method = 'GET') => respond(() => dispatch(new Request('https://codeaf.agentfield.ai' + path, { method }), env));

test('the production config routes the wire and the pairing page, and nothing else', () => {
  const patterns = [...config.matchAll(/pattern = "([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(patterns, ['codeaf.agentfield.ai/fabric*', 'codeaf.agentfield.ai/p/*']);
  assert.equal(env.BASE_PATH, '/fabric');
});

test('a pairing link opens the page with the strict CSP', async () => {
  const res = await fetchAt('/p/k7m2q9xd');
  assert.equal(res.status, 200);
  assert.match(res.headers.get('content-type'), /^text\/html/);
  const csp = res.headers.get('content-security-policy');
  assert.match(csp, /default-src 'none'/);
  assert.doesNotMatch(csp, /unsafe|\*/);
});

test('every other path outside /fabric is a 404', async () => {
  for (const path of ['/', '/anything', '/v1/dir/list', '/fabricx/v1/store/stats', '/p/', '/p/a/b', '/other/p/abc']) {
    assert.equal((await fetchAt(path)).status, 404, path);
  }
  assert.equal((await fetchAt('/p/k7m2q9xd', 'POST')).status, 404);
});
