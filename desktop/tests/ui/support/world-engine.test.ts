import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createPlacesClient, type PlacesTransport } from '../../../src/features/places/client.ts';
import { createUsingClient } from '../../../src/features/places/using-client.ts';
import { createWorldClient } from '../../../src/features/world/worldClient.ts';
import { parseRecord } from '../../../src/features/workspace-sync/client.ts';
import { createWorldEngine, WORLD_NOW, type WorldEngine, type WorldResponse } from './world-engine.ts';

const srcRoot = fileURLToPath(new URL('../../../src/', import.meta.url));

function filesUnder(dir: string): string[] {
  const found: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) found.push(...filesUnder(path));
    else if (/\.(ts|tsx|js|jsx|css|json)$/.test(name)) found.push(path);
  }
  return found;
}

async function ask(engine: WorldEngine, path: string, method = 'GET', body: Record<string, unknown> = {}, search = ''): Promise<WorldResponse> {
  const response = await engine.dispatch({ method, path, search: new URLSearchParams(search), body });
  assert.ok(response, `${method} ${path} should be owned by the world mock`);
  return response;
}

function transport(engine: WorldEngine): PlacesTransport {
  return async (path, request) => {
    const response = await ask(engine, path, request.method, (request.body ?? {}) as Record<string, unknown>);
    if (response.status !== 200) throw new Error(`${response.status} ${JSON.stringify(response.body)}`);
    return response.body;
  };
}

test('the renderer never imports the world fixture', () => {
  const hits = filesUnder(srcRoot).filter(path => readFileSync(path, 'utf8').includes('world-engine'));
  assert.deepEqual(hits, []);
});

test('conversation routes stay with the conversation mock', async () => {
  const engine = createWorldEngine('typicalDay');
  const missed = await engine.dispatch({ method: 'POST', path: '/sessions/mock-1/turn', search: new URLSearchParams(), body: { text: 'hi' } });
  assert.equal(missed, undefined);
  assert.equal(engine.counts.events + engine.counts.places + engine.counts.jobs, 0);
  // A slash inside the session id stays one segment, the same shape the jobs client encodes.
  const encoded = await engine.dispatch({ method: 'GET', path: '/sessions/sess%2F1/jobs', search: new URLSearchParams() });
  assert.equal(encoded?.status, 404);
  assert.equal((encoded?.body as { error: string }).error, 'reattach this conversation');
  assert.equal(engine.counts.jobs, 1);
});

