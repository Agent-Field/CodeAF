import test from 'node:test';
import assert from 'node:assert/strict';
import { WorldError, fetchWorld, parseWorldRecord } from './world-client.ts';
import type { WorldRow, WorldTransport } from './world-client.ts';
import { applyWorldRecord, createWorldStore } from './world-store.ts';
import type { WorldState } from './world-store.ts';

const row = (session: string, extra: Partial<WorldRow> = {}): WorldRow => ({
 session, title: session, project: 'repo', sourceFolders: [], state: 'idle', live: true, open: true, running: false, needsYou: false, failed: 0,
 tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 }, ...extra,
});
const frame = (seq: number, type: string, payload: unknown) => `id: ${seq}\ndata: ${JSON.stringify({ seq, type, at: 'x', payload })}\n\n`;

/** A transport whose streams the test feeds by hand. */
function fakeTransport() {
 const calls: { path: string; signal?: AbortSignal; push: (text: string) => void; end: () => void; fail: (error: Error) => void }[] = [];
 const encoder = new TextEncoder();
 const refuse: { status?: number } = {};
 const transport: WorldTransport = async (path, init) => {
  if (refuse.status) return new Response(JSON.stringify({ error: 'engine connection required' }), { status: refuse.status });
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({ start(c) { controller = c; } });
  const call = { path, signal: init.signal, push: (text: string) => controller.enqueue(encoder.encode(text)), end: () => controller.close(), fail: (error: Error) => controller.error(error) };
  calls.push(call);
  init.signal?.addEventListener('abort', () => { try { controller.error(new DOMException('aborted', 'AbortError')); } catch { /* already closed */ } });
  return new Response(body, { status: 200, headers: { 'content-type': 'text/event-stream' } });
 };
 return { transport, calls, refuse };
}
function fakeTimers() {
 const pending = new Map<number, { fn: () => void; ms: number }>();
 let next = 1;
 return {
  setTimer: (fn: () => void, ms: number) => { pending.set(next, { fn, ms }); return next++; },
  clearTimer: (handle: unknown) => { pending.delete(handle as number); },
  pending,
  fireAll() { for (const [id, t] of [...pending]) { pending.delete(id); t.fn(); } },
 };
}
const tick = () => new Promise(resolve => setTimeout(resolve, 5));

test('two subscribers share one stream and the last one aborts it', async () => {
 const { transport, calls } = fakeTransport();
 const store = createWorldStore({ transport });
 const a = store.subscribe(() => {}), b = store.subscribe(() => {});
 await tick();
 assert.equal(calls.length, 1);
 assert.equal(calls[0].path, '/events?after=0');
 a(); a();
 assert.equal(store.subscribers(), 1);
 assert.equal(calls[0].signal?.aborted, false);
 b();
 assert.equal(calls[0].signal?.aborted, true);
 assert.equal(store.getState().status, 'idle');
});

test('a reset then deltas build the state; replayed sequence numbers do nothing', async () => {
 const { transport, calls } = fakeTransport();
 const store = createWorldStore({ transport });
 let notified = 0;
 const off = store.subscribe(() => { notified++; });
 await tick();
 calls[0].push(': alive\n\n' + frame(5, 'reset', { rows: [row('a'), row('b')], items: [] }));
 await tick();
 assert.equal(store.getState().status, 'live');
 assert.equal(store.getState().seq, 5);
 calls[0].push(frame(6, 'world', { rows: [row('b', { running: true }), row('c')], removed: ['a'] }));
 calls[0].push(frame(6, 'world', { rows: [row('zzz')], removed: [] }));
 calls[0].push(frame(4, 'attention', { items: [] }));
 await tick();
 const state = store.getState();
 assert.deepEqual(state.rows.map(r => r.session), ['b', 'c']);
 assert.equal(state.rows[0].running, true);
 assert.equal(state.seq, 6);
 const before = notified;
 calls[0].push(frame(6, 'attention', { items: [] }));
 await tick();
 assert.equal(notified, before, 'a duplicate record must not notify');
 off();
});

test('records split across chunks and CRLF framing are read once', async () => {
 const { transport, calls } = fakeTransport();
 const store = createWorldStore({ transport });
 const off = store.subscribe(() => {});
 await tick();
 const text = frame(1, 'reset', { rows: [row('a')], items: [{ key: 'a:consent:7', session: 'a', kind: 'consent', text: 'Run it?', sourceFolders: [], answerable: true }] }).replace(/\n/g, '\r\n');
 calls[0].push(text.slice(0, 20));
 calls[0].push(text.slice(20));
 await tick();
 assert.equal(store.getState().rows.length, 1);
 assert.equal(store.getState().items[0].text, 'Run it?');
 off();
});

