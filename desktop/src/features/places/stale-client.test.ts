import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { PlacesError, type PlacesRequest, type PlacesTransport } from './client.ts';
import { createStaleClient } from './stale-client.ts';
import { staleActions, staleSuggestion, staleText } from './stale-model.ts';

// The fixtures are the REAL answers of the Go handlers (TestStaleWireFixtures), not hand-copied shapes.
const fixture = (name: string): any => JSON.parse(readFileSync(new URL(`./fixtures/${name}.json`, import.meta.url), 'utf8'));

function recording(answer: (path: string, request: PlacesRequest) => unknown) {
  const seen: { path: string; request: PlacesRequest }[] = [];
  const transport: PlacesTransport = async (path, request) => { seen.push({ path, request }); return structuredClone(answer(path, request)); };
  return { transport, seen };
}

test('the engine’s list parses and carries its own figures, not ours', async () => {
  const { transport, seen } = recording(() => fixture('stale'));
  const list = await createStaleClient(transport).list();
  assert.equal(seen[0].path, '/places/stale');
  assert.equal(seen[0].request.method, 'GET');
  assert.deepEqual([list.afterDays, list.snoozeDays], [60, 30]);
  assert.equal(list.places[0].name, 'Launch week');
  assert.equal(list.places[0].daysUntouched, 75);
});

test('an empty list is no places, not an error', async () => {
  const list = await createStaleClient(recording(() => fixture('stale-none')).transport).list();
  assert.deepEqual(list.places, []);
});

test('Not now is one POST to that place, and the answer says when the engine will offer it again', async () => {
  const { transport, seen } = recording(() => fixture('stale-snoozed'));
  const done = await createStaleClient(transport).snooze('pl_0000000000000001');
  assert.equal(seen[0].path, '/places/pl_0000000000000001/stale-snooze');
  assert.equal(seen[0].request.method, 'POST');
  assert.equal(done.until, '2027-01-22T12:00:00Z');
});

test('a place id is escaped into the path', async () => {
  const { transport, seen } = recording(() => ({ ok: true, placeId: 'a/b', until: '2027-01-01T00:00:00Z' }));
  await createStaleClient(transport).snooze('a/b');
  assert.equal(seen[0].path, '/places/a%2Fb/stale-snooze');
});

test('answers that are not the contract are refused, never guessed at', async () => {
  const list = (answer: unknown) => createStaleClient(async () => answer).list();
  await assert.rejects(list({}), PlacesError);
  await assert.rejects(list({ ...fixture('stale'), places: [{ id: 'x', name: 'x', touchedAt: 'y', daysUntouched: -1 }] }), /invalid untouched place/);
  await assert.rejects(list({ ...fixture('stale'), afterDays: '60' }), PlacesError);
  await assert.rejects(createStaleClient(async () => ({ ok: false })).snooze('p'), /invalid snooze answer/);
});

test('a refusal keeps the engine’s own sentence', async () => {
  const refusal = new PlacesError('That place doesn’t exist any more.', 404, 'not_found');
  await assert.rejects(createStaleClient(async () => { throw refusal; }).snooze('pl_gone'), /doesn’t exist any more/);
});

test('the sentence counts whole days, in the singular too', () => {
  assert.equal(staleText({ name: 'Launch week', daysUntouched: 75 }), '“Launch week” hasn’t been touched in 75 days');
  assert.equal(staleText({ name: 'A', daysUntouched: 1 }), '“A” hasn’t been touched in 1 day');
});

test('the verbs come in the design’s order and only the ones that are wired', () => {
  const none = () => undefined;
  assert.deepEqual(staleActions({ id: 'p' }, { snooze: none, archive: none, merge: none }).map(a => a.label), ['Merge', 'Archive', 'Not now']);
  assert.deepEqual(staleActions({ id: 'p' }, { archive: none }).map(a => a.label), ['Archive']);
});

test('a verb runs against the offered place', async () => {
  const calls: string[] = [];
  const line = staleSuggestion(fixture('stale').places, { merge: id => void calls.push(`merge ${id}`), archive: id => void calls.push(`archive ${id}`), snooze: id => void calls.push(`snooze ${id}`) })!;
  for (const action of line.actions) await action.onSelect();
  assert.deepEqual(calls, ['merge pl_0000000000000001', 'archive pl_0000000000000001', 'snooze pl_0000000000000001']);
});

test('no place due draws no line, and a line with nothing to tidy with is not drawn', () => {
  const none = () => undefined;
  assert.equal(staleSuggestion([], { merge: none, archive: none, snooze: none }), undefined);
  assert.equal(staleSuggestion(fixture('stale').places, { snooze: none }), undefined);
  assert.equal(staleSuggestion(fixture('stale').places, {}), undefined);
});
