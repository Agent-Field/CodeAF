import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createWorldClient, worldClient } from './worldClient.ts';
import type { WorldRecord, WorldRow } from './types.ts';

const row = (chatId: string, title = chatId): WorldRow => ({ chatId, title, running: false, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false });
const record = (seq: number, type: WorldRecord['type'], payload: unknown, epoch = 'engine-a') => ({ seq, epoch, type, payload, at: '2026-10-10T00:00:00Z' });
const tick = () => new Promise<void>(resolve => setImmediate(resolve));

function harness() {
 const calls: { path: string; signal: AbortSignal; controller: ReadableStreamDefaultController<Uint8Array> }[] = [];
 const timers = new Map<number, { fn: () => void; ms: number }>();
 let timerId = 0, active = 0, peak = 0, notifications = 0;
 const client = createWorldClient({
  transport: async (path, init) => {
   const signal = init.signal as AbortSignal;
   active++; peak = Math.max(peak, active);
   const body = new ReadableStream<Uint8Array>({
    start(controller) {
     calls.push({ path, signal, controller });
     signal.addEventListener('abort', () => controller.error(new Error('aborted')), { once: true });
    },
   });
   signal.addEventListener('abort', () => active--, { once: true });
   return new Response(body);
  },
  setTimer: (fn, ms) => { const id = ++timerId; timers.set(id, { fn, ms }); return id; },
  clearTimer: id => { timers.delete(id as number); },
 });
 const listen = () => client.subscribe(() => { notifications++; });
 const send = async (value: unknown) => {
  calls.at(-1)!.controller.enqueue(new TextEncoder().encode(`id: 9\ndata: ${JSON.stringify(value)}\n\n`));
  await tick();
 };
 const retry = async () => {
  const [id, timer] = timers.entries().next().value!;
  timers.delete(id); timer.fn(); await tick();
 };
 const end = async () => { active--; calls.at(-1)!.controller.close(); await tick(); };
 return { client, calls, timers, listen, send, retry, end, peak: () => peak, notifications: () => notifications };
}

test('resumes after the last applied sequence and epoch on EOF', async () => {
 const h = harness(), stop = h.listen();
 await h.send(record(7, 'reset', { rows: [row('a')], items: [] }));
 await h.end();
 assert.equal([...h.timers.values()][0].ms, 1000);
 await h.retry();
 assert.equal(h.calls[1].path, '/events?after=7&epoch=engine-a');
 stop();
});

test('reset replaces every collection and accepts a restarted engine cursor', async () => {
 const h = harness(), stop = h.listen();
 await h.send(record(8, 'reset', { rows: [row('a')], items: [{ id: 'q', chatId: 'a', kind: 'question' }] }));
 await h.send(record(9, 'jobs', { chatId: 'a', running: 1, jobs: [{ id: 1, state: 'running' }] }));
 await h.send(record(10, 'places', { generation: 1, nodes: [], rail: { pinned: [], open: [], openWindowHours: 12 } }));
 await h.send(record(11, 'workspace', { key: 'now', revision: 4, writer: 'one' }));
 await h.send(record(0, 'reset', { rows: [row('b')], items: [] }, 'engine-b'));
 assert.deepEqual(h.client.rows(), [row('b')]);
 assert.equal(h.client.row('a'), undefined);
 assert.deepEqual(h.client.attention(), []);
 assert.deepEqual(h.client.jobs('a'), []);
 assert.equal(h.client.lastPlaces(), undefined);
 assert.equal(h.client.lastWorkspace('now'), undefined);
 await h.send(record(1, 'world', { rows: [row('b', 'old engine')], removed: [] }));
 assert.equal(h.client.row('b')?.title, 'b');
 stop();
});

test('duplicate and older sequences are ignored, including duplicate resets', async () => {
 const h = harness(), stop = h.listen();
 await h.send(record(2, 'reset', { rows: [row('a')], items: [] }));
 const before = h.client.rows(), count = h.notifications();
 await h.send(record(2, 'reset', { rows: [], items: [] }));
 await h.send(record(1, 'world', { rows: [], removed: ['a'] }));
 assert.equal(h.client.rows(), before);
 assert.equal(h.notifications(), count);
 stop();
});

