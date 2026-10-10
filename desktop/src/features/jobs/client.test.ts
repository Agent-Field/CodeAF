import test from 'node:test';
import assert from 'node:assert/strict';
import { jobsForSession, listJobs, stopJob, jobLog } from './client.ts';
import { createWorldClient } from '../world/worldClient.ts';

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
const tick = () => new Promise<void>(resolve => setImmediate(resolve));

async function withFetch(run: () => Promise<void>, stub: typeof fetch) {
  const previous = globalThis.fetch;
  globalThis.fetch = stub;
  try { await run(); } finally { globalThis.fetch = previous; }
}

test('parses a running and an exited job', async () => {
  await withFetch(async () => {
    assert.deepEqual(await listJobs('attachment'), [
      { id: 1, state: 'running' }, { id: 2, name: 'build', state: 'done', exitCode: 0 },
    ]);
  }, async () => json([{ id: 1, state: 'running', ticks: 0, future: true }, { id: 2, name: 'build', state: 'done', exitCode: 0 }]));
});

test('stop posts to the job route', async () => {
  await withFetch(async () => { assert.deepEqual(await stopJob('attachment/1', 2), { accepted: true }); }, async (url, init) => {
    assert.equal(String(url), '/api/engine/sessions/attachment%2F1/jobs/2/stop');
    assert.equal(init?.method, 'POST');
    assert.equal(init?.body, '{}');
    return json({ accepted: true, future: true });
  });
});

test('log tail and largest retained log preserve truncation', async () => {
  const urls: string[] = [];
  await withFetch(async () => {
    assert.deepEqual(await jobLog('attachment', 2, 32), { text: 'tail', truncated: true });
    assert.deepEqual(await jobLog('attachment', 2), { text: 'tail', truncated: true });
  }, async url => { urls.push(String(url)); return json({ text: 'tail', truncated: true }); });
  assert.deepEqual(urls, ['/api/engine/sessions/attachment/jobs/2/log?tail=32', '/api/engine/sessions/attachment/jobs/2/log']);
});

test('world jobs records update the hook without a session SSE', async () => {
  const paths: string[] = [];
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const world = createWorldClient({ transport: async (path, init) => {
    paths.push(path);
    return new Response(new ReadableStream<Uint8Array>({ start(c) {
      controller = c;
      init.signal?.addEventListener('abort', () => c.close(), { once: true });
    } }));
  } });
  const sessionFile = '/projects/chat-a/session.jsonl';
  const unsubscribe = world.subscribe(() => undefined);
  const second = world.subscribe(() => undefined);
  const send = async (seq: number, type: string, payload: unknown, epoch = 'a') => {
    controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ epoch, seq, type, payload })}\n\n`));
    await tick();
  };
  try {
    assert.deepEqual(jobsForSession(world, sessionFile), []);
    await send(1, 'reset', { rows: [], items: [], jobs: [{ chatId: 'chat-a', jobs: [{ id: 1, state: 'running' }] }] });
    const running = jobsForSession(world, sessionFile);
    assert.deepEqual(running, [{ id: 1, state: 'running' }]);
    await send(2, 'jobs', { chatId: 'other', jobs: [{ id: 2 }] });
    assert.equal(jobsForSession(world, sessionFile), running);
    await send(3, 'jobs', { chatId: 'chat-a', jobs: [{ id: 1, state: 'done', exitCode: 0, future: true }] });
    assert.deepEqual(jobsForSession(world, sessionFile), [{ id: 1, state: 'done', exitCode: 0 }]);
    assert.deepEqual(jobsForSession(world, undefined), []);
    assert.deepEqual(jobsForSession(world, '/projects/missing/session.jsonl'), []);
    await send(0, 'reset', { rows: [], items: [] }, 'restarted');
    assert.deepEqual(jobsForSession(world, sessionFile), []);
    assert.deepEqual(paths, ['/events?after=0']);
  } finally { second(); unsubscribe(); }
});
