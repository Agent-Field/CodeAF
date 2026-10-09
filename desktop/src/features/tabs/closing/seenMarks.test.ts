import test from 'node:test';
import assert from 'node:assert/strict';
import { createSeenMarks } from './seenMarks.ts';
import type { WorldTransport } from '../../chat/world-client.ts';

const failure = { task: '2', at: '2026-10-02T10:00:00.25Z' };
const reply = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

test('a mark is sent to the engine with the failure it was shown, and the row leaves at once', async () => {
  const sent: { path: string; init: Parameters<WorldTransport>[1] }[] = [];
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const marks = createSeenMarks({ transport: async (path, init) => { sent.push({ path, init }); await gate; return reply(200, { session: 'c', through: failure.at, changed: true, unseenFailed: 0 }); }, onRefused: () => assert.fail('refused') });
  const done = marks.mark('c', failure);
  assert.deepEqual(marks.getPending(), { c: failure.at }, 'pending before the engine has answered');
  release();
  await done;
  assert.equal(sent.length, 1);
  assert.equal(sent[0].path, '/world/failures/seen');
  assert.equal(sent[0].init.method, 'POST');
  assert.deepEqual(JSON.parse(sent[0].init.body!), { session: 'c', at: failure.at, task: '2' });
  assert.deepEqual(marks.getPending(), { c: failure.at }, 'stays pending until the feed echoes the new count');
});

test('a repeat while pending sends nothing', async () => {
  let calls = 0;
  const marks = createSeenMarks({ transport: async () => { calls += 1; return reply(200, { session: 'c', through: failure.at, changed: true, unseenFailed: 0 }); }, onRefused: () => undefined });
  await marks.mark('c', failure);
  await marks.mark('c', failure);
  assert.equal(calls, 1);
});

test('a refused or unreachable mark brings the failure back and says why, never claiming it was seen', async () => {
  for (const [transport, words] of [
    [async () => reply(409, { error: 'that failure is no longer on record' }), 'no longer on record'],
    [async () => { throw new TypeError('offline'); }, 'Could not mark'],
    [async () => reply(500, {}), 'Could not reach the engine'],
  ] as const) {
    const said: string[] = [];
    const marks = createSeenMarks({ transport: transport as WorldTransport, onRefused: message => said.push(message) });
    await marks.mark('c', failure);
    assert.deepEqual(marks.getPending(), {}, 'the pending mark is withdrawn');
    assert.equal(said.length, 1);
    assert.match(said[0], new RegExp(words));
  }
});
