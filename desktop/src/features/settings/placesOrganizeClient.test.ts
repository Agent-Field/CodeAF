import test from 'node:test';
import assert from 'node:assert/strict';
import { OrganizeError, createOrganizeClient, type OrganizeRequest } from './placesOrganizeClient.ts';

const idle = { running: false, progress: { run: '', stage: '', done: 0, total: 0 }, last: { summary: '', outcome: '' } };

test('start posts the mode, and a 409 reads the run already going', async () => {
  const calls: { path: string; request: OrganizeRequest }[] = [];
  const client = createOrganizeClient(async (path, request) => {
    calls.push({ path, request });
    if (request.method === 'POST') return { run: 'run-1', started: true };
    return { running: true, progress: { run: 'run-1', stage: 'vectors', done: 2, total: 8, cancellable: true } };
  });
  const started = await client.start('refresh');
  assert.equal(started.progress?.total, 8);
  assert.deepEqual(calls[0], { path: '/places/organize', request: { method: 'POST', body: { mode: 'refresh' } } });

  const again = createOrganizeClient(async (path, request) => {
    if (request.method === 'POST') throw new OrganizeError('running', 409);
    return { running: true, progress: { run: 'run-9', stage: 'name', done: 1, total: 2, cancellable: true } };
  });
  assert.equal((await again.start('add-on')).progress?.run, 'run-9');
});

test('cancel, accept, decline and undo use the organizing routes', async () => {
  const calls: { path: string; body?: unknown }[] = [];
  const client = createOrganizeClient(async (path, request) => {
    calls.push({ path, body: request.body });
    if (path.endsWith('/accept')) return { receipt: { id: 'rc_1' }, receipts: [{ id: 'rc_1' }] };
    if (request.method === 'GET') return idle;
    return { cancelled: true };
  });
  await client.cancel();
  assert.equal(await client.accept('plan/1', [0, 2]), 'rc_1');
  await client.decline('plan/1');
  await client.undo('rc_1');
  assert.deepEqual(calls.map(call => call.path), [
    '/places/organize/cancel',
    '/places/organize',
    '/places/proposals/plan%2F1/accept',
    '/places/proposals/plan%2F1/decline',
    '/places/undo',
  ]);
  assert.deepEqual(calls[2].body, { skip: [0, 2] });
  assert.deepEqual(calls[4].body, { receipts: ['rc_1'] });
});

test('a refusal that is not 409 still fails the start', async () => {
  const client = createOrganizeClient(async () => { throw new OrganizeError('no', 500); });
  await assert.rejects(() => client.start('add-on'), (error: OrganizeError) => error.status === 500);
});

test('the first-run offer is read from the status and Not now posts its own route', async () => {
  const calls: string[] = [];
  const client = createOrganizeClient(async (path, request) => {
    calls.push(`${request.method} ${path}`);
    return request.method === 'GET' ? { running: false, offer: { unplaced: 14 } } : { dismissed: true };
  });
  assert.deepEqual((await client.read()).offer, { unplaced: 14 });
  await client.dismissOffer();
  assert.deepEqual(calls, ['GET /places/organize', 'POST /places/organize/offer/dismiss']);
});

test('a status with no offer, or a nonsense one, offers nothing', async () => {
  for (const offer of [undefined, null, { unplaced: 0 }, { unplaced: 'many' }]) {
    const client = createOrganizeClient(async () => ({ running: false, offer }));
    assert.equal((await client.read()).offer, undefined);
  }
});
