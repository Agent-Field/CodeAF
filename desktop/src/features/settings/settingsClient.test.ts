import test, { type TestContext } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { EngineError } from '../chat/engine-client.ts';
import { keyStatus, engineFacts, permissions, setPermissions } from './settingsClient.ts';

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function stub(t: TestContext, body: unknown, status = 200) {
 const calls: { url: string; init?: RequestInit }[] = [];
 t.mock.method(globalThis, 'fetch', async (url: string, init?: RequestInit) => {
  calls.push({ url, init });
  return json(body, status);
 });
 return calls;
}

test('key status reads only presence and the three safe source labels', async t => {
 const calls = stub(t, { present: true, source: 'profile', key: 'secret-canary', apiKey: 'secret-canary' });
 assert.deepEqual(await keyStatus(), { present: true, source: 'profile' });
 assert.equal(calls[0].url, '/api/engine/settings/key');
 assert.equal(calls[0].init?.cache, 'no-store');
 for (const source of ['OPENROUTER_API_KEY', 'OPENAI_API_KEY', 'profile']) {
  t.mock.method(globalThis, 'fetch', async () => json({ present: true, source }));
  assert.deepEqual(await keyStatus(), { present: true, source });
 }
});

test('a missing key and an unknown source stay absent', async t => {
 stub(t, { present: false });
 assert.deepEqual(await keyStatus(), { present: false });
 t.mock.method(globalThis, 'fetch', async () => json({ present: true }));
 assert.deepEqual(await keyStatus(), { present: true });
});

test('key status rejects malformed answers and cannot echo a secret source', async t => {
 for (const body of [null, [], {}, { present: 'yes' }, { present: true, source: 'secret-canary' }, { present: false, source: 'profile' }]) {
  stub(t, body);
  await assert.rejects(keyStatus(), e => e instanceof EngineError && e.message === 'The engine returned invalid key status.');
 }
});

test('the key status type has only presence and a closed source vocabulary', () => {
 const source = ts.createSourceFile('settingsClient.ts', readFileSync(new URL('./settingsClient.ts', import.meta.url), 'utf8'), ts.ScriptTarget.Latest, true);
 const alias = (name: string) => source.statements.find((s): s is ts.TypeAliasDeclaration => ts.isTypeAliasDeclaration(s) && s.name.text === name)!;
 const status = alias('KeyStatus').type;
 assert.ok(ts.isTypeLiteralNode(status));
 assert.deepEqual(status.members.map(member => member.name?.getText(source)), ['present', 'source']);
 assert.deepEqual(status.members.map(member => {
  assert.ok(ts.isPropertySignature(member));
  return [member.type?.getText(source), !!member.questionToken];
 }), [['boolean', false], ['KeySource', true]]);
 const vocabulary = alias('KeySource').type;
 assert.ok(ts.isUnionTypeNode(vocabulary));
 assert.deepEqual(vocabulary.types.map(type => {
  assert.ok(ts.isLiteralTypeNode(type) && ts.isStringLiteral(type.literal));
  return type.literal.text;
 }), ['OPENROUTER_API_KEY', 'OPENAI_API_KEY', 'profile']);
});

test('engine facts read local and forwarded connections without inventing missing facts', async t => {
 for (const local of [true, false]) {
  const body = { local, connection: local ? 'local' : 'forwarded', model: 'provider/model', version: 'dev' };
  const calls = stub(t, body);
  assert.deepEqual(await engineFacts(), body);
  assert.equal(calls[0].url, '/api/engine/settings/engine');
 }
 stub(t, { local: true, connection: 'local' });
 assert.deepEqual(await engineFacts(), { local: true, connection: 'local' });
});

test('invalid engine facts are rejected', async t => {
 for (const body of [null, {}, { local: true, connection: 'forwarded' }, { local: false, connection: 'remote' }, { local: true, connection: 'local', model: 5 }, { local: true, connection: 'local', version: null }]) {
  stub(t, body);
  await assert.rejects(engineFacts(), /invalid engine facts/);
 }
});

test('permissions read the engine registry and PUT exactly the selected mode', async t => {
 const body = { mode: 'ask', modes: ['ask', 'allow', 'deny'] };
 const calls = stub(t, body);
 assert.deepEqual(await permissions(), body);
 assert.equal(calls[0].url, '/api/engine/settings/permissions');
 const writes = stub(t, { ...body, mode: 'deny' });
 assert.deepEqual(await setPermissions('deny'), { ...body, mode: 'deny' });
 assert.equal(writes.length, 1);
 assert.equal(writes[0].url, '/api/engine/settings/permissions');
 assert.equal(writes[0].init?.method, 'PUT');
 assert.deepEqual(JSON.parse(String(writes[0].init?.body)), { mode: 'deny' });
 assert.equal(new Headers(writes[0].init?.headers).get('Content-Type'), 'application/json');
});

test('permissions preserve future engine modes and reject invalid reads or write acknowledgements', async t => {
 stub(t, { mode: 'future', modes: ['future'] });
 assert.deepEqual(await permissions(), { mode: 'future', modes: ['future'] });
 for (const body of [null, {}, { mode: 'ask', modes: [] }, { mode: 'ask', modes: ['ask', 1] }, { mode: '', modes: [''] }, { accepted: true }]) {
  stub(t, body);
  await assert.rejects(permissions(), /invalid permissions/);
  await assert.rejects(setPermissions('ask'), /invalid permissions/);
 }
});

test('all settings methods carry engine refusals and network failures through the shared transport', async t => {
 for (const call of [keyStatus, engineFacts, permissions, () => setPermissions('unknown')]) {
  stub(t, { error: 'unknown tool approval mode' }, 400);
  await assert.rejects(call(), e => e instanceof EngineError && e.status === 400 && e.message === 'unknown tool approval mode');
  t.mock.method(globalThis, 'fetch', async () => { throw new TypeError('offline'); });
  await assert.rejects(call(), e => e instanceof EngineError && e.unreachable);
 }
});
