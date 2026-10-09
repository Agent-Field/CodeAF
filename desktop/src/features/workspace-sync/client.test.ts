import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createWorkspaceClient, isWorkspaceKey, WorkspaceSyncError, type WorkspaceTransport } from './client.ts';
import { adoptOrphans, parsePersisted, persistKey, writePersisted } from './windowStore.ts';
import { compose, emptyLocal, sharedOf } from './shared.ts';
import { newTab } from '../tabs/helpers.ts';

// Written by the Go handler's own test (TestWorkspaceWireFixtures): these are the engine's real answers.
const fixture = (name: string): unknown => JSON.parse(readFileSync(new URL(`./fixtures/${name}.json`, import.meta.url), 'utf8'));
const answering = (status: number, body: unknown): WorkspaceTransport => async () => ({ status, body: structuredClone(body) });

test('the client reads every answer the engine really sends', async () => {
  const empty = await createWorkspaceClient(answering(200, fixture('empty'))).get('now');
  assert.deepEqual([empty.revision, empty.workspace], [0, undefined]);
  const saved = await createWorkspaceClient(answering(200, fixture('saved'))).get('now');
  assert.equal(saved.revision, 1);
  assert.deepEqual(saved.workspace?.tabs.map(t => t.id), ['a']);
  const conflict = await createWorkspaceClient(answering(409, fixture('conflict'))).put('now', 0, 'w', saved.workspace!);
  assert.equal(conflict.kind, 'conflict');
  assert.equal(conflict.kind === 'conflict' && conflict.current.writer, 'win-a');
  await assert.rejects(createWorkspaceClient(answering(400, fixture('invalid'))).put('now', 1, 'w', saved.workspace!), (e: WorkspaceSyncError) => e.code === 'invalid' && /could not be saved/.test(e.message) && !e.unreachable);
});

test('a malformed answer is an error, never an empty tab set', async () => {
  for (const body of [null, {}, { key: 'other', revision: 1 }, { key: 'now', revision: 2, workspace: null }, { key: 'now', revision: 1, workspace: { schema: 1, tabs: [], groups: [], closed: [] } }]) {
    await assert.rejects(createWorkspaceClient(answering(200, body)).get('now'), WorkspaceSyncError);
  }
});

test('only now and place-graph ids are keys', () => {
  assert.ok(isWorkspaceKey('now'));
  assert.ok(isWorkspaceKey('pl_0123456789abcdef'));
  for (const bad of ['root', '../now', 'pl_0123', 'pl_0123456789ABCDEF', '']) assert.equal(isWorkspaceKey(bad), false);
});

test('the shared document never carries a window\'s own focus', () => {
  const a = newTab({ id: 'a' }), b = newTab({ id: 'b' }), c = newTab({ id: 'c' });
  const state = { tabs: [a, { ...b, split: { layout: '1x2' as const, focus: 1, panes: [b, c] } }], groups: [], closed: [], activeId: 'b', nextNumber: 3, recentIds: ['b', 'a'] };
  const text = JSON.stringify(sharedOf(state));
  assert.doesNotMatch(text, /activeId|recentIds|"focus"/);
  const back = compose(sharedOf(state), { ...emptyLocal(), activeId: 'b', focus: { b: 1 } });
  assert.equal(back.activeId, 'b');
  assert.equal(back.tabs[1].split?.focus, 1);
});

test('a saved window copy is validated, and junk is ignored rather than trusted', () => {
  assert.equal(parsePersisted('{oops'), undefined);
  assert.equal(parsePersisted(JSON.stringify({ revision: -1, pending: [] })), undefined);
  assert.equal(parsePersisted(JSON.stringify({ revision: 1, base: { schema: 1, tabs: [] }, pending: [] })), undefined);
  const kept = parsePersisted(JSON.stringify({ revision: 3, pending: [{ action: { type: 'pin', id: 'a' }, ids: [], want: true }, { nonsense: 1 }], local: { activeId: 'a', scroll: { a: 12, b: -4 } } }));
  assert.equal(kept?.pending.length, 1);
  assert.equal(kept?.pending[0].want, true);
  assert.deepEqual(kept?.local.scroll, {}, 'a negative scroll invalidates the map, not the copy');
});

function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() { return map.size; }, key: (i: number) => [...map.keys()][i] ?? null, getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => { map.set(k, v); }, removeItem: (k: string) => { map.delete(k); }, clear: () => map.clear(),
  };
}

test('a gone window\'s unsaved changes are taken over; a live window\'s are left alone; nothing is removed before commit', async () => {
  const storage = memoryStorage();
  const entry = { action: { type: 'open' as const, tab: newTab({ id: 'x' }), background: true }, ids: [] };
  writePersisted(storage, 'now', 'gone', { revision: 1, pending: [entry], local: emptyLocal() });
  writePersisted(storage, 'now', 'alive', { revision: 1, pending: [entry], local: emptyLocal() });
  const heldByOthers = new Set(['codeaf-workspace-window:alive']);
  const locks = { request: async (name: string, _o: { ifAvailable?: boolean }, run: (lock: unknown) => unknown) => { await run(heldByOthers.has(name) ? null : {}); } };
  const taken = await adoptOrphans(storage, 'now', 'me', locks);
  assert.equal(taken.entries.length, 1);
  assert.ok(storage.getItem(persistKey('now', 'gone')), 'kept until this window has saved them as its own');
  taken.commit();
  assert.equal(storage.getItem(persistKey('now', 'gone')), null);
  assert.ok(storage.getItem(persistKey('now', 'alive')));
  const without = await adoptOrphans(storage, 'now', 'me', null);
  assert.equal(without.entries.length, 0, 'no Web Locks: nothing is taken over');
});
