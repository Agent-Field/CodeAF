import test from 'node:test';
import assert from 'node:assert/strict';
import { EngineError } from '../chat/engine-client.ts';
import { jobLog, listJobs, stopJob } from './jobsClient.ts';

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

type Seen = { url: string; method?: string; body?: BodyInit | null; cache?: RequestCache; accept: string | null };

async function withFetch<T>(stub: typeof fetch, run: () => Promise<T>): Promise<T> {
  const original = globalThis.fetch;
  globalThis.fetch = stub;
  try { return await run(); } finally { globalThis.fetch = original; }
}

function recording(answer: (url: string, init?: RequestInit) => Response): { stub: typeof fetch; seen: Seen[] } {
  const seen: Seen[] = [];
  const stub: typeof fetch = async (url, init) => {
    seen.push({
      url: String(url),
      method: init?.method,
      body: init?.body,
      cache: init?.cache,
      accept: new Headers(init?.headers).get('Accept'),
    });
    return answer(String(url), init);
  };
  return { stub, seen };
}

// The shelf GET /jobs answers, in the engine's order, including a real exit of 0
// and a stopped job that has no exit code. Matches TestJobsListRelaysTheEngine.
const shelf = [
  { id: 2, name: 'build', command: 'make build', kind: 'command', state: 'done', startedAt: '2026-10-09T15:00:00Z', elapsedMs: 90000, exitCode: 0, logPath: '/places/chat-9/jobs/2.log', extra: true },
  { id: 9, name: 'nightly bench', command: 'make bench', detail: 'every 10s', kind: 'watch', state: 'running', startedAt: '2026-10-09T15:00:00Z', elapsedMs: 4000, ticks: 3, logPath: '/places/chat-9/jobs/9.log' },
  { id: 1, command: 'sleep 1', kind: 'command', state: 'stopped', startedAt: '2026-10-09T15:00:00Z', logPath: '/places/chat-9/jobs/1.log' },
];

test('list, stop and log use the bridge paths and do not name a log file', async () => {
  const { stub, seen } = recording(url => {
    if (url.endsWith('/jobs')) return json(200, shelf);
    if (url.endsWith('/stop')) return json(200, { accepted: true, line: 'stopped job 3 (nightly bench) — its log is kept' });
    return json(200, { text: 'ghij', truncated: true });
  });
  await withFetch(stub, async () => {
    await listJobs('sess/1');
    await stopJob('sess/1', 3);
    await jobLog('sess/1', 3, 4);
    await jobLog('chat', '9');
  });
  assert.deepEqual(seen.map(call => [call.method, call.url]), [
    [undefined, '/api/engine/sessions/sess%2F1/jobs'],
    ['POST', '/api/engine/sessions/sess%2F1/jobs/3/stop'],
    [undefined, '/api/engine/sessions/sess%2F1/jobs/3/log?tail=4'],
    [undefined, '/api/engine/sessions/chat/jobs/9/log'],
  ]);
  assert.equal(seen[1].body, '{}');
  assert.equal(seen[0].cache, 'no-store');
  assert.equal(seen[0].accept, 'application/json');
  assert.equal(seen.every(call => !call.url.includes('path=')), true);
});

test('a slash in an id stays one path segment', async () => {
  const { stub, seen } = recording(() => json(200, { text: '', truncated: false }));
  await withFetch(stub, () => jobLog('s', '3/log', 0));
  assert.equal(seen[0].url, '/api/engine/sessions/s/jobs/3%2Flog/log?tail=0');
});

test('the list keeps the engine order, a real exit of 0, and drops what the bridge omits', async () => {
  const got = await withFetch(async () => json(200, shelf), () => listJobs('chat-9'));
  assert.deepEqual(got.map(job => job.id), [2, 9, 1]);
  assert.equal(got[0].exitCode, 0);
  assert.equal(got[0].name, 'build');
  assert.equal(got[0].state, 'done');
  assert.equal('extra' in got[0], false);
  assert.equal(got[1].detail, 'every 10s');
  assert.equal(got[1].kind, 'watch');
  assert.equal(got[1].ticks, 3);
  assert.equal(got[1].exitCode, undefined);
  assert.equal(got[2].name, undefined);
  assert.equal(got[2].exitCode, undefined);
  assert.equal(got[2].ticks, undefined);
  const empty = await withFetch(async () => json(200, []), () => listJobs('chat-9'));
  assert.deepEqual(empty, []);
});

