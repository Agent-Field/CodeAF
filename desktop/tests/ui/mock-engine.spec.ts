import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion, plainReply, streaming, toolsReply, withTasks } from './support/scenarios';

// Exercises the mock through fetch only; no UI markup is read.
type Reply = { status: number; body: any };

async function call(page: Page, path: string, method = 'GET', body?: unknown): Promise<Reply> {
  return page.evaluate(async ([p, m, b]) => {
    const response = await fetch(`/api/engine${p}`, {
      method: m as string,
      body: b === undefined ? undefined : JSON.stringify(b),
      headers: b === undefined ? undefined : { 'Content-Type': 'application/json' },
    });
    return { status: response.status, body: await response.json() };
  }, [path, method, body] as const);
}

async function readStream(page: Page, id: string, after: number): Promise<{ type: string; seq: number; event?: { text: string } }[]> {
  return page.evaluate(async ([i, a]) => {
    const response = await fetch(`/api/engine/sessions/${i}/events?after=${a}`, { headers: { Accept: 'text/event-stream' } });
    const text = await response.text();
    const type = response.headers.get('content-type');
    const records = text.split('\n').filter(l => l.startsWith('data:')).map(l => JSON.parse(l.slice(5)));
    return type?.includes('text/event-stream') ? records : [];
  }, [id, after] as const);
}

test.beforeEach(async ({ page }) => { await page.goto('/'); });

test('creates and reattaches a valid snapshot', async ({ page }) => {
  await installMockEngine(page, plainReply());
  const created = await call(page, '/sessions', 'POST', {});
  expect(created.body).toMatchObject({ model: 'deepseek/deepseek-v4.1-flash', persistent: true });
  expect(created.body.sessionFile).not.toBe('');
  const again = await call(page, '/sessions', 'POST', { sessionFile: created.body.sessionFile });
  expect(again.body.id).toBe(created.body.id);
  expect(created.body.entries.map((e: any) => e.Role)).toEqual(['user', 'assistant']);
  expect(created.body.entries[1].Text).toContain('```ts');
  expect(created.body.entries[1].Text).toContain('| Name | Value |');
});

test('turn appends the scripted reply and returns accepted', async ({ page }) => {
  const mock = await installMockEngine(page, { ...plainReply(), initial: { entries: [] } });
  const sent = await call(page, '/sessions/mock-1/turn', 'POST', { text: 'hi', mode: 'submit' });
  expect(sent.body).toEqual({ accepted: true });
  const read = await call(page, '/sessions/mock-1');
  expect(read.body.entries.map((e: any) => e.Role)).toEqual(['user', 'assistant']);
  expect(read.body.running).toBe(false);
  expect(mock.calls.some(c => c.path.endsWith('/turn') && c.body.text === 'hi')).toBe(true);
});

test('manual scenarios hold the reply until advance, and stop clears it', async ({ page }) => {
  const mock = await installMockEngine(page, { ...plainReply(), initial: { entries: [] }, manual: true });
  await call(page, '/sessions/mock-1/turn', 'POST', { text: 'hi', mode: 'submit' });
  expect((await call(page, '/sessions/mock-1')).body).toMatchObject({ running: true });
  mock.advance();
  const done = (await call(page, '/sessions/mock-1')).body;
  expect(done.running).toBe(false);
  expect(done.entries).toHaveLength(2);
  await call(page, '/sessions/mock-1/turn', 'POST', { text: 'again', mode: 'submit' });
  await call(page, '/sessions/mock-1/stop', 'POST');
  expect((await call(page, '/sessions/mock-1')).body.running).toBe(false);
});

test('tool calls pair by CallID and the long output reads in full', async ({ page }) => {
  await installMockEngine(page, toolsReply());
  const { entries } = (await call(page, '/sessions/mock-1')).body;
  const tools = entries.filter((e: any) => e.Role === 'tool');
  expect(tools.map((e: any) => e.CallID)).toEqual(['call-1', 'call-2', 'call-3']);
  const full = await call(page, '/sessions/mock-1/tools/call-3');
  expect(full.body.full).toBe(true);
  expect(full.body.output.length).toBeGreaterThan(tools[2].Output.length);
  expect((await call(page, '/sessions/mock-1/tools/nope')).status).toBe(404);
});