test('selectors preserve references for unchanged records and unrelated changes', async () => {
 const h = harness(), stop = h.listen();
 const full = { rows: [row('a'), row('b')], items: [{ id: 'q', chatId: 'a', kind: 'question' }], jobs: [{ chatId: 'a', running: 1, jobs: [{ id: 1 }] }], places: { generation: 1, nodes: [], rail: { pinned: [], open: [], openWindowHours: 12 } }, workspaces: [{ key: 'now', revision: 1 }] };
 await h.send(record(1, 'reset', full));
 const rows = h.client.rows(), a = h.client.row('a'), items = h.client.attention(), jobs = h.client.jobs('a'), workspace = h.client.lastWorkspace('now');
 const missing = h.client.jobs('missing');
 const places = h.client.lastPlaces();
 await h.send(record(2, 'reset', full));
 assert.equal(h.client.rows(), rows);
 assert.equal(h.client.row('a'), a);
 assert.equal(h.client.attention(), items);
 assert.equal(h.client.jobs('a'), jobs);
 assert.equal(h.client.lastWorkspace('now'), workspace);
 assert.equal(h.client.jobs('missing'), missing);
 assert.equal(h.client.lastPlaces(), places);
 await h.send(record(3, 'world', { rows: [row('b', 'new title')], removed: [] }));
 assert.equal(h.client.row('a'), a);
 assert.equal(h.client.attention(), items);
 assert.equal(h.client.jobs('a'), jobs);
 await h.send(record(4, 'world', { rows: [], removed: ['b'] }));
 assert.equal(h.client.row('b'), undefined);
 stop();
});

test('subscribers share one stream and duplicate cleanup cannot close another subscriber', async () => {
 const h = harness(), stopA = h.listen(), stopB = h.listen();
 assert.equal(h.calls.length, 1);
 stopA(); stopA();
 assert.equal(h.calls[0].signal.aborted, false);
 stopB(); await tick();
 assert.equal(h.calls[0].signal.aborted, true);
 assert.equal(h.timers.size, 0);
 const stopC = h.listen();
 assert.equal(h.calls.length, 2);
 assert.equal(h.peak(), 1);
 stopC();
 assert.equal((await import('./worldClient.ts')).worldClient, worldClient);
});

test('retry doubles to its cap and final unsubscribe cancels a pending retry', async () => {
 const h = harness(), stop = h.listen();
 for (const delay of [1000, 2000, 4000, 8000, 16000, 30000, 30000]) {
  await h.end();
  assert.equal([...h.timers.values()][0].ms, delay);
  await h.retry();
 }
 await h.end(); stop();
 assert.equal(h.timers.size, 0);
});

test('split CRLF frames, multiline data and heartbeats are read without inventing rows', async () => {
 const h = harness(), stop = h.listen();
 const data = JSON.stringify(record(0, 'reset', { rows: [], items: [] }), null, 1).split('\n').map(line => `data: ${line}`).join('\r\n');
 const encoded = new TextEncoder().encode(`: alive\r\n\r\n${data}\r\n\r\n`);
 for (const byte of encoded) h.calls[0].controller.enqueue(Uint8Array.of(byte));
 await tick();
 assert.deepEqual(h.client.rows(), []);
 await h.end(); await h.retry();
 assert.equal(h.calls[1].path, '/events?after=0&epoch=engine-a');
 stop();
});

test('a malformed reset retains the previous state and resumes its previous cursor', async () => {
 const h = harness(), stop = h.listen();
 await h.send(record(4, 'reset', { rows: [row('a')], items: [] }));
 const before = h.client.rows();
 await h.send(record(5, 'reset', { rows: [{ session: 'old-schema', needsYou: false }], items: [] }));
 assert.equal(h.client.rows(), before);
 assert.equal(h.timers.size, 1);
 await h.retry();
 assert.equal(h.calls[1].path, '/events?after=4&epoch=engine-a');
 stop();
});
