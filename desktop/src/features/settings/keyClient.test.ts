import test, { type TestContext } from 'node:test';
import assert from 'node:assert/strict';
import { keyStatusWords } from './keyClient.ts';

function answer(t: TestContext, body: unknown, status = 200) {
 t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));
}

test('maps each source to words and ignores any value field', async t => {
 const cases: [unknown, string][] = [
  [{ present: true, source: 'OPENROUTER_API_KEY', key: 'sk-canary', length: 41 }, 'From OPENROUTER_API_KEY'],
  [{ present: true, source: 'OPENAI_API_KEY', value: 'sk-canary' }, 'From OPENAI_API_KEY'],
  [{ present: true, source: 'profile', mask: 'sk-…' }, 'Saved in your profile'],
  [{ present: false }, 'Not set'],
 ];
 for (const [body, words] of cases) {
  answer(t, body);
  const got = await keyStatusWords();
  assert.equal(got, words);
  assert.ok(!String(got).includes('canary'));
 }
});

test('a failed or malformed answer is unknown and renders nothing', async t => {
 answer(t, { error: 'down' }, 500);
 assert.equal(await keyStatusWords(), null);
 answer(t, { present: 'yes' });
 assert.equal(await keyStatusWords(), null);
 answer(t, { present: true });
 assert.equal(await keyStatusWords(), null);
});
