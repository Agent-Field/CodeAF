import { test, expect } from '@playwright/test';
const model = 'deepseek/deepseek-v4.1-flash';
const snapshot = { id: 'session-1', sessionFile: 'saved-session.jsonl', workspace: '/workspace', model, persistent: true, running: false, needsPerson: false, entries: [], tasks: [], usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 }, title: 'Real conversation', seq: 4 };

test('canonical history keeps user boundaries and presentation flags without inventing answers', async ({ page }) => {
 await page.goto('/');
 const result = await page.evaluate(async (base) => {
  const path = '/src/features/chat/engine-client.ts'; const client = await import(path);
  const next = { ...base, entries: [
   { Role: 'user', Text: 'Inspect the real engine' },
   { Role: 'aside', Text: 'Task ended: this was written by the session' },
   { Role: 'tool', Text: 'secret-looking tool output is not an answer', Tool: 'shell' },
   { Role: 'tool', Text: 'duplicate raw tool result' },
   { Role: 'assistant', Text: 'The shared engine is available.\nSecond line.', Answer: true },
   { Role: 'note', Text: 'Compaction marker' },
   { Role: 'user', Text: 'Continue with tests' },
  ] };
  const first = client.projectEngineSections(next, []);
  first[0].folded = true; first[0].originalOpen = true; first[0].stepsOpen = true;
  const replay = client.projectEngineSections(next, first);
  return replay;
 }, snapshot);
 expect(result).toHaveLength(2);
 expect(result[0].recordedSteps).toHaveLength(1);
 expect(result[0].original).toBe('Inspect the real engine');
 expect(result[0].digest).toBe('The shared engine is available.');
 expect(result[0].blocks).toEqual([{ kind: 'paragraph', text: 'The shared engine is available.\nSecond line.' }]);
 expect(result[0]).toMatchObject({ folded: true, originalOpen: true, stepsOpen: true, sample: false, engineNotes: ['Task ended: this was written by the session', 'Compaction marker'], recordedSteps: [{ tool: 'shell', hint: '', args: '', output: '', answered: false }] });
 expect(result[1]).toMatchObject({ original: 'Continue with tests', digest: '', blocks: [], sample: false });
});

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

test('fixed model and failed requests never silently create a replacement session', async ({ page }) => {
 await page.goto('/');
 const result = await page.evaluate(async (base) => {
  const path = '/src/features/chat/engine-client.ts'; const client = await import(path);
  const calls: string[] = []; const errors: string[] = [];
  const originalFetch = window.fetch;
  window.fetch = async (input) => {
   calls.push(String(input));
   return calls.length === 1 ? Response.json({ ...base, model: 'another-model' }) : Response.json({ error: 'reattach this conversation' }, { status: 404 });
  };
  try {
   for (let index = 0; index < 2; index++) {
    try { await client.readEngine(base.id); } catch (error) { errors.push((error as Error).message); }
   }
   return { calls, errors };
  } finally { window.fetch = originalFetch; }
 }, snapshot);
 expect(result.errors[0]).toContain('required DeepSeek v4.1 Flash');
 expect(result.errors[1]).toBe('reattach this conversation');
 expect(result.calls).toEqual(['/api/engine/sessions/session-1', '/api/engine/sessions/session-1']);
});
