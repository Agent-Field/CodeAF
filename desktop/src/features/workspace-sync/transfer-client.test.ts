import test from 'node:test';
import assert from 'node:assert/strict';
import { createWorkspaceClient, type WorkspaceTransferRequest } from './client.ts';
const place = 'pl_0123456789abcdef';
const workspace = { schema: 1 as const, tabs: [{ id: 'home', kind: 'home', title: 'Home', draft: '', pinned: true }], closed: [], groups: [], nextNumber: 2 };
const request: WorkspaceTransferRequest = { intent: 'intent-1', writer: 'win-a', destination: place, sourceRevision: 2, destinationRevision: 3, sourceWorkspace: workspace, destinationWorkspace: workspace };
const source = { key: 'now', revision: 3, workspace, movedTo: { moved: place } }, destination = { key: place, revision: 4, workspace };
test('pair acknowledgment validates both canonical keys and echoes immutable intent; conflicts carry both records', async () => {
  const calls: unknown[] = [];
  const client = createWorkspaceClient(async (path, ask) => { calls.push([path, ask]); return { status: 200, body: { intent: request.intent, source, destination, already: true } }; });
  const saved = await client.transfer('now', request);
  assert.equal(saved.kind, 'saved'); assert.deepEqual(saved.source.movedTo, { moved: place });
  assert.deepEqual(calls, [['/workspaces/now/transfer', { method: 'POST', body: request, signal: undefined }]]);
  const conflict = await createWorkspaceClient(async () => ({ status: 409, body: { code: 'conflict', source, destination } })).transfer('now', request);
  assert.equal(conflict.kind, 'conflict');
  await assert.rejects(createWorkspaceClient(async () => ({ status: 200, body: { intent: 'different', source, destination, already: false } })).transfer('now', request), /invalid tab transfer/);
  await assert.rejects(createWorkspaceClient(async () => ({ status: 200, body: { intent: request.intent, source, destination: source, already: false } })).transfer('now', request), /invalid tab set/);
  await assert.rejects(createWorkspaceClient(async () => ({ status: 409, body: { code: 'intent_changed', error: 'Intent was already committed with different tabs.' } })).transfer('now', request), /different tabs/);
});
