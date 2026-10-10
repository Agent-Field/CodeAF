import test from 'node:test';
import assert from 'node:assert/strict';
import { ENGINE_ON_THIS_MAC, engineWhere, readEngineWhere } from './enginePlace.ts';

test('a loopback desktop connection is On this Mac, and any other host is named', () => {
  for (const url of ['http://127.0.0.1:1423', 'http://localhost:9/', 'http://[::1]:1423']) {
    assert.equal(engineWhere({ desktop: true, connectionUrl: url }), ENGINE_ON_THIS_MAC);
  }
  assert.equal(engineWhere({ desktop: true, connectionUrl: 'http://build-host.local:1423' }), 'build-host.local');
  assert.equal(engineWhere({ desktop: true }), '');
  assert.equal(engineWhere({ desktop: true, connectionUrl: 'not a url' }), '');
});

test('the browser build shows the dev transport address and nothing else', () => {
  assert.equal(engineWhere({ desktop: false, devAddress: 'http://127.0.0.1:1423' }), 'http://127.0.0.1:1423');
  assert.equal(engineWhere({ desktop: false, devAddress: '  ' }), '');
  assert.equal(engineWhere({ desktop: false, connectionUrl: 'http://127.0.0.1:1', devAddress: 'http://127.0.0.1:1423' }), 'http://127.0.0.1:1423');
});

test('a health answer is connected, and a missing answer is not', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ status: 'ready' }), { status: 200 }));
  assert.deepEqual(await readEngineWhere(), { ok: true, where: '' });
  t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ error: 'no' }), { status: 404 }));
  assert.deepEqual(await readEngineWhere(), { ok: false, where: '' });
  t.mock.method(globalThis, 'fetch', async () => { throw new TypeError('offline'); });
  assert.deepEqual(await readEngineWhere(), { ok: false, where: '' });
});

test('an abandoned check is not reported as a failure of its own', async (t) => {
  const caller = new AbortController();
  caller.abort();
  t.mock.method(globalThis, 'fetch', async () => new Response('{}', { status: 200 }));
  await assert.rejects(readEngineWhere(caller.signal), (error: unknown) => error instanceof DOMException && error.name === 'AbortError');
});
