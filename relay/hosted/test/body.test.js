import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readBody } from '../src/body.js';

const request = (body, headers = {}) => new Request('http://x/', { method: 'POST', body, headers, duplex: 'half' });
const refusal = (p) => p.then(() => null, (e) => e);

test('a body within its cap is read whole', async () => {
  assert.deepEqual(await readBody(request(new Uint8Array([1, 2, 3])), 3, 'too_large'), new Uint8Array([1, 2, 3]));
});

test('a body one byte over its cap is 413 with the route code, never 400', async () => {
  const e = await refusal(readBody(request(new Uint8Array(4)), 3, 'bad_frame'));
  assert.deepEqual([e.status, e.code], [413, 'bad_frame']);
});

test('a stream with no length is stopped at the cap, not after the whole body', async () => {
  let pulled = 0;
  const stream = new ReadableStream({
    pull(c) {
      pulled++;
      c.enqueue(new Uint8Array(1024));
      if (pulled === 1000) c.close();
    },
  });
  const e = await refusal(readBody(request(stream), 4096, 'too_large'));
  assert.equal(e.status, 413);
  assert.ok(pulled < 20, `read ${pulled} chunks`);
});

test('a declared length over the cap is refused before a byte is read', async () => {
  const req = request(new Uint8Array(1), { 'content-length': '999' });
  assert.equal((await refusal(readBody(req, 10, 'too_large'))).status, 413);
});
