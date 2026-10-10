import { test, expect } from '@playwright/test';
import { installNativeHttpMock } from './support/native-http-mock';
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

test('exported helpers carry the token, the 30s clock and If-Match', async ({ page }) => {
 await installNativeHttpMock(page);
 // The transport fixture has no app observers that could make unrelated requests during the clock probe.
 await page.route('**/__transport-test', route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Transport fixture</title>' }));
 await page.goto('/__transport-test');
 const result = await page.evaluate(async () => {
  const path = '/src/features/chat/engine-client.ts'; const client = await import(path);
  const calls: { path: string; method: string; token: string | null; match: string | null; accept: string | null; contentType: string | null; body?: string }[] = [];
  const timers: number[] = []; const cleared: number[] = []; const records: string[] = [];
  const originalFetch = window.fetch; const originalTimeout = window.setTimeout; const originalClear = window.clearTimeout;
  let expire: (() => void) | undefined;
  const native = window as unknown as { isTauri?: boolean; __TAURI_INTERNALS__?: unknown; __engineHttpInvoke: (command: string, args: Record<string, unknown>) => Promise<unknown> };
  const originalTauri = native.isTauri; const originalInternals = native.__TAURI_INTERNALS__;
  native.isTauri = true;
  native.__TAURI_INTERNALS__ = { invoke: async (command: string, args: Record<string, unknown>) => {
   if (command === 'engine_connection') return { url: location.origin, token: 'fixture-transport-token', model: 'deepseek/deepseek-v4.1-flash' };
   return native.__engineHttpInvoke(command, args);
  } };
  window.setTimeout = ((callback: () => void, delay: number) => {
   if (delay !== client.ENGINE_REQUEST_TIMEOUT_MS) return originalTimeout(callback, delay);
   timers.push(delay); expire = callback; return 987654;
  }) as typeof window.setTimeout;
  window.clearTimeout = ((id: number) => { if (id === 987654) cleared.push(id); else originalClear(id); }) as typeof window.clearTimeout;
  window.fetch = async (input, init) => {
   const url = new URL(String(input)); const headers = new Headers(init?.headers);
   calls.push({ path: url.pathname + url.search, method: init?.method ?? 'GET', token: headers.get('Authorization'), match: headers.get('If-Match'), accept: headers.get('Accept'), contentType: headers.get('Content-Type'), body: init?.body ? new TextDecoder().decode(init.body as Uint8Array) : undefined });
   if (url.pathname.endsWith('/timeout')) return new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true });
    expire!();
   });
   if (url.pathname.endsWith('/conflict')) return Response.json({ error: 'This workspace changed. Reload it before saving.' }, { status: 412 });
   if (url.pathname.endsWith('/events')) return new Response(new ReadableStream({ start(controller) {
    controller.enqueue(new TextEncoder().encode(': heartbeat\r\n\r\ndata: {"seq":8,\r\ndata: "type":"world"}\r\n\r\ndata: ignored after detach\r\n\r\n'));
   } }), { headers: { 'Content-Type': 'text/event-stream' } });
   return Response.json({ version: 8 });
  };
  try {
   const saved = await client.engineJson('/workspaces/now', { method: 'PUT', headers: { 'If-Match': '7' }, body: JSON.stringify({ tabs: [] }) });
   const errors = [];
   for (const path of ['/conflict', '/timeout']) {
    try { await client.engineJson(path); } catch (error) {
     const engineError = error as { message: string; status: number; unreachable: boolean };
     errors.push({ engineError: error instanceof client.EngineError, message: engineError.message, status: engineError.status, unreachable: engineError.unreachable });
    }
   }
   const abort = new AbortController();
   await client.engineEventStream('/events?scope=world', 7, (text: string) => { records.push(text); abort.abort(); }, abort.signal);
   return { saved, calls, timers, cleared: cleared.length, errors, records };
  } finally {
   window.fetch = originalFetch; window.setTimeout = originalTimeout; window.clearTimeout = originalClear;
   native.isTauri = originalTauri; native.__TAURI_INTERNALS__ = originalInternals;
  }
 });
 expect(result.saved).toEqual({ version: 8 });
 expect(result.calls.map(call => call.token)).toEqual(Array(4).fill('Bearer fixture-transport-token'));
 expect(result.calls[0]).toMatchObject({ path: '/api/engine/workspaces/now', method: 'PUT', match: '7', contentType: 'application/json' });
 expect(result.calls[0].body).toBe('{"tabs":[]}');
 expect(result.timers).toEqual([30_000, 30_000, 30_000]);
 expect(result.cleared).toBe(3);
 expect(result.errors).toEqual([
  { engineError: true, message: 'This workspace changed. Reload it before saving.', status: 412, unreachable: false },
  { engineError: true, message: 'codeaf engine is not running', status: 0, unreachable: true },
 ]);
 expect(result.calls[3]).toMatchObject({ path: '/api/engine/events?scope=world&after=7', method: 'GET', accept: 'text/event-stream' });
 expect(result.records).toEqual(['{"seq":8,\n"type":"world"}']);
});
