// The pairing link's page: fixed headers, no network in the body, and no lookup of the code.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { isPagePath, servePage } from '../src/page.js';

const get = (path, method = 'GET') => new Request('https://h.example' + path, { method });
// servePage takes the request alone: it is given no binding, so it cannot reach the directory.
const fetchPage = (path) => servePage(get(path));

test('the page carries the strict headers', async () => {
  const res = await fetchPage('/p/k7m2q9xd');
  assert.equal(res.status, 200);
  assert.match(res.headers.get('content-type'), /^text\/html/);
  assert.equal(res.headers.get('referrer-policy'), 'no-referrer');
  assert.equal(res.headers.get('cache-control'), 'no-store');
  const csp = res.headers.get('content-security-policy');
  assert.match(csp, /default-src 'none'/);
  assert.match(csp, /style-src 'sha256-[A-Za-z0-9+/=]+'/);
  assert.match(csp, /script-src 'sha256-[A-Za-z0-9+/=]+'/);
  assert.doesNotMatch(csp, /unsafe|\*/);
});

test('the CSP hashes match the inline style and script', async () => {
  const res = await fetchPage('/p/abc');
  const html = await res.text();
  const csp = res.headers.get('content-security-policy');
  for (const [tag, directive] of [['style', 'style-src'], ['script', 'script-src']]) {
    const text = new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`).exec(html)[1];
    const sum = Buffer.from(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))).toString('base64');
    assert.ok(csp.includes(`${directive} 'sha256-${sum}'`), directive);
  }
});

test('the body makes no request and names no outside address', async () => {
  const html = await (await fetchPage('/p/k7m2q9xd')).text();
  assert.doesNotMatch(html, /fetch|XMLHttpRequest|sendBeacon|WebSocket|EventSource|localStorage|sessionStorage|cookie/i);
  assert.doesNotMatch(html, /https?:|(?<!codeaf:)\/\/|<[^>]+\s(src|href)=|@import|url\(/i);
  assert.match(html, /codeaf pair approve/);
});

test('the page is the same for any code, and the handler is given no bindings', async () => {
  const [a, b] = await Promise.all([fetchPage('/p/k7m2q9xd'), fetchPage('/p/zzzzzzzz')]);
  assert.equal(await a.text(), await b.text());
  assert.equal(servePage.length, 1);
});

test('the page text uses none of the barred words', async () => {
  const html = (await (await fetchPage('/p/x')).text()).replace(/<script>[\s\S]*?<\/script>|<style>[\s\S]*?<\/style>/g, '');
  assert.doesNotMatch(html, /\b(node|relay|take|lease|manifest)\b/i);
});

test('only a GET of one code is a page', async () => {
  await assert.rejects(servePage(get('/p/x', 'POST')), { code: 'not_found' });
  await assert.rejects(fetchPage('/p/'), { code: 'not_found' });
  await assert.rejects(fetchPage('/p/a/b'), { code: 'not_found' });
  assert.ok(isPagePath(get('/p/x')) && !isPagePath(get('/v1/dir/list')));
});

test('the deep link is the one the app reads (shared vector with internal/pair)', async () => {
  const { DEEP_LINK } = await import('../src/page.js');
  const vector = JSON.parse(readFileSync(new URL('../../../internal/pair/testdata/pagelink.json', import.meta.url)));
  const deepLink = new Function(`${DEEP_LINK}; return deepLink;`)();
  assert.equal(deepLink(vector.code, vector.key), vector.url);
  assert.equal(deepLink(vector.code, ''), `codeaf://pair?code=${vector.code}`);
  const html = await (await fetchPage('/p/' + vector.code)).text();
  assert.ok(html.includes(DEEP_LINK) && html.includes('deepLink(code,key)'));
});
