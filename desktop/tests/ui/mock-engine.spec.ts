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
