import test from 'node:test';
import assert from 'node:assert/strict';
import { displayParts, initialWebState, monogram, parseAddress, webReducer } from './model.ts';
import { webStateSentence } from './strings.ts';

test('parseAddress refuses file:, javascript:, data:', () => {
  for (const text of ['file:///etc/passwd', 'javascript:alert(1)', 'data:text/html,hi', 'ftp://x.com']) assert.equal(parseAddress(text), null, text);
});

test('bare host becomes https', () => {
  assert.equal(parseAddress('pkg.go.dev/fmt')?.href, 'https://pkg.go.dev/fmt');
  assert.equal(parseAddress('http://a.com/')?.protocol, 'http:');
});

test('spaces make it a question, not an address', () => {
  assert.equal(parseAddress('what is go'), null);
  assert.equal(parseAddress('   '), null);
});

test('displayParts splits the site from the rest', () => {
  assert.deepEqual(displayParts(parseAddress('https://www.a.com/x?y=1')!), { host: 'a.com', rest: '/x?y=1' });
});

test('monogram is stable', () => {
  assert.deepEqual(monogram('github.com'), monogram('github.com'));
  assert.equal(monogram('github.com').letter, 'G');
});

test('reducer clears error on a new load', () => {
  const failed = webReducer(webReducer(initialWebState, { type: 'load', url: 'https://a.com/' }), { type: 'error', error: 'offline' });
  assert.equal(failed.error, 'offline');
  assert.equal(failed.loading, false);
  const next = webReducer(failed, { type: 'load', url: 'https://b.com/' });
  assert.equal(next.error, 'none');
  assert.equal(next.loading, true);
});

test('progress is clamped and ignored when not loading', () => {
  assert.equal(webReducer(initialWebState, { type: 'progress', progress: 0.5 }).progress, null);
  assert.equal(webReducer(webReducer(initialWebState, { type: 'load', url: 'https://a.com/' }), { type: 'progress', progress: 3 }).progress, 1);
});

test('every error kind but none has a sentence', () => {
  for (const [kind, s] of Object.entries(webStateSentence)) assert.equal(s === null, kind === 'none');
});