test('a lost stream reconnects from the last cursor and keeps what it knew', async () => {
 const { transport, calls } = fakeTransport();
 const timers = fakeTimers();
 const store = createWorldStore({ transport, retryMs: 1000, maxRetryMs: 4000, setTimer: timers.setTimer, clearTimer: timers.clearTimer });
 const off = store.subscribe(() => {});
 await tick();
 calls[0].push(frame(9, 'reset', { rows: [row('a')], items: [] }));
 await tick();
 calls[0].end();
 await tick();
 assert.equal(store.getState().status, 'unavailable');
 assert.equal(store.getState().rows.length, 1, 'honest status, not a blank list');
 assert.deepEqual([...timers.pending.values()].map(t => t.ms), [1000]);
 timers.fireAll();
 await tick();
 assert.equal(calls.length, 2);
 assert.equal(calls[1].path, '/events?after=9');
 calls[1].end();
 await tick();
 assert.deepEqual([...timers.pending.values()].map(t => t.ms), [2000], 'backoff doubles');
 off();
 assert.equal(timers.pending.size, 0, 'unsubscribing cancels the pending reconnect');
 assert.equal(calls.length, 2);
});

test('a reset at the same sequence replaces the state', () => {
 const state: WorldState = { status: 'live', seq: 3, rows: [row('a')], items: [] };
 const next = applyWorldRecord(state, { seq: 3, type: 'reset', at: 'x', payload: { rows: [row('z')], items: [] } });
 assert.deepEqual(next.rows.map(r => r.session), ['z']);
 assert.equal(applyWorldRecord(state, { seq: 3, type: 'attention', at: 'x', payload: { items: [] } }), state);
});

test('a refused connection is unavailable, does not retry on a timer, and recovers on retryNow', async () => {
 const { transport, calls, refuse } = fakeTransport();
 const timers = fakeTimers();
 refuse.status = 401;
 const store = createWorldStore({ transport, setTimer: timers.setTimer, clearTimer: timers.clearTimer });
 const off = store.subscribe(() => {});
 await tick();
 assert.equal(store.getState().status, 'unavailable');
 assert.equal(store.getState().error, 'engine connection required');
 assert.equal(timers.pending.size, 0);
 refuse.status = undefined;
 store.retryNow();
 await tick();
 assert.equal(calls.length, 1);
 calls[0].push(frame(1, 'reset', { rows: [], items: [] }));
 await tick();
 assert.equal(store.getState().status, 'live');
 off();
});

test('an unreadable record is surfaced, never skipped', async () => {
 const { transport, calls } = fakeTransport();
 const timers = fakeTimers();
 const store = createWorldStore({ transport, setTimer: timers.setTimer, clearTimer: timers.clearTimer });
 const off = store.subscribe(() => {});
 await tick();
 calls[0].push('data: {"seq":1,"type":"mystery","payload":{}}\n\n');
 await tick();
 assert.equal(store.getState().status, 'unavailable');
 assert.match(store.getState().error ?? '', /unknown world record/);
 off();
});

test('parseWorldRecord rejects shapes the engine never sends', () => {
 assert.throws(() => parseWorldRecord('nope'), WorldError);
 assert.throws(() => parseWorldRecord('{"seq":-1,"type":"world","payload":{}}'), WorldError);
 assert.throws(() => parseWorldRecord('{"seq":1,"type":"world","payload":{"rows":[]}}'), WorldError);
 assert.equal(parseWorldRecord('{"seq":1,"type":"attention","at":"x","payload":{"items":[]}}').type, 'attention');
});

test('fetchWorld reads the whole state and reports a refusal in the engine words', async () => {
 const ok: WorldTransport = async () => new Response(JSON.stringify({ seq: 4, rows: [row('a')], items: [] }), { status: 200 });
 assert.equal((await fetchWorld(ok)).seq, 4);
 const bad: WorldTransport = async () => new Response(JSON.stringify({ error: 'engine connection required' }), { status: 401 });
 await assert.rejects(() => fetchWorld(bad), (error: WorldError) => error.status === 401 && error.message === 'engine connection required');
 const broken: WorldTransport = async () => new Response('{"seq":"x"}', { status: 200 });
 await assert.rejects(() => fetchWorld(broken), /invalid world/);
});

test('engine identity crosses resets and rejects cross-epoch deltas', () => {
 const a = applyWorldRecord({ status: 'live', epoch: 'old', seq: 900, rows: [row('old')], items: [] }, { epoch: 'new', seq: 3, type: 'reset', at: 'x', payload: { rows: [row('new')], items: [] } });
 assert.equal(a.epoch, 'new'); assert.equal(a.seq, 3);
 const delayed = applyWorldRecord(a, { epoch: 'old', seq: 901, type: 'attention', at: 'x', payload: { items: [] } });
 assert.equal(delayed, a);
});
