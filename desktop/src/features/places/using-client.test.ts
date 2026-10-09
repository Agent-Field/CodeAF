import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { PlacesError, type PlacesRequest, type PlacesTransport } from './client.ts';
import { createUsingClient, settingIsApplied } from './using-client.ts';

// The fixtures are written by the Go handlers' own test (TestUsingWireFixtures),
// so these tests parse what the bridge really sends.
const fixture = (name: string): any => JSON.parse(readFileSync(new URL(`./fixtures/${name}.json`, import.meta.url), 'utf8'));

type Seen = { path: string; request: PlacesRequest };
function recording(answer: (path: string) => unknown): { transport: PlacesTransport; seen: Seen[] } {
  const seen: Seen[] = [];
  return { seen, transport: async (path, request) => { seen.push({ path, request }); return structuredClone(answer(path)); } };
}

test('the Using fixture parses: places with provenance, a conflict that applies nothing, a decided setting', async () => {
  const { transport, seen } = recording(() => fixture('using'));
  const view = await createUsingClient(transport).using('tok en');
  assert.equal(seen[0].path, '/sessions/tok%20en/using', 'the bridge token is the path segment, encoded');
  assert.equal(view.chatId, view.bundle.chatId);
  assert.equal(view.engine.places, true);
  assert.deepEqual(view.bundle.places.map(p => [p.name, p.inherited]), [['Release', false], ['Marketing', false], ['Software', true]]);
  assert.equal(view.bundle.counts.sources, view.bundle.sources.length);
  const model = view.settings.find(s => s.field === 'model')!;
  assert.equal(model.state, 'needsPick');
  assert.equal(model.value, undefined, 'a conflict carries no value');
  assert.equal(settingIsApplied(model), false);
  const permissions = view.settings.find(s => s.field === 'permissions')!;
  assert.equal(permissions.state, 'pending');
  assert.equal(settingIsApplied(permissions), false, 'pending is not applied yet');
});

test('a pick posts the field and place and parses the new view', async () => {
  const { transport, seen } = recording(() => fixture('using-choice'));
  const view = await createUsingClient(transport).choose('t1', 'model', 'pl_0000000000000005');
  assert.deepEqual(seen[0], { path: '/sessions/t1/using/choice', request: { method: 'POST', body: { field: 'model', placeId: 'pl_0000000000000005' } } });
  const decision = view.bundle.policy.find(d => d.field === 'model')!;
  assert.equal(decision.outcome, 'chosen');
  assert.equal(view.settings.find(s => s.field === 'model')!.state, 'pending');
});

test('apply posts only the field', async () => {
  const { transport, seen } = recording(() => fixture('using'));
  await createUsingClient(transport).apply('t1', 'permissions');
  assert.deepEqual(seen[0].request.body, { field: 'permissions' });
  assert.equal(seen[0].path, '/sessions/t1/using/apply');
});

test('a refusal arrives as the engine sentence', async () => {
  const refusal = fixture('using-error-not-a-candidate');
  const client = createUsingClient(async () => { throw new PlacesError(refusal.error, 422, refusal.code); });
  await assert.rejects(client.choose('t1', 'model', 'pl_x'), (e: unknown) => e instanceof PlacesError && e.code === 'not_a_candidate' && e.status === 422);
});

test('an answer that claims a place setting applies on an engine that reads no places is refused', async () => {
  const lying = fixture('using');
  lying.engine = { places: false, reason: 'opened in the terminal' };
  await assert.rejects(createUsingClient(async () => lying).using('t1'), PlacesError);
});

test('a conflict with a value, a missing reason or a count that disagrees is refused', async () => {
  const client = (mutate: (v: any) => void) => createUsingClient(async () => { const v = fixture('using'); mutate(v); return v; });
  await assert.rejects(client(v => { v.bundle.policy[0].value = 'place/flash'; }).using('t'), PlacesError);
  await assert.rejects(client(v => { delete v.settings[0].reason; }).using('t'), PlacesError);
  await assert.rejects(client(v => { v.bundle.counts.sources = 9; }).using('t'), PlacesError);
  await assert.rejects(client(v => { v.settings[0].state = 'enforced'; }).using('t'), PlacesError);
});