test('typicalDay answers the world, places, using, workspace, settings and jobs', async () => {
  const engine = createWorldEngine('typicalDay');
  const places = createPlacesClient(transport(engine));
  const graph = await places.graph();
  assert.equal(graph.readAt, WORLD_NOW);
  assert.equal(graph.places.length, 9);
  assert.deepEqual(graph.rail.pinned.map(place => place.name), ['codeaf', 'Personal']);
  assert.deepEqual(graph.rail.open.map(place => place.name), ['Config parser', 'Marketing', 'Q3 report']);
  assert.equal(graph.now.chats, 3);
  assert.equal(graph.totals.running, 1);
  const reports = graph.places.find(place => place.name === 'Reports');
  assert.ok(reports);
  assert.equal((await places.home(reports.id)).children.length, 1);

  const using = await createUsingClient(transport(engine)).using('mock-1');
  assert.equal(using.chatId, 'config-stack');
  assert.deepEqual(using.bundle.places.map(place => place.name), ['Config parser', 'Software', 'codeaf']);
  assert.equal(using.bundle.counts.sources, 1);
  assert.equal(using.engine.places, true);

  const world = await ask(engine, '/events', 'GET', {}, 'after=0');
  assert.match(world.sse ?? '', /text\/|data:/);
  const record = JSON.parse((world.sse ?? '').split('\n').find(line => line.startsWith('data:'))!.slice(5));
  assert.equal(record.type, 'reset');
  assert.equal(record.epoch, 'mock-world');
  const row = record.payload.rows.find((item: { chatId: string }) => item.chatId === 'config-stack');
  assert.equal(row.needsYou, 1);
  assert.equal(row.running, true);
  assert.equal(typeof row.session, 'undefined');

  const client = createWorldClient({
    transport: async () => new Response(world.sse, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }),
    setTimer: () => 0,
    clearTimer: () => {},
  });
  const stop = client.subscribe(() => {});
  for (let i = 0; i < 20 && client.cursor().seq === 0; i++) await new Promise(resolve => setImmediate(resolve));
  assert.equal(client.row('config-stack')?.needsYou, 1);
  assert.equal(client.jobs('config-stack')[0]?.state, 'running');
  assert.equal(client.lastWorkspace('now')?.revision, 1);
  assert.ok(client.lastPlaces());
  stop();

  const tabs = await ask(engine, '/workspaces/now');
  assert.equal(parseRecord(tabs.body, 'now').revision, 1);
  const conflict = await ask(engine, '/workspaces/now', 'PUT', { revision: 0, writer: 'win-b', workspace: { schema: 1, tabs: [], groups: [], closed: [], nextNumber: 1 } });
  assert.equal(conflict.status, 409);
  assert.equal((conflict.body as { code: string }).code, 'conflict');

  const key = await ask(engine, '/settings/key');
  assert.deepEqual(key.body, { present: true, source: 'profile' });
  const facts = await ask(engine, '/settings/engine');
  assert.deepEqual(facts.body, { local: true, connection: 'local', model: 'deepseek/deepseek-v4.1-flash' });
  const saved = await ask(engine, '/settings/permissions', 'PUT', { mode: 'allow' });
  assert.equal((saved.body as { mode: string }).mode, 'allow');
  const refused = await ask(engine, '/settings/permissions', 'PUT', { mode: 'secret-canary' });
  assert.equal(refused.status, 400);

  const jobs = await ask(engine, '/sessions/mock-1/jobs');
  assert.equal((jobs.body as { state?: string }[])[0].state, 'running');
  assert.equal('exitCode' in (jobs.body as object[])[0], false);
  const stopped = await ask(engine, '/sessions/mock-1/jobs/1/stop', 'POST', {});
  assert.deepEqual(stopped.body, { accepted: true });
  const after = await ask(engine, '/sessions/mock-1/jobs');
  assert.equal((after.body as { state?: string; exitCode?: number }[])[0].state, 'stopped');
  assert.equal((after.body as { exitCode?: number }[])[0].exitCode, undefined);
  const log = await ask(engine, '/sessions/mock-1/jobs/1/log', 'GET', {}, 'tail=4');
  assert.equal((log.body as { truncated: boolean }).truncated, true);

  assert.ok(engine.counts.events >= 1);
  assert.ok(engine.counts.places >= 1);
  assert.ok(engine.counts.using >= 1);
  assert.ok(engine.counts.workspaces >= 1);
  assert.ok(engine.counts.settings >= 1);
  assert.ok(engine.counts.jobs >= 1);
  assert.ok(engine.calls.some(call => call.path === '/sessions/mock-1/jobs/1/stop' && call.method === 'POST'));
});

test('empty renders nothing and twoHundredPlaces is two hundred deep', async () => {
  const empty = createWorldEngine('empty');
  const quiet = createPlacesClient(transport(empty));
  assert.equal((await quiet.graph()).places.length, 0);
  assert.deepEqual((await ask(empty, '/settings/key')).body, { present: false });
  assert.equal((await ask(empty, '/workspaces/now')).status, 200);
  assert.equal(((await ask(empty, '/workspaces/now')).body as { revision: number }).revision, 0);
  assert.equal((await ask(empty, '/sessions/mock-1/jobs')).status, 404);
  const reset = JSON.parse(((await ask(empty, '/events', 'GET', {}, 'after=0')).sse ?? '').split('\n').find(line => line.startsWith('data:'))!.slice(5));
  assert.deepEqual(reset.payload.rows, []);
  assert.deepEqual(reset.payload.items, []);

  const wide = createWorldEngine('twoHundredPlaces');
  const graph = await createPlacesClient(transport(wide)).graph();
  assert.equal(graph.places.length, 200);
  const depth = (id: string, seen = new Set<string>()): number => {
    const place = graph.places.find(row => row.id === id)!;
    if (seen.has(id) || place.parents.length === 0) return 1;
    seen.add(id);
    return 1 + Math.max(...place.parents.map(parent => depth(parent, seen)));
  };
  assert.equal(Math.max(...graph.places.map(place => depth(place.id))), 3);
  const reports = graph.places.find(place => place.name === 'Reports')!;
  assert.equal((await createPlacesClient(transport(wide)).home(reports.id)).children.length, 47);
});
