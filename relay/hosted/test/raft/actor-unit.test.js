import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { createHash, generateKeyPairSync, sign } from 'node:crypto';
import { Reactor, HostCodec } from '../../src/raft/abi.js';
import { MemberStorage, b64 } from '../../src/raft/storage.js';
import { MemberActor } from '../../src/raft/actor.js';
import { MemberInbox } from '../../src/raft/inbox.js';

const reactor = new Reactor(new WebAssembly.Module(readFileSync((process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p08') + '/core.wasm')));
const codec = new HostCodec(reactor), empty = new Uint8Array();
const sha = b => createHash('sha256').update(b).digest('hex');
const text = new TextEncoder();
class Host {
  constructor() { this.db = new DatabaseSync(':memory:'); this.sql = { exec: (q, ...p) => { const rows = this.db.prepare(q).all(...p); return { toArray: () => rows, [Symbol.iterator]: () => rows[Symbol.iterator]() }; } }; this.gate = null; }
  transactionSync(fn) { this.db.exec('BEGIN'); try { const v = fn(); this.db.exec('COMMIT'); return v; } catch (e) { this.db.exec('ROLLBACK'); throw e; } }
  async sync() { if (this.gate) await this.gate; }
}
const identity = id => ({ cluster: 'unit', id: String(id), protocol: 1 });
const conf = Uint8Array.of(8,1,8,2,8,3);
const entry = (index, data = Uint8Array.of(1)) => Uint8Array.of(8,0,16,1,24,index,34,data.length,...data);
const hard = commit => Uint8Array.of(8,1,16,0,24,commit);
async function seeded(host, id = 1) {
  const s = new MemberStorage(host, codec); await s.open(identity(id));
  const entries = [entry(1)]; await s.persist({ hard: hard(1), entries, snapshot: empty });
  const writes = ['state','sessions','membership','enrollment','tombstones','outbox'].map((namespace, i) => ({ namespace, key: 'opaque-' + i, data: new Uint8Array(i === 0 ? 600000 : 4096).fill(i + 1) }));
  await s.apply({ index: 1n, conf, entries, snapshot: empty }, () => ({ writes, version: '8', now: '1000' })); return s;
}

test('R3 R27 serial actor waits for durable host completion and bounds stalled admission', async () => {
  const host = new Host(), actor = new MemberActor(host, reactor);
  let release; host.gate = new Promise(resolve => { release = resolve; });
  let completed = false;
  const first = actor.init({ cluster: 'queue', id: '1', seed: '101', bootstrap: ['1','2','3'] }).then(v => { completed = true; return v; });
  await new Promise(resolve => setImmediate(resolve)); assert.equal(completed, false);
  const tick = actor.input(1); const queued = [];
  for (let i = 0; i < 62; i++) queued.push(actor.serial(() => i));
  await assert.rejects(actor.serial(() => 100), /capacity/);
  release(); await first; await tick; assert.deepEqual(await Promise.all(queued), Array.from({ length: 62 }, (_, i) => i));
  assert.equal(actor.status.applied, 3n); await actor.close(); host.db.close();
});

test('R12 R13 R17 R28 portable host snapshots retain every namespace, incomplete stages and both generations', async () => {
  const sourceHost = new Host(), destHost = new Host();
  const source = await seeded(sourceHost), snapshot = await source.checkpoint(), info = codec.inspect(3, snapshot), manifest = JSON.parse(new TextDecoder().decode(info.data)), id = sha(info.data);
  assert(BigInt(manifest.chunks) >= 3n); assert.equal(manifest.records, '6');
  let dest = new MemberStorage(destHost, codec); await dest.open(identity(2)); await dest.beginSnapshot(info.data);
  const chunk = source.chunk(id, 0); await dest.putChunk(id, 0, chunk, sha(chunk));
  await assert.rejects(dest.completeSnapshot(id), /invalid|damaged/);
  // Reopening discards no authoritative state and does not adopt the partial
  // generation. All namespace bytes remain at the source throughout.
  dest = new MemberStorage(destHost, codec); const recovered = await dest.open(identity(2)); assert.equal(recovered.applied, 0n);
  for (let n = 1; n < Number(manifest.chunks); n++) { const b = source.chunk(id, n); await dest.putChunk(id, n, b, sha(b)); }
  assert.deepEqual(await dest.completeSnapshot(id), snapshot);
  await dest.persist({ hard: hard(1), entries: [], snapshot });
  for (const r of source.recordRows()) assert.deepEqual(dest.read(r.namespace, r.key), r.data);
  assert.equal(dest.application().version, '8'); assert.equal(dest.application().now, '1000');
  assert.deepEqual(dest.recover().conf, info.conf);
  assert.deepEqual(await source.checkpoint(), snapshot, 'repeated checkpoint is idempotent');
  for (let index = 2; index <= 3; index++) {
    const entries = [entry(index)]; await source.persist({ hard: hard(index), entries, snapshot: empty });
    await source.apply({ index: BigInt(index), conf, entries, snapshot: empty }, () => ({ writes: [], version: String(index + 8) })); await source.checkpoint();
  }
  assert.equal(source.one('SELECT count(*) AS n FROM snapshots WHERE complete=1').n, 2);
  assert.equal(source.one('SELECT count(*) AS n FROM records').n, 6);
  assert.equal(source.one('SELECT count(*) AS n FROM journal').n, 0);
  for (const r of source.rows('SELECT id FROM snapshots')) source.walk(r.id);
  sourceHost.db.close(); destHost.db.close();
});

test('R3 R13 hosted transaction failure and corrupt complete bytes refuse participation without resetting records', async () => {
  const host = new Host(), source = await seeded(host), snapshot = await source.checkpoint(), info = codec.inspect(3, snapshot), id = sha(info.data);
  host.db.prepare('UPDATE chunks SET hash=? WHERE generation=? AND offset=0').run('damaged', id);
  const reopened = new MemberStorage(host, codec);
  await assert.rejects(reopened.open(identity(1)), /damaged/);
  assert.equal(host.db.prepare('SELECT count(*) AS n FROM records').get().n, 6);
  assert.equal(host.db.prepare('SELECT count(*) AS n FROM snapshots').get().n, 1);
  host.db.close();
  const failingHost = new Host(), actor = new MemberActor(failingHost, reactor);
  failingHost.db.exec("CREATE TABLE evidence (body TEXT)");
  await assert.rejects(actor.init({ cluster: 'refuse', id: '1', seed: '1', bootstrap: ['1','2','3'] }), /damaged/);
  assert.equal(failingHost.db.prepare('SELECT count(*) AS n FROM evidence').get().n, 0);
  failingHost.db.close();
});

test('R14 R16 hosted inbox accepts native wire and rechecks revocation after queued signature admission', async () => {
  const f = JSON.parse(readFileSync(new URL('../../../../internal/rafttransport/testdata/exchange-v1.json', import.meta.url), 'utf8'));
  const e = f.request.messages[0], scope = e.scope; let view = { 1: { incarnation: e.incarnation, key: f.public_key, voter: true }, 2: { incarnation: '1', key: f.public_key, voter: true } };
  const queue = [], inbox = new MemberInbox(scope, '2', codec, () => view, fn => new Promise((resolve, reject) => queue.push(() => Promise.resolve().then(fn).then(resolve, reject))));
  let dispatched = 0; const first = inbox.receive(text.encode(JSON.stringify(e)), () => { dispatched++; });
  while (queue.length === 0) await new Promise(resolve => setImmediate(resolve));
  view = { ...view, 1: { ...view[1], revoked: true } }; queue.shift()();
  await assert.rejects(first, /authorization/); assert.equal(dispatched, 0);
  view = { ...view, 1: { ...view[1], revoked: false } };
  const accepted = inbox.receive(text.encode(JSON.stringify(e)), () => { dispatched++; });
  while (queue.length === 0) await new Promise(resolve => setImmediate(resolve)); queue.shift()(); await accepted; assert.equal(dispatched, 1);
  await assert.rejects(inbox.receive(text.encode(JSON.stringify({ ...e, to: '3' })), () => {}), /authorization/);
});

test('R27 R28 hosted quotas refuse growth and excessive evaluation sets without deleting retained rows', async () => {
  const host = new Host(), s = await seeded(host);
  const entries = [entry(2)]; await s.persist({ hard: hard(2), entries, snapshot: empty });
  await assert.rejects(s.apply({ index: 2n, entries, conf, snapshot: empty }, (_, read) => { read('state', 'opaque-0'); read('state', 'opaque-0'); return { writes: [] }; }), /read-set capacity/);
  assert.equal(s.one('SELECT count(*) AS n FROM records').n, 6); assert.equal(s.application().applied, '1'); host.db.close();
  const growingHost = new Host(), growing = await seeded(growingHost); let refused = false;
  for (let index = 2; index < 16; index++) {
    const entries = [entry(index)]; await growing.persist({ hard: hard(index), entries, snapshot: empty });
    const before = growing.one('SELECT count(*) AS n FROM records').n;
    try { await growing.apply({ index: BigInt(index), entries, conf, snapshot: empty }, () => ({ writes: [{ namespace: 'sessions', key: `retained-${index}`, data: new Uint8Array(900000).fill(index) }] })); }
    catch (e) { assert.match(e.message, /application capacity/); assert.equal(growing.one('SELECT count(*) AS n FROM records').n, before); refused = true; break; }
  }
  assert(refused); assert(growing.application().size <= 8 * 1024 * 1024);
  assert.equal(growing.read('state', 'opaque-0').length, 600000); growingHost.db.close();
});

test('R11 R16 R27 hosted inbox reservations preserve voter delivery when a learner stalls', async () => {
  const keys = generateKeyPairSync('ed25519'), key = keys.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64');
  const scope = { identity: 'bounded', cluster: 'bounded', epoch: '1', protocol: 1 };
  const view = { 1: { incarnation: '1', key, voter: true }, 2: { incarnation: '1', key, voter: true }, 3: { incarnation: '1', key, voter: false } };
  const queue = [], inbox = new MemberInbox(scope, '2', codec, () => view, fn => new Promise((resolve, reject) => queue.push(() => Promise.resolve().then(fn).then(resolve, reject))));
  const packet = (from, sequence) => {
    const payload = Uint8Array.of(8,8,16,2,24,from,32,1);
    const e = { scope, from: String(from), to: '2', incarnation: '1', destination: '1', sequence: String(sequence), chunk: null, digest: sha(payload), payload: b64(payload), signature: null };
    e.signature = sign(null, text.encode('codeaf/raft/envelope/1\n' + JSON.stringify(e)), keys.privateKey).toString('base64'); return text.encode(JSON.stringify(e));
  };
  const learner = [inbox.receive(packet(3, 1), () => 'learner'), inbox.receive(packet(3, 2), () => 'learner')];
  while (queue.length < 2) await new Promise(resolve => setImmediate(resolve));
  await assert.rejects(inbox.receive(packet(3, 3), () => {}), /capacity/);
  const voter = inbox.receive(packet(1, 1), () => 'voter');
  while (queue.length < 3) await new Promise(resolve => setImmediate(resolve));
  queue.pop()(); assert.equal(await voter, 'voter');
  for (const deliver of queue) deliver(); await Promise.all(learner);
  assert.deepEqual(inbox.counts, [0,0,0]);
});

test('R27 isolate activation refuses before allocating a new member store and retains all active followers', async () => {
  const actors = [], hosts = []; let refused = false;
  try {
    for (let i = 0; i < 16; i++) {
      const host = new Host(), actor = new MemberActor(host, reactor); hosts.push(host);
      try { await actor.init({ cluster: `capacity-${i}`, id: '1', seed: '101', bootstrap: ['1','2','3'] }); actors.push(actor); }
      catch (e) { assert.match(e.message, /isolate capacity/); assert.equal(host.db.prepare("SELECT count(*) AS n FROM sqlite_master WHERE name='meta'").get().n, 0); refused = true; break; }
    }
    assert(refused); assert(actors.length >= 3);
    for (const actor of actors) assert.equal((await actor.input(0)).status.applied, 3n);
  } finally { for (const actor of actors) await actor.close(); for (const host of hosts) host.db.close(); }
});

// R3/R27 distinguish the local platform's name index from unknown retained
// application data. An explorer-created table cannot block first activation,
// and it never licenses resetting an unrelated authoritative table.
test('R3 R27 a fresh hosted store accepts platform name metadata and refuses unknown tables',async()=>{
 const host=new Host();host.db.exec('CREATE TABLE __miniflare_do_name(name TEXT); INSERT INTO __miniflare_do_name VALUES(\'anchor-a\')');
 try{
  const store=new MemberStorage(host,codec);const recovered=await store.open(identity(1));assert.equal(recovered.applied,0n);
  assert.equal(host.db.prepare('SELECT name FROM __miniflare_do_name').get().name,'anchor-a');
  assert.equal(store.application().count,0);
 }finally{host.db.close();}
 const unknown=new Host();unknown.db.exec('CREATE TABLE evidence(body TEXT);INSERT INTO evidence VALUES(\'retained\')');
 try{await assert.rejects(new MemberStorage(unknown,codec).open(identity(2)),/invalid|damaged/);assert.equal(unknown.db.prepare('SELECT body FROM evidence').get().body,'retained');}finally{unknown.db.close();}
});

// R25/R27 keep temporary body pages separate from retained capacity through
// snapshots, while unrelated growth still refuses without deleting records.
test('R25 R27 staged legacy requests preserve retained capacity across snapshots', async () => {
  const host=new Host(),source=await seeded(host);let index=1;
  const apply=async(store,writes)=>{
    index++;const entries=[entry(index)];
    await store.persist({hard:hard(index),entries,snapshot:empty});
    await store.apply({index:BigInt(index),entries,conf,snapshot:empty},()=>({writes}));
  };
  while(source.application().size<source.identity.ceiling){
    const name=`retained-${index}`,cost=12+text.encode('state'+name).length;
    const length=Math.min(900000,source.identity.ceiling-source.application().size-cost);
    assert(length>0);await apply(source,[{namespace:'state',key:name,data:new Uint8Array(length).fill(19)}]);
  }
  const retained=source.application().size;
  const policy=codec.stagingPolicy(),keys=[...policy.keys.keys()].map(pair=>pair.split('\0'));
  const staged=keys.slice(0,2).map(([namespace,key],n)=>({namespace,key,data:n?new Uint8Array(32768).fill(31):text.encode('{"id":"staged","next":32768}')}));
  await apply(source,staged);
  assert(source.application().size>retained);assert.equal(source.stagingBytes(),source.application().size-retained);
  const snapshot=await source.checkpoint(),info=codec.inspect(3,snapshot),manifest=JSON.parse(new TextDecoder().decode(info.data)),id=sha(info.data);
  const destHost=new Host(),dest=new MemberStorage(destHost,codec);await dest.open(identity(2));await dest.beginSnapshot(info.data);
  for(let n=0;n<Number(manifest.chunks);n++){const data=source.chunk(id,n);await dest.putChunk(id,n,data,sha(data));}
  assert.deepEqual(await dest.completeSnapshot(id),snapshot);
  await dest.persist({hard:hard(index),entries:[],snapshot});assert.equal(dest.recover().applied,BigInt(index));
  assert.equal(dest.application().size-dest.stagingBytes(),retained);
  const original=dest.read('state','opaque-0');
  await apply(dest,[{namespace:'state',key:'opaque-0',data:original},...staged.map(row=>({...row,data:empty,delete:true}))]);
  assert.equal(dest.stagingBytes(),0);assert.equal(dest.application().size,retained);assert.deepEqual(dest.read('state','opaque-0'),original);
  const before=dest.application().applied;
  await assert.rejects(apply(dest,[{namespace:'state',key:'extra',data:Uint8Array.of(1)}]),/application capacity/);
  assert.equal(dest.application().applied,before);assert.deepEqual(dest.read('state','opaque-0'),original);
  host.db.close();destHost.db.close();
});
