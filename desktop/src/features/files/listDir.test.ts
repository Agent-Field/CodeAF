import test from 'node:test';
import assert from 'node:assert/strict';
import { EngineError } from '../chat/engine-client.ts';
import { listEngineFolder } from './listDir.ts';

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

type Seen = { url: string; method?: string; body?: BodyInit | null; cache?: RequestCache; accept: string | null };

async function withFetch<T>(stub: typeof fetch, run: () => Promise<T>): Promise<T> {
  const original = globalThis.fetch;
  globalThis.fetch = stub;
  try { return await run(); } finally { globalThis.fetch = original; }
}

function recording(answer: (url: string) => Response): { stub: typeof fetch; seen: Seen[] } {
  const seen: Seen[] = [];
  const stub: typeof fetch = async (url, init) => {
    seen.push({
      url: String(url),
      method: init?.method,
      body: init?.body,
      cache: init?.cache,
      accept: new Headers(init?.headers).get('Accept'),
    });
    return answer(String(url));
  };
  return { stub, seen };
}

const listing = {
  path: '/engine/project/src',
  entries: [
    { name: 'pkg', dir: true, size: 0, modTime: 1700000000, mime: 'inode/directory' },
    { name: 'a.go', dir: false, size: 12, modTime: 1700000000, mime: 'text/x-go' },
    { name: 'empty', dir: false, size: 0 },
  ],
  truncated: false,
};

test('listEngineFolder GETs /files/list and keeps the engine row, order and resolved path', async () => {
  const { stub, seen } = recording(() => json(200, listing));
  const got = await withFetch(stub, () => listEngineFolder('sess/1', 'src/pkg'));
  assert.equal(seen.length, 1);
  assert.equal(seen[0].url, '/api/engine/sessions/sess%2F1/files/list?path=src%2Fpkg');
  assert.equal(seen[0].method, undefined);
  assert.equal(seen[0].body, undefined);
  assert.equal(seen[0].cache, 'no-store');
  assert.equal(seen[0].accept, 'application/json');
  assert.equal(got.path, '/engine/project/src');
  assert.equal(got.truncated, false);
  assert.deepEqual(got.entries, [
    { name: 'pkg', dir: true, size: 0, modTime: 1700000000 },
    { name: 'a.go', dir: false, size: 12, modTime: 1700000000 },
    { name: 'empty', dir: false, size: 0 },
  ]);
  assert.equal('mime' in got.entries[0], false);
});

test('a blank path is sent blank, and a path is not rewritten before the engine sees it', async () => {
  const { stub, seen } = recording(() => json(200, { path: '/engine/project', entries: [], truncated: false }));
  await withFetch(stub, () => listEngineFolder('s', ''));
  await withFetch(stub, () => listEngineFolder('s', '../up'));
  await withFetch(stub, () => listEngineFolder('s', 'a&b=c'));
  assert.deepEqual(seen.map(call => call.url), [
    '/api/engine/sessions/s/files/list?path=',
    '/api/engine/sessions/s/files/list?path=..%2Fup',
    '/api/engine/sessions/s/files/list?path=a%26b%3Dc',
  ]);
});

test('an empty folder is an empty list, and a cut page stays cut, in the engine order', async () => {
  const empty = await withFetch(async () => json(200, { path: '/engine/empty', entries: [], truncated: false }), () => listEngineFolder('s', 'empty'));
  assert.deepEqual(empty, { path: '/engine/empty', entries: [], truncated: false });
  const cut = await withFetch(async () => json(200, {
    path: '/engine/big',
    entries: [{ name: 'z.txt', dir: false, size: 1 }, { name: 'a', dir: true, size: 0, modTime: 0 }],
    truncated: true,
  }), () => listEngineFolder('s', 'big'));
  assert.equal(cut.truncated, true);
  assert.deepEqual(cut.entries.map(row => row.name), ['z.txt', 'a']);
  assert.equal(cut.entries[1].modTime, undefined);
});

test('the engine\'s refusal sentence and status are kept', async () => {
  await withFetch(async () => json(409, { error: 'this engine cannot list folders' }), async () => {
    await assert.rejects(listEngineFolder('s', 'src'), (error: EngineError) => error instanceof EngineError && error.status === 409 && error.message === 'this engine cannot list folders' && !error.unreachable);
  });
  await withFetch(async () => json(403, { error: "engine: /etc is outside this conversation's workspace and its own folder" }), async () => {
    await assert.rejects(listEngineFolder('s', '/etc'), (error: EngineError) => error.status === 403 && /outside this conversation's workspace/.test(error.message));
  });
  await withFetch(async () => json(404, { error: 'engine: no such file: missing' }), async () => {
    await assert.rejects(listEngineFolder('s', 'missing'), (error: EngineError) => error.status === 404 && error.message === 'engine: no such file: missing');
  });
  await withFetch(async () => json(502, { error: 'engine: report.md is not a directory' }), async () => {
    await assert.rejects(listEngineFolder('s', 'report.md'), (error: EngineError) => error.status === 502 && /not a directory/.test(error.message) && !error.unreachable);
  });
});

test('nothing answering is unreachable, and a listing that is not the documented shape is refused', async () => {
  await withFetch(async () => { throw new TypeError('fetch failed'); }, async () => {
    await assert.rejects(listEngineFolder('s', 'src'), (error: EngineError) => error instanceof EngineError && error.unreachable && error.message === 'codeaf engine is not running');
  });
  await withFetch(async () => new Response('bad gateway', { status: 502 }), async () => {
    await assert.rejects(listEngineFolder('s', 'src'), (error: EngineError) => error.unreachable);
  });
  const bad = [
    null,
    { entries: [], truncated: false },
    { path: '/x', entries: [] },
    { path: 1, entries: [], truncated: false },
    { path: '/x', truncated: false },
    { path: '/x', entries: null, truncated: false },
    { path: '/x', entries: [], truncated: 'no' },
    { path: '/x', entries: [{ name: '', dir: false, size: 0 }], truncated: false },
    { path: '/x', entries: [{ name: 'a', dir: 'yes', size: 0 }], truncated: false },
    { path: '/x', entries: [{ name: 'a', dir: false, size: -1 }], truncated: false },
    { path: '/x', entries: [{ name: 'a', dir: false, size: 1.5 }], truncated: false },
    { path: '/x', entries: [{ name: 'a', dir: false, size: 0, modTime: -1 }], truncated: false },
  ];
  for (const body of bad) {
    await withFetch(async () => json(200, body), async () => {
      await assert.rejects(listEngineFolder('s', 'src'), /invalid folder listing/);
    });
  }
  await withFetch(async () => new Response('not-json', { status: 200, headers: { 'Content-Type': 'text/plain' } }), async () => {
    await assert.rejects(listEngineFolder('s', 'src'), /invalid folder listing/);
  });
  const kept = await withFetch(async () => json(200, { path: '/x', entries: [], truncated: false, extra: true }), () => listEngineFolder('s', 'src'));
  assert.deepEqual(kept, { path: '/x', entries: [], truncated: false });
});
