import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createDecisionsClient, createPlanClient } from './client.ts';
import { createKnowsClient } from '../places/knows/client.ts';
import { createCouncilClient } from '../council/client.ts';
import { PlacesError, placesTransport, type PlacesRequest, type PlacesTransport } from '../places/client.ts';

const fixture = (name: string): any => JSON.parse(readFileSync(new URL(`./fixtures/${name}.json`, import.meta.url), 'utf8'));
function recording(answer: unknown) {
  const seen: { path: string; request: PlacesRequest }[] = [];
  const transport: PlacesTransport = async (path, request) => { seen.push({ path, request }); return structuredClone(answer); };
  return { transport, seen };
}

test('all plan routes use their Go goldens and canonical encoded ids', async () => {
  for (const verb of ['go', 'edit', 'cancel'] as const) {
    const { transport, seen } = recording(fixture(`plan-${verb}`));
    const client = createPlanClient(transport);
    const steps = fixture('plan-go').results.map((r: any) => r.step);
    const signal = new AbortController().signal;
    const result = verb === 'edit' ? await client.edit('chat /?', 'plan /?', steps, signal) : await client[verb]('chat /?', 'plan /?', signal);
    assert.deepEqual(result, fixture(`plan-${verb}`));
    assert.deepEqual(seen, [{ path: `/sessions/chat%20%2F%3F/plan/plan%20%2F%3F/${verb}`, request: { method: 'POST', body: verb === 'edit' ? { steps } : {}, signal } }]);
  }
});

test('knows CRUD, confirmation and same-day conflict carry Go evidence and guards', async () => {
  for (const name of ['knows-list', 'knows-empty']) {
    const { transport, seen } = recording(fixture(name));
    assert.deepEqual(await createKnowsClient(transport).list('p /'), fixture(name));
    assert.equal(seen[0].path, '/places/p%20%2F/knows');
  }
  for (const verb of ['add', 'edit', 'remove', 'confirm'] as const) {
    const { transport, seen } = recording(fixture(`knows-${verb}`));
    const client = createKnowsClient(transport);
    const signal = new AbortController().signal;
    const write = { text: 'Run tests', supersedes: 'old', ifRevision: 7 };
    const result = verb === 'add' ? await client.add('p /', write, signal)
      : verb === 'edit' ? await client.edit('p /', 'k /', write, signal)
      : await client[verb]('p /', 'k /', 7, signal);
    assert.deepEqual(result, fixture(`knows-${verb}`));
    assert.deepEqual(seen, [{ path: `/places/p%20%2F/knows${verb === 'add' ? '' : '/k%20%2F'}${verb === 'confirm' ? '/still-true' : ''}`, request: {
      method: verb === 'edit' ? 'PATCH' : verb === 'remove' ? 'DELETE' : 'POST',
      body: verb === 'add' || verb === 'edit' ? write : verb === 'confirm' ? { yes: true, ifRevision: 7 } : { ifRevision: 7 }, signal,
    } }]);
  }
  const result = await createKnowsClient(recording(fixture('knows-conflict')).transport).add('p', { text: 'Skip tests', supersedes: 'old' });
  assert.deepEqual(result.ask, fixture('knows-conflict').ask);
});

test('council filters, steering and absent optional data match Go wire types', async () => {
  const { transport, seen } = recording({ councils: [fixture('council')] });
  const signal = new AbortController().signal;
  assert.deepEqual(await createCouncilClient(transport).list('p &/', signal), { councils: [fixture('council')] });
  assert.deepEqual(seen, [{ path: '/councils?place=p%20%26%2F', request: { method: 'GET', signal } }]);
  assert.deepEqual(await createCouncilClient(recording(fixture('councils-empty')).transport).list(), { councils: [] });
  const steer = recording(fixture('council'));
  assert.deepEqual(await createCouncilClient(steer.transport).steer('c /', 'Wait', signal), fixture('council'));
  assert.deepEqual(steer.seen, [{ path: '/councils/c%20%2F/steer', request: { method: 'POST', body: { text: 'Wait' }, signal } }]);
  assert.equal('closedAt' in fixture('council'), false);
});

test('all five reserved decision routes make one request without inventing response fields', async () => {
  const { transport, seen } = recording({});
  const client = createDecisionsClient(transport);
  await client.list('p /'); await client.status('p /'); await client.get('d /');
  await client.overturn('d /', { reason: 'Wait' }); await client.setDecide('p /', { alwaysAsk: true, threshold: 90 });
  assert.deepEqual(seen.map(r => [r.path, r.request.method]), [
    ['/places/p%20%2F/decisions', 'GET'], ['/places/p%20%2F/decide-status', 'GET'], ['/decisions/d%20%2F', 'GET'],
    ['/decisions/d%20%2F/overturn', 'POST'], ['/places/p%20%2F/decide', 'PUT'],
  ]);
});

test('default authenticated transport preserves real 409 and 501 sentences without retry', async () => {
  const original = globalThis.fetch;
  try {
    for (const [name, status] of [['stale', 409], ['plan-refusal', 409], ['council-refusal', 409], ['decisions-unavailable', 501]] as const) {
      let calls = 0;
      const body = fixture(name);
      globalThis.fetch = async () => { calls++; return new Response(JSON.stringify(body), { status }); };
      await assert.rejects(placesTransport('/councils/c1/steer', { method: 'POST', body: {} }), (error: unknown) => {
        assert.ok(error instanceof PlacesError);
        assert.equal(error.message, body.error); assert.equal(error.status, status); assert.equal(error.code, body.code ?? '');
        return true;
      });
      assert.equal(calls, 1);
    }
  } finally { globalThis.fetch = original; }
});

test('each wrapper propagates the same conflict object and never writes again', async () => {
  const error = new PlacesError(fixture('stale').error, 409, 'stale');
  let calls = 0;
  const transport: PlacesTransport = async () => { calls++; throw error; };
  const operations = [
    () => createKnowsClient(transport).edit('p', 'k', { text: 'x', ifRevision: 1 }),
    () => createPlanClient(transport).go('s', 'p'),
    () => createCouncilClient(transport).steer('c', 'x'),
    () => createDecisionsClient(transport).overturn('d', {}),
  ];
  for (const op of operations) await assert.rejects(op(), e => e === error);
  assert.equal(calls, operations.length);
});

test('malformed success payloads cannot become invented facts or acknowledgements', async () => {
  const operations = [
    () => createKnowsClient(recording({ revision: 1, lines: [{ id: 'k', placeId: 'p', text: 'x', source: { kind: 'learned', answers: '3' } }], stillTrue: [] }).transport).list('p'),
    () => createKnowsClient(recording({ ...fixture('knows-conflict'), ask: { a: {} } }).transport).add('p', { text: 'x' }),
    () => createCouncilClient(recording({ ...fixture('council'), places: ['p'] }).transport).steer('c', 'x'),
    () => createPlanClient(recording({ ...fixture('plan-go'), results: [{ step: {}, status: 'done', line: 'x' }] }).transport).go('s', 'p'),
    () => createPlanClient(recording({ accepted: false }).transport).edit('s', 'p', []),
    () => createPlanClient(recording({ cancelled: false }).transport).cancel('s', 'p'),
  ];
  for (const op of operations) await assert.rejects(op(), PlacesError);
});
