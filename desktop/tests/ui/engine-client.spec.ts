import { test, expect } from '@playwright/test';
const model = 'deepseek/deepseek-v4.1-flash';
const snapshot = { id: 'session-1', sessionFile: 'saved-session.jsonl', workspace: '/workspace', model, persistent: true, running: false, needsPerson: false, entries: [], tasks: [], usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 }, title: 'Real conversation', seq: 4 };

test('stream handles chunked CRLF, multiline data and replay then aborts without stopping work', async ({ page }) => {
 await page.goto('/');
 const result = await page.evaluate(async (base) => {
  const path = '/src/features/chat/engine-client.ts'; const client = await import(path);
  const calls: string[] = []; const events: string[] = []; const snapshots: number[] = [];
  const abort = new AbortController();
  const event = { seq: 5, type: 'event', event: { kind: 'text_delta', text: 'An actual reply', tool: '', hint: '', raw: {} } };
  const payload = `: heartbeat\r\n\r\nid: 4\r\ndata: ${JSON.stringify({ ...event, seq: 4 })}\r\n\r\nid: 5\r\ndata: ${JSON.stringify(event)}\r\n\r\ndata: {"seq":6,\r\ndata: "type":"snapshot",\r\ndata: "snapshot":${JSON.stringify({ ...base, seq: 6 })}}\r\n\r\n`;
  const originalFetch = window.fetch;
  window.fetch = async (input) => {
   calls.push(String(input));
   return new Response(new ReadableStream({ start(controller) {
    const bytes = new TextEncoder().encode(payload);
    for (let index = 0; index < bytes.length; index += 7) controller.enqueue(bytes.slice(index, index + 7));
   } }), { headers: { 'Content-Type': 'text/event-stream' } });
  };
  try {
   await client.watchEngine(base, (next: { seq: number }) => { snapshots.push(next.seq); abort.abort(); }, (event: { text: string }) => events.push(event.text), abort.signal);
   return { calls, events, snapshots };
  } finally { window.fetch = originalFetch; }
 }, snapshot);
 expect(result.events).toEqual(['An actual reply']);
 expect(result.snapshots).toEqual([6]);
 expect(result.calls).toEqual(['/api/engine/sessions/session-1/events?after=4']);
});

test('a chosen model is accepted and failed requests never silently create a replacement session', async ({ page }) => {
 await page.goto('/');
 const result = await page.evaluate(async (base) => {
  const path = '/src/features/chat/engine-client.ts'; const client = await import(path);
  const calls: string[] = []; const errors: string[] = []; const models: string[] = [];
  const originalFetch = window.fetch;
  window.fetch = async (input) => {
   calls.push(String(input));
   return calls.length === 1 ? Response.json({ ...base, model: 'another-model' }) : Response.json({ error: 'reattach this conversation' }, { status: 404 });
  };
  try {
   for (let index = 0; index < 2; index++) {
    try { models.push((await client.readEngine(base.id)).model); } catch (error) { errors.push((error as Error).message); }
   }
   return { calls, errors, models };
  } finally { window.fetch = originalFetch; }
 }, snapshot);
 expect(result.models).toEqual(['another-model']);
 expect(result.errors).toEqual(['reattach this conversation']);
 expect(result.calls).toEqual(['/api/engine/sessions/session-1', '/api/engine/sessions/session-1']);
});
