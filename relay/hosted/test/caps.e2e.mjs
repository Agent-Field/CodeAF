// The fair-use caps end to end, against a relay started with short limits (npm run e2e): 25 requests
// a minute per device, 40 per identity, 6 frames a day, 3000 stored bytes, 12 objects, 1 put at a
// time. Every refusal is one code the client can turn into one sentence, never silence.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, signed } from './party.js';
import { frame, rid } from './helpers.js';
import { TIGHT, call, answerOf, trickle } from './client.js';

const on = { base: TIGHT };
const fresh = async () => newDevice(await newIdentity());
const put = (dev, f) => call(dev, 'POST', '/v1/store/frames', f.bytes, on);
const stats = (dev) => call(dev, 'GET', '/v1/store/stats', undefined, on);

const tests = {
  async DeviceRateIsItsOwn() {
    const d1 = await fresh();
    const d2 = await newDevice(d1.identity);
    for (let i = 0; i < 25; i++) assert.equal((await stats(d1)).status, 200);
    const over = await stats(d1);
    assert.deepEqual([over.status, answerOf(over)], [429, 'rate_limited']);
    assert.ok(Number(over.headers.get('retry-after')) > 0, 'Retry-After names the wait');
    assert.equal((await stats(d2)).status, 200, 'the other device of the same identity is not held back');
  },

  async IdentityRateIsShared() {
    const d1 = await fresh();
    const d2 = await newDevice(d1.identity);
    for (let i = 0; i < 25; i++) await stats(d1);
    for (let i = 0; i < 15; i++) assert.equal((await stats(d2)).status, 200);
    assert.equal(answerOf(await stats(d2)), 'rate_limited', 'forty requests in all are the identity share');
  },

  async StoredBytes() {
    const dev = await fresh();
    assert.equal((await put(dev, frame([rid(1)], 'x'.repeat(2000)))).status, 200);
    const over = await put(dev, frame([rid(2)], 'y'.repeat(2000)));
    assert.deepEqual([over.status, answerOf(over)], [507, 'full']);
  },

  async StoredObjects() {
    const dev = await fresh();
    const over = await put(dev, frame(Array.from({ length: 13 }, (_, i) => rid(i + 1))));
    assert.deepEqual([over.status, answerOf(over)], [507, 'full']);
    assert.equal((await put(dev, frame(Array.from({ length: 12 }, (_, i) => rid(i + 1))))).status, 200);
  },

  async FramesPerDayCountOnlyNewFrames() {
    const dev = await fresh();
    for (let i = 1; i <= 6; i++) assert.equal((await put(dev, frame([rid(i)]))).status, 200);
    assert.equal(answerOf(await put(dev, frame([rid(7)]))), 'full');
    assert.equal((await put(dev, frame([rid(1)]))).status, 200, 'a frame already stored costs no quota');
  },

  async OnePutAtATime() {
    const dev = await fresh();
    const f = frame([rid(1)]);
    const slow = trickle(await signed(dev, 'POST', '/v1/store/frames', f.bytes), '/v1/store/frames', f.bytes, 800, TIGHT);
    await new Promise((r) => setTimeout(r, 200));
    const second = await put(dev, frame([rid(2)]));
    assert.deepEqual([second.status, answerOf(second)], [429, 'rate_limited']);
    assert.equal(await slow, 200);
    assert.equal((await put(dev, frame([rid(2)]))).status, 200, 'and the next one is fine');
  },

  async OversizedFrameIs413EvenWhenStreamed() {
    const dev = await fresh();
    const big = new Uint8Array((16 << 20) + 1);
    const headers = await signed(dev, 'POST', '/v1/store/frames', big);
    const res = await fetch(TIGHT + '/v1/store/frames', { method: 'POST', headers, body: new ReadableStream({ start(c) { c.enqueue(big); c.close(); } }), duplex: 'half' });
    assert.deepEqual([res.status, (await res.json()).err], [413, 'bad_frame']);
  },
};

for (const [name, run] of Object.entries(tests)) {
  await run();
  console.log('caps ok', name);
}
console.log('caps: all pass');
