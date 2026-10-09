import { createServer, type ServerResponse } from 'node:http';
import { test as base, expect, type Page } from '@playwright/test';
import type { EngineSnapshot, EngineEntry } from '../../src/features/chat/engine-client';
const MODEL = 'deepseek/deepseek-v4.1-flash';
type MockEngine = {
 url: string; model: string; needsPerson: boolean; failTurn: boolean;
 calls: { path: string; method: string; body: Record<string, unknown> }[];
 sessions: Map<string, EngineSnapshot>; readers: Map<string, Set<ServerResponse>>;
 complete: (id: string, text: string) => void;
};
const test = base.extend<{ engine: MockEngine }>({
 engine: async ({ page }, use) => {
  const sessions = new Map<string, EngineSnapshot>();
  const readers = new Map<string, Set<ServerResponse>>();
  const records = new Map<string, { seq: number; type: string; snapshot: EngineSnapshot }[]>();
  const publish = (id: string) => {
   const snapshot = sessions.get(id)!; snapshot.seq++; snapshot.updatedAt = new Date().toISOString();
   const record = { seq: snapshot.seq, type: 'snapshot', snapshot: structuredClone(snapshot) };
   records.set(id, [...(records.get(id) ?? []), record]);
   for (const reader of readers.get(id) ?? []) reader.write(`id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n`);
  };
  const engine: MockEngine = { url: '', model: MODEL, needsPerson: false, failTurn: false, calls: [], sessions, readers, complete(id, text) {
   const snapshot = sessions.get(id)!;
   snapshot.entries.push({ Role: 'assistant', Text: text, Answer: true }); snapshot.running = false; publish(id);
  } };
  const server = createServer(async (request, response) => {
   response.setHeader('Access-Control-Allow-Origin', '*');
   response.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
   response.setHeader('Access-Control-Allow-Headers', 'Content-Type, Accept, Cache-Control');
   if (request.method === 'OPTIONS') { response.writeHead(204).end(); return; }
   const chunks: Buffer[] = []; for await (const chunk of request) chunks.push(Buffer.from(chunk));
   const body = chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown> : {};
   const url = new URL(request.url!, engine.url);
   engine.calls.push({ path: url.pathname, method: request.method!, body });
   const json = (value: unknown, status = 200) => { response.writeHead(status, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(value)); };
   if (url.pathname === '/api/engine/sessions' && request.method === 'POST') {
    let snapshot = [...sessions.values()].find(s => s.sessionFile === body.sessionFile);
    if (!snapshot) {
     const id = `conversation-${sessions.size + 1}`;
     snapshot = { id, sessionFile: `saved-${id}.jsonl`, workspace: '/test-workspace', model: engine.model, persistent: true, running: false, needsPerson: engine.needsPerson, entries: [], tasks: [], usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 }, title: '', seq: 0 };
     sessions.set(id, snapshot);
    }
    json(snapshot); return;
   }
   const [, , , , id, action] = url.pathname.split('/');
   const snapshot = sessions.get(id);
   if (!snapshot) { json({ error: 'reattach this conversation' }, 404); return; }
   if (!action) { json(snapshot); return; }
   if (action === 'events') {
    response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
    response.write(': connected\n\n');
    const after = Number(url.searchParams.get('after') ?? 0);
    for (const record of records.get(id) ?? []) if (record.seq > after) response.write(`id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n`);
    const set = readers.get(id) ?? new Set(); set.add(response); readers.set(id, set);
    response.on('close', () => set.delete(response)); return;
   }
   if (action === 'turn') {
    if (engine.failTurn) { json({ error: 'Instruction rejected by the engine' }, 409); return; }
    snapshot.entries.push({ Role: 'user', Text: String(body.text) } as EngineEntry); snapshot.running = true; publish(id); json({ accepted: true }); return;
   }
   if (action === 'tools') {json({output:'Full canonical result\nSecond result line',full:true});return;}
   if (action === 'stop') { snapshot.running = false; publish(id); json({ accepted: true }); return; }
   json({ error: 'unknown action' }, 404);
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('Mock engine did not bind');
  engine.url = `http://127.0.0.1:${address.port}`;
  await page.addInitScript(url => {
   const original = window.fetch;
   window.fetch = (input, init) => typeof input === 'string' && input.startsWith('/api/engine') ? original(`${url}${input}`, init) : original(input, init);
  }, engine.url);
  try { await use(engine); } finally { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
 },
});

async function connect(page: Page) {
 await page.getByRole('button', { name: 'Connect engine', exact: true }).click();
 await expect(page.getByText('Engine connected', { exact: true })).toBeVisible();
}

test('engine connects explicitly and submits real history through the fixed model', async ({ page, engine }) => {
 await page.goto('/');
 await expect(page.getByRole('button', { name: 'Connect engine', exact: true })).toBeVisible();
 expect(engine.calls).toHaveLength(0);
 await connect(page);
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Inspect the canonical engine');
 await expect(page.getByText('DeepSeek v4.1 Flash', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Choose model preset', exact: true })).not.toBeVisible();
 await draft.press('Enter'); await expect(draft).toHaveValue('');
 const turn = engine.calls.find(call => call.path.endsWith('/turn'))!;
 expect(turn.body).toEqual({ text: 'Inspect the canonical engine', mode: 'submit' });
 expect([...engine.sessions.values()][0].model).toBe(MODEL);
 await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
 engine.complete('conversation-1', 'Verified canonical reply from the engine.');
 await expect(page.locator('.work-output')).toContainText('Verified canonical reply from the engine.');
 await expect(page.getByRole('button', { name: 'Stop', exact: true })).not.toBeVisible();
 await expect(page.locator('.workspace-tab').getByRole('img', { name: 'Completed', exact: true })).toBeVisible();
 await page.reload();
 await expect(page.locator('.work-output')).toContainText('Verified canonical reply from the engine.');
 expect(engine.calls.filter(call => call.path === '/api/engine/sessions').at(-1)?.body).toEqual({ sessionFile: 'saved-conversation-1.jsonl' });
});

test('inactive tabs receive completion; closing detaches without Stop and reopening reattaches', async ({ page, engine }) => {
 await page.goto('/'); await connect(page);
 const draft = page.getByRole('textbox', { name: /Draft for/ }); await draft.fill('Run background work'); await draft.press('Enter'); await expect(draft).toHaveValue('');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect.poll(() => engine.readers.get('conversation-1')?.size ?? 0).toBeGreaterThan(0);
 engine.complete('conversation-1', 'Background work completed.');
 const first = page.getByRole('tab', { name: 'New conversation', exact: true });
 await expect(page.locator('.workspace-tab').first().getByRole('img', { name: 'Completed', exact: true })).toBeVisible();
 await first.click();
 await expect(page.getByText('Engine connected', { exact: true })).toBeVisible();
 await draft.fill('Continue while detached'); await draft.press('Enter'); await expect(draft).toHaveValue('');
 await first.click({ button: 'right' }); await page.getByRole('menuitem', { name: /^Close tab/ }).click();
 await expect.poll(() => engine.readers.get('conversation-1')?.size ?? 0).toBe(0);
 expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toHaveLength(0);
 expect(engine.sessions.get('conversation-1')?.running).toBe(true);
 engine.complete('conversation-1', 'Work continued after its tab closed.');
 await page.getByRole('button', { name: 'Tab actions', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Reopen closed tab', exact: true }).click();
 await expect(page.locator('.work-output').last()).toContainText('Work continued after its tab closed.');
 await expect.poll(() => engine.readers.get('conversation-1')?.size ?? 0).toBeGreaterThan(0);
 expect(engine.sessions.size).toBe(1);
});

test('failed submissions preserve the unsent draft and show the engine error', async ({ page, engine }) => {
 await page.goto('/'); await connect(page); engine.failTurn = true;
 const draft = page.getByRole('textbox', { name: /Draft for/ }); await draft.fill('Keep this instruction'); await draft.press('Enter');
 await expect(page.getByRole('status').filter({ hasText: 'Instruction rejected by the engine' })).toBeVisible();
 await expect(draft).toHaveValue('Keep this instruction');
 expect(engine.calls.filter(call => call.path.endsWith('/turn'))).toHaveLength(1);
 expect([...engine.sessions.values()][0].entries).toHaveLength(0);
});

test('wrong-model sessions are rejected before any instruction can execute', async ({ page, engine }) => {
 engine.model = 'older-flash'; await page.goto('/');
 await page.getByRole('button', { name: 'Connect engine', exact: true }).click();
 await expect(page.getByRole('status').filter({ hasText: 'required DeepSeek v4.1 Flash' })).toBeVisible();
 await expect(page.getByText('Engine connected', { exact: true })).not.toBeVisible();
 expect(engine.calls.filter(call => call.path.endsWith('/turn') || call.path.endsWith('/events'))).toHaveLength(0);
});

test('pending canonical decisions block sending and preserve words without fake answers', async ({ page, engine }) => {
 engine.needsPerson = true; await page.goto('/'); await connect(page);
 await expect(page.getByRole('status').filter({ hasText: 'pending approval or question' })).toBeVisible();
 const draft = page.getByRole('textbox', { name: /Draft for/ }); await draft.fill('Do not answer for me'); await draft.press('Enter');
 await expect(page.getByRole('status').filter({ hasText: 'TUI first' })).toBeVisible();
 await expect(draft).toHaveValue('Do not answer for me');
 expect(engine.calls.filter(call => call.path.endsWith('/turn'))).toHaveLength(0);
 expect([...engine.sessions.values()][0].entries).toHaveLength(0);
});


test('running instructions map to Steer and Queue while Stop remains explicit', async ({ page, engine }) => {
 await page.goto('/'); await connect(page);
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Start work'); await draft.press('Enter'); await expect(draft).toHaveValue('');
 await draft.fill('Adjust the current work');
 await page.getByRole('button', { name: 'Steer', exact: true }).click(); await expect(draft).toHaveValue('');
 await draft.fill('Run this after the turn');
 await page.getByRole('button', { name: 'Submission actions', exact: true }).click();
 await page.getByRole('menuitem', { name: /^Queue after this turn/ }).click(); await expect(draft).toHaveValue('');
 expect(engine.calls.filter(call => call.path.endsWith('/turn')).map(call => call.body)).toEqual([
  { text: 'Start work', mode: 'submit' },
  { text: 'Adjust the current work', mode: 'steer' },
  { text: 'Run this after the turn', mode: 'queue' },
 ]);
 await draft.fill('Keep this unsent');
 await page.getByRole('button', { name: 'Stop', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Stop', exact: true })).not.toBeVisible();
 await expect(draft).toHaveValue('Keep this unsent');
 expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toHaveLength(1);
 expect(engine.sessions.get('conversation-1')?.running).toBe(false);
});

test('canonical replies render Markdown with ordered expandable tools and AI titles', async ({page,engine})=>{
 await page.goto('/');await connect(page);
 const draft=page.getByRole('textbox',{name:/Draft for/});await draft.fill('Research the project');await draft.press('Enter');await expect(draft).toHaveValue('');
 const snapshot=engine.sessions.get('conversation-1')!;
 snapshot.entries.push(
  {Role:'assistant',Text:'I will **inspect** the project.'},
  {Role:'tool',Text:'',Tool:'web_search',CallID:'call-search',Hint:'Find official documentation',Args:JSON.stringify({query:'official project docs'}),Output:'Recorded compact summary',Answered:true},
  {Role:'assistant',Text:'## Findings\n\n- **Documented** result\n\n| Name | State |\n| --- | --- |\n| Engine | Ready |\n\n```ts\nconst ready = true;\n```'}
 );snapshot.entries.push({Role:'tool',Text:'',Tool:'web_fetch',CallID:'call-fetch',Args:'{"url":"https://example.com/docs"}',Output:'Fetched source',Answered:true});snapshot.title='Project research';
 engine.complete('conversation-1','Read the [source](https://example.com/docs).');
 await expect(page.getByRole('tab',{name:'Project research',exact:true})).toBeVisible();
 await expect(page.getByRole('heading',{name:'Findings',exact:true})).toBeVisible();
 await expect(page.locator('.markdown strong').first()).toHaveText('inspect');
 await expect(page.getByRole('region',{name:'Response table'})).toBeVisible();
 await expect(page.getByRole('link',{name:'source',exact:true})).toHaveAttribute('href','https://example.com/docs');
 const trace=page.getByRole('button',{name:'1 tool call',exact:true}).first();await expect(trace).toBeVisible();await trace.click();
 await expect(page.locator('.tool-call')).toHaveCount(1);
 await expect(page.locator('.tool-call-preview')).toHaveText('official project docs');
 await expect(page.locator('.tool-call-result')).not.toBeVisible();
 await page.locator('.tool-call > summary').click();
 await expect(page.locator('.tool-call-result')).toContainText('Full canonical result');
 expect(engine.calls.filter(call=>call.path.endsWith('/tools/call-search'))).toHaveLength(1);
 const order=await page.locator('.work-output > *').evaluateAll(nodes=>nodes.map(node=>node.className));
 expect(order.slice(0,3)).toEqual(['markdown ','tool-activity','markdown ']);
 await expect(page.getByText(/Completed · sample/)).not.toBeVisible();
 await page.reload();await expect(page.getByRole('tab',{name:'Project research',exact:true})).toBeVisible();await expect(page.getByRole('heading',{name:'Findings'})).toBeVisible();
});
