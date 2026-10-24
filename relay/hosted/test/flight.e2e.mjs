// The rule "Has never answers no while a put for the same identity is still being received",
// and its price: one identity's slow put never delays another identity's Has. A slow put is
// a body sent in two halves with a pause between (trickle). Not part of `npm test`; `npm run e2e` runs it.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, signed } from './party.js';
import { frame, rid } from './helpers.js';
import { call, trickle } from './client.js';

const PAUSE_MS = 1500;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const timed = async (fn) => {
  const t0 = performance.now();
  const answer = await fn();
  return { answer, ms: performance.now() - t0 };
};

const mine = await newDevice(await newIdentity());
const myOther = await newDevice(mine.identity);
const theirs = await newDevice(await newIdentity());
const f = frame([rid(1)]);

const putting = trickle(await signed(mine, 'POST', '/v1/store/frames', f.bytes), '/v1/store/frames', f.bytes, PAUSE_MS);
await sleep(300);

const other = await timed(() => call(theirs, 'POST', '/v1/store/has', { rids: [rid(1)] }));
assert.ok(other.ms < PAUSE_MS / 2, `another identity waited ${other.ms} ms`);
assert.deepEqual(other.answer.json, { have: [false] });

const same = await timed(() => call(myOther, 'POST', '/v1/store/has', { rids: [rid(1)] }));
assert.ok(same.ms > PAUSE_MS - 500, `Has answered in ${same.ms} ms, before the put finished`);
assert.deepEqual(same.answer.json, { have: [true] }, 'never no while a put is arriving');
assert.equal(await putting, 200);

// A stranger holding no cert of this identity cannot hold its Has hostage with a put it never finishes.
const forged = { ...(await signed(theirs, 'POST', '/v1/store/frames', f.bytes)), 'codeaf-identity': (await signed(mine, 'GET', '/')) ['codeaf-identity'] };
const attack = trickle(forged, '/v1/store/frames', f.bytes, PAUSE_MS);
await sleep(300);
const free = await timed(() => call(myOther, 'POST', '/v1/store/has', { rids: [rid(1)] }));
assert.ok(free.ms < PAUSE_MS / 2, `a forged put delayed Has by ${free.ms} ms`);
assert.equal(await attack, 401);
console.log('flight: all pass');