test('tasks rows and the task page match the wire shape', async ({ page }) => {
  await installMockEngine(page, withTasks());
  const snapshot = (await call(page, '/sessions/mock-1')).body;
  expect(snapshot.entries.find((e: any) => e.Role === 'aside').TaskIDs).toEqual(['2']);
  expect(snapshot.tasks.map((t: any) => [t.ID, t.Parent ?? '', t.Status])).toEqual([
    ['2', '', 'running'], ['2.1', '2', 'done'], ['2.2', '2', 'running'], ['2.3', '2', 'failed'],
  ]);
  expect(snapshot.tasks[2].Live).toMatchObject({ Step: 8 });
  const taskPage = (await call(page, '/sessions/mock-1/tasks/2')).body;
  expect(taskPage).toMatchObject({ Row: { ID: '2' } });
  expect(taskPage.Description && taskPage.Result && taskPage.Steps.length && taskPage.Checks.length).toBeTruthy();
  expect((await call(page, '/sessions/mock-1/tasks/9')).status).toBe(404);
});

test('a pending question is answerable', async ({ page }) => {
  const mock = await installMockEngine(page, pendingQuestion());
  const snapshot = (await call(page, '/sessions/mock-1')).body;
  expect(snapshot.needsPerson).toBe(true);
  expect(snapshot.questions[0].options).toHaveLength(2);
  const answer = { kind: 'choice', id: 7, key: 'sqlite' };
  expect((await call(page, '/sessions/mock-1/answer', 'POST', answer)).body).toEqual({ accepted: true });
  expect((await call(page, '/sessions/mock-1')).body).toMatchObject({ needsPerson: false, questions: [] });
  expect(mock.calls.at(-2)?.body).toEqual(answer);
});

test('streaming scenario emits text events then a final snapshot', async ({ page }) => {
  await installMockEngine(page, streaming());
  await call(page, '/sessions/mock-1/turn', 'POST', { text: 'hi', mode: 'submit' });
  const records = await readStream(page, 'mock-1', 0);
  const texts = records.filter(r => r.type === 'event').map(r => r.event!.text);
  expect(texts.join('')).toBe('Hello there. This reply arrived in pieces.');
  expect(records.at(-1)?.type).toBe('snapshot');
  const seqs = records.map(r => r.seq);
  expect(seqs).toEqual([...seqs].sort((a, b) => a - b));
});

test('forced failures and unknown sessions return error bodies', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), fail: { turn: 409 } });
  const rejected = await call(page, '/sessions/mock-1/turn', 'POST', { text: 'x', mode: 'submit' });
  expect(rejected.status).toBe(409);
  expect(typeof rejected.body.error).toBe('string');
  expect((await call(page, '/sessions/other')).status).toBe(404);
});

const big = (n: number) => 'é'.repeat(n);
const toolEntry = (n: number, Output: string) => ({ Role: 'tool' as const, Text: '', Tool: 'bash', CallID: `c${n}`, Output });

test('since= answers the header plus the entries tail, eliding over-cap outputs', async ({ page }) => {
  const entries = [{ Role: 'user' as const, Text: 'a' }, toolEntry(1, 'small'), toolEntry(2, big(9000)), { Role: 'assistant' as const, Text: 'done' }];
  await installMockEngine(page, { initial: { entries, title: 'T' } });
  const tail = (await call(page, '/sessions/mock-1?since=2')).body;
  expect(tail).toMatchObject({ header: { title: 'T', entryCount: 4 }, from: 2 });
  expect(tail.reset).toBeUndefined();
  expect(tail.entries.map((e: any) => e.Role)).toEqual(['tool', 'assistant']);
  expect(tail.entries[0]).toMatchObject({ Output: '', OutputOmitted: true, OutputBytes: 18000 });
  const full = (await call(page, '/sessions/mock-1')).body;
  expect(full.entries).toHaveLength(4);
  expect(full.entries[1].Output).toBe('small');
  // Reattach is the body a dropped stream asks for, so it elides the same way a read does.
  const reattached = (await call(page, '/sessions', 'POST', { sessionFile: 'mock-session-1.jsonl' })).body;
  expect(reattached.entries[2]).toMatchObject({ Output: '', OutputOmitted: true, OutputBytes: 18000 });
  expect(reattached.entries[1].Output).toBe('small');
  const rewritten = (await call(page, '/sessions/mock-1?since=9')).body;
  expect(rewritten).toMatchObject({ reset: true, from: 0 });
  expect(rewritten.entries).toHaveLength(4);
});

