import test from 'node:test';
import assert from 'node:assert/strict';
import { mockIPC, clearMocks } from '@tauri-apps/api/mocks';
import { engineFetch } from './engineFetch.ts';

test('browser engine requests preserve the proxy transport and abort signal', async () => {
 const prior = globalThis.fetch;
 const signal = new AbortController().signal;
 try {
  globalThis.fetch = async (url, init) => { assert.equal(url, '/api/engine/world'); assert.equal(init?.signal, signal); return Response.json({ rows: [] }); };
  assert.deepEqual(await (await engineFetch('/api/engine/world', { signal })).json(), { rows: [] });
 } finally { globalThis.fetch = prior; }
});

test('native transport validates local engine paths before any IPC request', async () => {
 const globals = globalThis as unknown as { isTauri?: boolean; window?: object };
 globals.isTauri = true; globals.window = {};
 let calls = 0; mockIPC(() => { calls++; });
 try {
  for (const url of ['https://example.com/api/engine/world', 'http://127.0.0.1:1234/not-engine', 'http://user@127.0.0.1:1234/api/engine/world', 'http://127.0.0.1:1234/api/engine/../../admin']) {
   await assert.rejects(engineFetch(url), /invalid connection/);
  }
  assert.equal(calls, 0);
 } finally { clearMocks(); delete globals.isTauri; delete globals.window; }
});

test('native streaming response preserves chunks and releases its body when the consumer cancels', async () => {
 const globals = globalThis as unknown as { isTauri?: boolean; window?: object };
 globals.isTauri = true; globals.window = {};
 let dropped = false;
 mockIPC((command, payload) => {
  if (command === 'plugin:http|fetch') {
   const request = payload?.clientConfig as { url: string; maxRedirections: number; headers: [string,string][] };
   assert.equal(request.url, 'http://127.0.0.1:1234/api/engine/world/stream');
   assert.equal(request.maxRedirections, 0);
   assert.ok(request.headers.some(([key,value]) => key.toLowerCase() === 'authorization' && value === 'Bearer test-only'));
   return 1;
  }
  if (command === 'plugin:http|fetch_send') return { status: 200, statusText: 'OK', headers: [['content-type','text/event-stream']], url: 'http://127.0.0.1:1234/api/engine/world/stream', rid: 2 };
  if (command === 'plugin:http|fetch_read_body') return Uint8Array.from([...new TextEncoder().encode('data: résumé\n\n'),0]).buffer;
  if (command === 'plugin:http|fetch_cancel_body') { dropped = true; return; }
  throw new Error(`Unexpected test IPC command: ${command}`);
 });
 try {
  const response = await engineFetch('http://127.0.0.1:1234/api/engine/world/stream', { headers: { Authorization: 'Bearer test-only' } });
  assert.equal(response.headers.get('content-type'), 'text/event-stream');
  const reader = response.body!.getReader();
  assert.equal(new TextDecoder().decode((await reader.read()).value), 'data: résumé\n\n');
  await reader.cancel(); assert.equal(dropped, true);
 } finally { clearMocks(); delete globals.isTauri; delete globals.window; }
});
