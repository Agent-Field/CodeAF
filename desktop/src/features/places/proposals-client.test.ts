import test from 'node:test';
import assert from 'node:assert/strict';
import { createProposalsClient, type PlaceProposal } from './proposals-client.ts';
import type { PlacesRequest } from './client.ts';
const proposal: PlaceProposal = { id: 'prop_0123456789abcdef', kind: 'file', status: 'pending', chatIds: ['chat'], placeId: 'pl_0123456789abcdef', reason: 'Same folder as Reading', offerVersion: 'version' };
test('reading and declining offers send no model command; accepting echoes the exact offer version and preserves receipt order', async () => {
  const calls: { path: string; request: PlacesRequest }[] = [];
  const receipts = [1, 2].map(n => ({ id: `receipt-${n}`, action: 'member.add', beforeRevision: n, afterRevision: n + 1, at: '' }));
  const client = createProposalsClient(async (path, request) => { calls.push({ path, request }); return path.endsWith('/accept') ? { receipts, view: { proposals: [], organizing: false } } : { proposals: [proposal], organizing: false }; });
  assert.equal((await client.read()).proposals[0].id, proposal.id);
  await client.decline(proposal.id);
  const mutation = await client.accept(proposal);
  assert.deepEqual(mutation.undo, ['receipt-1', 'receipt-2']);
  assert.equal(mutation.revision, 3);
  assert.deepEqual(calls.map(call => [call.path, call.request.method, call.request.body]), [
    ['/places/proposals', 'GET', undefined], ['/places/proposals/prop_0123456789abcdef/decline', 'POST', {}],
    ['/places/proposals/prop_0123456789abcdef/accept', 'POST', { offerVersion: 'version' }],
  ]);
});
test('invalid pending offers and invalid acceptance receipts cannot reach a UI action or Undo', async () => {
  await assert.rejects(createProposalsClient(async () => ({ proposals: [{ ...proposal, chatIds: [42] }], organizing: false })).read(), /invalid place offer/);
  await assert.rejects(createProposalsClient(async () => ({ receipts: [{ id: '' }], view: { proposals: [], organizing: false } })).accept(proposal), /invalid receipt/);
});