test('queue-send steers a queued message into the running turn and refuses a sent one', async ({ page }) => {
  const mock = await installMockEngine(page, { ...plainReply(), initial: { entries: [] }, manual: true });
  await call(page, '/sessions/mock-1/turn', 'POST', { text: 'first', mode: 'submit' });
  await call(page, '/sessions/mock-1/turn', 'POST', { text: 'later', mode: 'queue' });
  const queued = (await call(page, '/sessions/mock-1')).body.queue;
  expect(queued).toHaveLength(1);
  expect((await call(page, '/sessions/mock-1/queue-send', 'POST', { id: queued[0].id })).body).toEqual({ accepted: true });
  const after = (await call(page, '/sessions/mock-1')).body;
  expect(after.queue).toEqual([]);
  expect(after.entries.at(-1)).toMatchObject({ Role: 'user', Text: 'later', Steer: { Consumed: false } });
  const again = await call(page, '/sessions/mock-1/queue-send', 'POST', { id: queued[0].id });
  expect(again).toMatchObject({ status: 409, body: { error: 'that message has already been sent' } });
  mock.advance();
});

test('offline fails every route until restored, and notice events stream', async ({ page }) => {
  const mock = await installMockEngine(page, plainReply());
  mock.setOffline(true, '503');
  expect((await call(page, '/sessions/mock-1')).status).toBe(503);
  mock.setOffline(true);
  await expect(call(page, '/sessions/mock-1')).rejects.toThrow();
  mock.setOffline(false);
  expect((await call(page, '/sessions/mock-1')).status).toBe(200);
  const after = mock.snapshot().seq;
  mock.retrying(4, 'busy');
  mock.compacting();
  mock.compacted();
  const events = (await readStream(page, 'mock-1', after)).map(r => (r as any).event);
  expect(events.map((e: any) => e.kind)).toEqual(['retrying', 'compacting', 'compacted']);
  expect(events[0]).toMatchObject({ text: 'busy', raw: { Retry: { DelaySeconds: 4 } } });
});

test('places routes, Using and world-stream places records come from the mounted places engine', async ({ page }) => {
  await installMockEngine(page, { initial: { entries: [] }, places: '200-places', world: { rows: [], items: [] } });
  const graph = (await call(page, '/places')).body;
  expect(graph.places).toHaveLength(200);
  const depth = (id: string, seen = new Set<string>()): number => {
    const place = graph.places.find((row: { id: string }) => row.id === id);
    if (!place || seen.has(id) || place.parents.length === 0) return 1;
    seen.add(id);
    return 1 + Math.max(...place.parents.map((parent: string) => depth(parent, seen)));
  };
  expect(Math.max(...graph.places.map((place: { id: string }) => depth(place.id)))).toBe(3);
  const reports = graph.places.find((place: { name: string }) => place.name === 'Reports');
  expect((await call(page, `/places/${reports.id}`)).body.children).toHaveLength(47);
  const using = await call(page, '/sessions/config-stack/using');
  expect(using.status).toBe(200);
  expect(using.body.bundle.places.map((place: { id: string }) => place.id)).toEqual(['config-parser', 'software', 'codeaf']);
  expect((await call(page, '/places/policy')).body.settings.map((row: { key: string }) => row.key)).toContain('clusterOffers');
  const read = async (after: number) => page.evaluate(async (cursor) => {
    const response = await fetch(`/api/engine/events?after=${cursor}`, { headers: { Accept: 'text/event-stream' } });
    const text = await response.text();
    return text.split('\n').filter(line => line.startsWith('data:')).map(line => JSON.parse(line.slice(5)));
  }, after);
  const first = await read(0);
  expect(first.map((record: { type: string }) => record.type)).toEqual(['reset', 'places']);
  expect(first[0].payload.places.nodes).toHaveLength(200);
  expect(first[1].payload.nodes).toHaveLength(200);
  expect(first[1].payload.generation).toBe(graph.generation);
  expect(first[1].seq).toBe(first[0].seq + 1);
  const created = await call(page, '/places', 'POST', { name: 'Scale child' });
  expect(created.status).toBe(200);
  const next = await read(first[1].seq);
  expect(next.map((record: { type: string }) => record.type)).toEqual(['reset', 'places']);
  expect(next[1].payload.nodes).toHaveLength(201);
});
