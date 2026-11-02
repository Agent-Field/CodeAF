import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash, generateKeyPairSync } from 'node:crypto';
import { openWorkerd } from './workerd.js';

test('r6.2 r6.3 r6.4 retained hosted tables refuse former SQL atomically and remain readable', {timeout: 30000}, async t => {
  const out = await mkdtemp(join(tmpdir(), 'retained-worker-'));
  const persist = await mkdtemp(join(tmpdir(), 'retained-disk-'));
  const proof = process.env.CODEAF_RAFT_PROOF_DIR;
  const bundled = await build({entryPoints: ['test/raft/legacy-guard-worker.js'], bundle: true, format: 'esm', target: 'es2022', external: ['core.wasm', 'cloudflare:*'], write: false});
  await writeFile(join(out, 'worker.js'), bundled.outputFiles[0].contents);
  await copyFile(join(proof, 'core.wasm'), join(out, 'core.wasm'));
  const pub = () => generateKeyPairSync('ed25519').publicKey.export({format: 'der', type: 'spki'}).subarray(-32);
  const root = pub();
  const config = {cluster: 'retained-source', generation: '1', identity: 'id_' + createHash('sha256').update(root).digest('hex').slice(0, 32), signing_key: root.toString('base64'), effects_key: pub().toString('base64'), anchors: ['1', '2', '3'], now: 1000};
  const records = [
    {kind: 'identity', id: '', doc: {V: 1, identity: config.identity, vault: 'sealed-vault'}},
    {kind: 'devices', id: 'a', doc: {V: 1, name: 'c2VhbGVk', caps: {os: 'linux'}}},
    {kind: 'cells', id: 'chat', doc: {V: 1, head: 'saved', class: 'work', size: 7, keys: {}, lease: {device: 'a', fence: 1, expires: 0, pending: 0}, frames: ['sealed-frame']}},
  ];
  let server;
  const restart = async () => {if (server) await server.dispose(); server = await openWorkerd(out, persist, true);};
  const call = async (op, extra = {}) => {
    const response = await server.dispatchFetch(server.origin + '/source', {method: 'POST', body: JSON.stringify({op, config, ...extra}), signal: AbortSignal.timeout(5000)});
    assert.equal(response.status, 200);
    return response.json();
  };
  const ok = async (op, extra) => {const result = await call(op, extra); assert('ok' in result, JSON.stringify(result)); return result.ok;};
  const sql = (query, args) => ok('sql', {query, args});
  const original = async () => ({rows: await sql('SELECT kind,id,hex(doc) AS bytes FROM dir ORDER BY kind,id'), version: await sql('SELECT * FROM dirver')});
  try {
    await restart();
    await ok('seed', {records});
    await t.test('r6.3 rollback leaves all former statements usable and no guards', async () => {
      assert.equal(await ok('rollback-freeze'), 'interrupted before commit');
      assert.equal(await ok('gate'), null);
      assert.deepEqual(await sql("SELECT name FROM sqlite_master WHERE type='trigger' AND tbl_name IN ('dir','dirver')"), []);
      await sql('INSERT OR IGNORE INTO dirver VALUES (1, 0)');
      await sql('INSERT OR REPLACE INTO dir VALUES (?,?,?)', ['devices', 'a', JSON.stringify(records[1].doc)]);
      await sql('UPDATE dirver SET n = ?', [4]);
    });
    const before = await original();
    await ok('freeze');
    await restart();
    for (const [query, args] of [
      ['INSERT OR IGNORE INTO dirver VALUES (1, 0)'],
      ...records.map(record => ['INSERT OR REPLACE INTO dir VALUES (?,?,?)', [record.kind, record.id, JSON.stringify({...record.doc, vault: 'changed'})]]),
      ['INSERT OR REPLACE INTO dir VALUES (?,?,?)', ['cells', 'new', '{}']],
      ['UPDATE dirver SET n = ?', [99]],
      ["UPDATE dir SET doc = '{}'"],
      ['DELETE FROM dir'],
      ['DELETE FROM dirver'],
    ]) {
      await t.test('r6.2 ' + query + ' ' + (args?.[0] ?? ''), async () => {
        const result = await call('sql', {query, args});
        assert.match(result.error ?? '', /directory upgraded/);
        assert.deepEqual(await original(), before);
      });
    }
    await t.test('r6.3 committed freeze retains guards after process death', async () => {
      assert.equal((await ok('gate')).stage, 'frozen');
      assert.equal((await sql("SELECT name FROM sqlite_master WHERE type='trigger' AND tbl_name IN ('dir','dirver')")).length, 6);
    });
    await t.test('r6.4 guarded source inventories and resumes without changing retained rows', async () => {
      assert('error' in await call('sql', {query: 'UPDATE dirver SET n = ?', args: [99]}));
      const manifest = await ok('inventory');
      assert((await ok('rows')).length > 0);
      await restart();
      assert.deepEqual(await ok('inventory'), manifest);
      await ok('own', {receipts: [{node: '1', applied: '2', manifest}, {node: '2', applied: '2', manifest}]});
      await ok('freeze');
      assert.equal((await ok('gate')).stage, 'raft-owned');
      assert.deepEqual(await original(), before);
    });
  } finally {
    if (server) await server.dispose();
    await rm(out, {recursive: true, force: true});
    await rm(persist, {recursive: true, force: true});
  }
});
