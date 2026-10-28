import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openWorkerd } from './workerd.js';

// R27 preserves the complete local RPC boundary while its host stays small.
// The native denial service must also prevent an unplanned outbound request.
test('R27 local host preserves complete replies and refuses outbound traffic', { timeout: 15000 }, async () => {
  const out = await mkdtemp(join(tmpdir(), 'codeaf-host-'));
  const requestWithin = 5000;
  let runtime, attempts = 0;
  const canary = createServer((_, response) => { attempts++; response.end('unexpected'); });
  try {
    await new Promise(resolve => canary.listen(0, '127.0.0.1', resolve));
    await copyFile(join(process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-raft-proof', 'core.wasm'), join(out, 'core.wasm'));
    await copyFile('test/raft/storage-worker.js', join(out, 'storage-worker.js'));
    await writeFile(join(out, 'worker.js'), `export default { async fetch(request) {
      const url = new URL(request.url);
      if (url.pathname === '/ready') return new Response(null, { status: 204 });
      if (url.pathname === '/binary') return new Response(await request.arrayBuffer(), { status: 201 });
      if (url.pathname === '/json') return Response.json(await request.json());
      try { await fetch(url.searchParams.get('to')); return new Response('allowed'); }
      catch { return new Response('denied', { status: 403 }); }
    } };`);
    runtime = await openWorkerd(out, join(out, 'disk'));
    const bytes = Uint8Array.from({ length: 128 * 1024 }, (_, i) => i % 256);
    const binary = await runtime.dispatchFetch('http://proof/binary', { method: 'POST', body: bytes, signal: AbortSignal.timeout(requestWithin) });
    assert.equal(binary.status, 201);
    assert.equal(binary.ok, true);
    assert.deepEqual(new Uint8Array(await binary.arrayBuffer()), bytes);
    const value = { text: 'a complete reply', fence: '18446744073709551615' };
    const json = await runtime.dispatchFetch('http://proof/json', { method: 'POST', body: JSON.stringify(value), signal: AbortSignal.timeout(requestWithin) });
    assert.equal(json.status, 200);
    assert.deepEqual(await json.json(), value);
    const denied = await runtime.dispatchFetch('http://proof/outbound?to=' + encodeURIComponent(`http://127.0.0.1:${canary.address().port}`), { signal: AbortSignal.timeout(requestWithin) });
    assert.equal(denied.status, 403);
    assert.equal(denied.ok, false);
    assert.equal(await denied.text(), 'denied');
    assert.equal(attempts, 0);
  } finally {
    await runtime?.dispose();
    await new Promise(resolve => canary.close(resolve));
    await rm(out, { recursive: true, force: true });
  }
});
