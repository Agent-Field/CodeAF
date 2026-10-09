import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { chatIdFromSessionFile, createPlacesClient, PlacesError, type PlacesRequest, type PlacesTransport } from './client.ts';

// The fixtures are written by the Go handlers' own test (TestPlacesWireFixtures),
// so these tests parse what the engine really sends, not a hand-copied shape.
const fixture = (name: string): any => JSON.parse(readFileSync(new URL(`./fixtures/${name}.json`, import.meta.url), 'utf8'));

type Seen = { path: string; request: PlacesRequest };
function recording(answer: (path: string, request: PlacesRequest) => unknown): { transport: PlacesTransport; seen: Seen[] } {
  const seen: Seen[] = [];
  return { seen, transport: async (path, request) => { seen.push({ path, request }); return structuredClone(answer(path, request)); } };
}

test('the graph fixture parses and carries the roll-ups the engine computed', async () => {
  const { transport } = recording(() => fixture('graph'));
  const graph = await createPlacesClient(transport).graph();
  const software = graph.places.find(p => p.name === 'Software')!;
  assert.deepEqual([software.statusInclusive.chats, software.statusInclusive.needsYou, software.statusInclusive.running], [3, 1, 1]);
  assert.equal(software.status.chats, 0);
  assert.equal(software.pinned, true);
  assert.equal(graph.totals.unplaced, 1);
  const release = graph.places.find(p => p.name === 'Release')!;
  assert.equal(release.alsoIn.length, 1, 'a diamond child names its other parent');
  assert.equal(graph.rail.pinned[0].id, software.id);
  assert.equal(graph.recovery, undefined);
});

test('status, rail, Home digests, chat places and delete preview parse', async () => {
  const answers: Record<string, string> = {
    '/places/status': 'status', '/places/rail': 'rail', '/places/root': 'home-root', '/places/now': 'home-now',
    '/places/pl_0000000000000005': 'home-place', '/places/pl_0000000000000001': 'home-parent',
    '/chats/sess-need/places': 'chat-places', '/places/pl_0000000000000005/delete-preview': 'delete-preview',
  };
  const client = createPlacesClient(async path => fixture(answers[path] ?? assert.fail(`unexpected ${path}`)));
  const status = await client.status();
  assert.equal(status.places['pl_0000000000000001'].statusInclusive.needsYou, 1);
  assert.equal((await client.rail()).openWindowHours, 12);
  const root = await client.home('root');
  assert.equal(root.kind, 'root'); assert.equal(root.place, undefined);
  const now = await client.home('now');
  assert.deepEqual(now.chats.map(c => c.id), ['sess-loose']);
  const place = await client.home('pl_0000000000000005');
  assert.equal(place.kind, 'place');
  assert.equal(place.place?.sources[0].check.state, 'ok');
  assert.deepEqual(place.attention.map(a => a.kind), ['needsYou', 'running']);
  assert.equal(place.chats[1].reason, 'Allow the v1 branch push?');
  const parent = await client.home('pl_0000000000000001');
  assert.equal(parent.attention[0].placeName, 'Config parser', 'a parent attributes child news to the child');
  const who = await client.chatPlaces('sess-need');
  assert.equal(who.known, true);
  assert.equal((await client.deletePreview('pl_0000000000000005')).chatsHere, 2);
});

test('write receipts parse and an empty write is a noop with no undo', async () => {
  const create = await createPlacesClient(async () => fixture('create')).createPlace({ name: 'Software' });
  assert.equal(create.noop, false);
  assert.equal(create.undo.length, 1);
  assert.equal(create.place?.name, 'Software');
  const members = await createPlacesClient(async () => fixture('members')).addChats('pl_0000000000000005', ['sess-need', 'sess-run']);
  assert.equal(members.receipts.length, 2);
  assert.equal(members.memberships?.length, 2);
  const sources = await createPlacesClient(async () => fixture('sources')).addSource('pl_0000000000000005', { kind: 'repo', ref: '/work/repo' });
  assert.equal(sources.place?.sources[0].label, 'repo');
  const undo = await createPlacesClient(async () => fixture('undo')).undo(['rc_x']);
  assert.equal(undo.undone, 1);
  await createPlacesClient(async () => fixture('visit')).visit('pl_0000000000000003');
  const noop = await createPlacesClient(async () => ({ revision: 3, receipts: [], noop: true, undo: [] })).archivePlace('pl_x');
  assert.equal(noop.noop, true);
});

