import test from 'node:test';
import assert from 'node:assert/strict';
import { markFailureSeen, WorldError, type WorldTransport } from './world-client.ts';

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

test('markFailureSeen posts the identity it was given and returns the engine\'s answer', async () => {
  let seen: { path: string; method?: string; body?: string } | undefined;
  const transport: WorldTransport = async (path, init) => { seen = { path, method: init.method, body: init.body }; return json(200, { session: 'c', through: 't', changed: false, unseenFailed: 1 }); };
  assert.deepEqual(await markFailureSeen(transport, { session: 'c', at: 't', task: '2' }), { session: 'c', through: 't', changed: false, unseenFailed: 1 });
  assert.deepEqual(seen, { path: '/world/failures/seen', method: 'POST', body: '{"session":"c","at":"t","task":"2"}' });
});

test('markFailureSeen carries the engine\'s refusal sentence and status, and rejects a reply it cannot trust', async () => {
  await assert.rejects(markFailureSeen(async () => json(409, { error: 'that failure is no longer on record' }), { session: 'c', at: 't' }), (e: WorldError) => e.status === 409 && /no longer on record/.test(e.message) && !e.unreachable);
  await assert.rejects(markFailureSeen(async () => json(404, { error: 'no such conversation' }), { session: 'c', at: 't' }), (e: WorldError) => e.status === 404);
  await assert.rejects(markFailureSeen(async () => json(200, { session: 'c' }), { session: 'c', at: 't' }), /invalid reply/);
});