test('a zero duration or tick count is absent, and an empty log is still a log', async () => {
  const got = await withFetch(async () => json(200, [{ id: 4, elapsedMs: 0, ticks: 0, name: '', state: 'running' }]), () => listJobs('s'));
  assert.deepEqual(got, [{ id: 4, state: 'running' }]);
  const log = await withFetch(async () => json(200, { text: '', truncated: false }), () => jobLog('s', 4));
  assert.deepEqual(log, { text: '', truncated: false });
  const quiet = await withFetch(async () => json(200, { accepted: true }), () => stopJob('s', 4));
  assert.deepEqual(quiet, { accepted: true });
  const said = await withFetch(async () => json(200, { accepted: true, line: 'stopped job 3 (nightly bench) — its log is kept' }), () => stopJob('s', '3'));
  assert.equal(said.line, 'stopped job 3 (nightly bench) — its log is kept');
});

test('the engine\'s refusal sentence and status are kept', async () => {
  const refuse = async (status: number, error: string, run: () => Promise<unknown>) => {
    await withFetch(async () => json(status, { error }), async () => {
      await assert.rejects(run, (failure: EngineError) => failure instanceof EngineError && failure.status === status && failure.message === error && !failure.unreachable);
    });
  };
  await refuse(409, 'this engine cannot list its jobs', () => listJobs('s'));
  await refuse(409, 'no such method', () => listJobs('s'));
  await refuse(409, 'this engine cannot stop a job', () => stopJob('s', 3));
  await refuse(409, 'there is no job 8 in this session', () => stopJob('s', 8));
  await refuse(400, 'tail must be a number of bytes', () => jobLog('s', 3, Number.NaN));
  await refuse(403, 'that log is outside this conversation', () => jobLog('s', 4));
  await refuse(404, 'this job has no log', () => jobLog('s', 7));
  await refuse(404, 'there is no job 99 in this conversation', () => jobLog('s', 99));
  await refuse(409, 'this engine cannot hand over files', () => jobLog('s', 3));
});

test('nothing answering is unreachable, and a body that is not the documented shape is refused', async () => {
  await withFetch(async () => { throw new TypeError('fetch failed'); }, async () => {
    await assert.rejects(listJobs('s'), (failure: EngineError) => failure instanceof EngineError && failure.unreachable && failure.message === 'codeaf engine is not running');
  });
  await withFetch(async () => new Response('bad gateway', { status: 502 }), async () => {
    await assert.rejects(stopJob('s', 3), (failure: EngineError) => failure.unreachable);
  });
  await withFetch(async () => json(502, { error: 'the file is gone' }), async () => {
    await assert.rejects(jobLog('s', 3), (failure: EngineError) => failure.status === 502 && failure.message === 'the file is gone' && !failure.unreachable);
  });
  const badLists = [
    null,
    {},
    { jobs: [] },
    [{ name: 'build' }],
    [{ id: 0, state: 'running' }],
    [{ id: -1 }],
    [{ id: 1.5 }],
    [{ id: 1, kind: 'nope' }],
    [{ id: 1, state: 'queued' }],
    [{ id: 1, elapsedMs: -1 }],
    [{ id: 1, exitCode: 1.5 }],
    [{ id: 1, exitCode: null }],
  ];
  for (const body of badLists) {
    await withFetch(async () => json(200, body), async () => {
      await assert.rejects(listJobs('s'), /invalid job/);
    });
  }
  await withFetch(async () => json(200, { accepted: false }), async () => {
    await assert.rejects(stopJob('s', 3), /invalid stop/);
  });
  await withFetch(async () => json(200, { line: 'stopped' }), async () => {
    await assert.rejects(stopJob('s', 3), /invalid stop/);
  });
  const badLogs = [null, { text: 'ok' }, { truncated: false }, { text: 1, truncated: false }, { text: 'ok', truncated: 'no' }];
  for (const body of badLogs) {
    await withFetch(async () => json(200, body), async () => {
      await assert.rejects(jobLog('s', 3), /invalid job log/);
    });
  }
  await withFetch(async () => new Response('not-json', { status: 200, headers: { 'Content-Type': 'text/plain' } }), async () => {
    await assert.rejects(listJobs('s'), /invalid job list/);
  });
});