test('each call sends the documented method, path and body', async () => {
  const { transport, seen } = recording(path => path.endsWith('/visit') ? { ok: true } : path === '/places/undo' ? { revision: 1, undone: 2 } : fixture('create'));
  const c = createPlacesClient(transport);
  await c.createPlace({ name: 'A', parent: 'pl_1' });
  await c.updatePlace('pl_a/b', { tint: '' });
  await c.setParents('pl_1', { add: 'pl_2' });
  await c.addChats('now', ['s1'], { moveFrom: 'pl_1' });
  await c.removeChats('pl_1', ['s1']);
  await c.removeSource('pl_1', 'src_1');
  await c.pinPlace('pl_1', 0);
  await c.visit('pl_1');
  await c.undo(['rc_1', 'rc_2']);
  assert.deepEqual(seen.map(s => [s.request.method, s.path]), [
    ['POST', '/places'], ['POST', '/places/pl_a%2Fb'], ['POST', '/places/pl_1/parents'], ['POST', '/places/now/members'],
    ['POST', '/places/pl_1/members/remove'], ['POST', '/places/pl_1/sources/remove'], ['POST', '/places/pl_1/pin'], ['POST', '/places/pl_1/visit'], ['POST', '/places/undo'],
  ]);
  assert.deepEqual(seen[0].request.body, { name: 'A', parent: 'pl_1' });
  assert.deepEqual(seen[1].request.body, { tint: '' }, 'an empty tint is sent: it clears the choice');
  assert.deepEqual(seen[3].request.body, { chats: ['s1'], moveFrom: 'pl_1' });
  assert.deepEqual(seen[8].request.body, { receipts: ['rc_1', 'rc_2'] });
  const reads = recording(() => fixture('graph'));
  await createPlacesClient(reads.transport).graph({ archived: true });
  assert.equal(reads.seen[0].path, '/places?archived=1');
  assert.equal(reads.seen[0].request.method, 'GET');
});

test('an answer that is not what the engine documents is refused, not trusted', async () => {
  const broken = fixture('graph');
  delete broken.places[0].statusInclusive;
  await assert.rejects(createPlacesClient(async () => broken).graph(), (e: unknown) => e instanceof PlacesError && /invalid place/.test(e.message));
  const lying = fixture('create');
  lying.noop = true; // says nothing changed while carrying a receipt
  await assert.rejects(createPlacesClient(async () => lying).createPlace({ name: 'x' }), PlacesError);
  const homeWithoutPlace = fixture('home-place');
  delete homeWithoutPlace.place;
  await assert.rejects(createPlacesClient(async () => homeWithoutPlace).home('pl_x'), PlacesError);
  await assert.rejects(createPlacesClient(async () => null).graph(), PlacesError);
  await assert.rejects(createPlacesClient(async () => ({ revision: 'x' })).undo(['rc']), PlacesError);
});

// The default transport, over a stubbed fetch, with the engine's real error bodies.
async function withFetch<T>(stub: typeof fetch, run: () => Promise<T>): Promise<T> {
  const original = globalThis.fetch;
  globalThis.fetch = stub;
  try { return await run(); } finally { globalThis.fetch = original; }
}
const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

test('the engine refuses in a sentence and the client keeps the sentence, the code and what was applied', async () => {
  const sentence = fixture('error-name-taken');
  await withFetch(async () => json(409, sentence), async () => {
    await assert.rejects(createPlacesClient().createPlace({ name: 'software' }), (e: unknown) =>
      e instanceof PlacesError && e.status === 409 && e.code === 'name_taken' && e.message === sentence.error && !e.unreachable && e.applied.length === 0);
  });
  await withFetch(async () => json(404, fixture('error-unknown-chat')), async () => {
    await assert.rejects(createPlacesClient().addChats('pl_1', ['ghost']), (e: unknown) => e instanceof PlacesError && e.code === 'unknown_chat');
  });
  await withFetch(async () => json(400, { error: 'Nope.', code: 'invalid', applied: [{ id: 'rc_1' }], undone: 1 }), async () => {
    await assert.rejects(createPlacesClient().updatePlace('pl_1', { name: 'x' }), (e: unknown) => e instanceof PlacesError && e.applied.length === 1 && e.undone === 1);
  });
});

test('nothing answering is "unreachable"; a 5xx with no sentence is too', async () => {
  await withFetch(async () => { throw new TypeError('fetch failed'); }, async () => {
    await assert.rejects(createPlacesClient().graph(), (e: unknown) => e instanceof PlacesError && e.unreachable && e.message === 'codeaf engine is not running');
  });
  await withFetch(async () => new Response('bad gateway', { status: 502 }), async () => {
    await assert.rejects(createPlacesClient().graph(), (e: unknown) => e instanceof PlacesError && e.unreachable);
  });
  await withFetch(async () => json(503, { error: 'Places are busy in another window. Try again in a moment.', code: 'busy' }), async () => {
    await assert.rejects(createPlacesClient().graph(), (e: unknown) => e instanceof PlacesError && !e.unreachable && e.code === 'busy');
  });
});

test('the default transport talks to /api/engine with JSON bodies outside the native app', async () => {
  let seen: { url: string; init: RequestInit } | undefined;
  await withFetch(async (url, init) => { seen = { url: String(url), init: init ?? {} }; return json(200, fixture('create')); }, async () => {
    await createPlacesClient().createPlace({ name: 'Software' });
  });
  assert.equal(seen?.url, '/api/engine/places');
  assert.equal(seen?.init.method, 'POST');
  assert.equal(new Headers(seen?.init.headers).get('Content-Type'), 'application/json');
  assert.equal(seen?.init.body, JSON.stringify({ name: 'Software' }));
});

test('a chat id is the folder that holds the transcript', () => {
  assert.equal(chatIdFromSessionFile('/home/u/.codeaf/v3/projects/bucket/abc123/transcript.jsonl'), 'abc123');
  assert.equal(chatIdFromSessionFile('C:\\Users\\u\\sessions\\abc123\\t.jsonl'), 'abc123');
  assert.equal(chatIdFromSessionFile(''), '');
  assert.equal(chatIdFromSessionFile('transcript.jsonl'), '');
});
