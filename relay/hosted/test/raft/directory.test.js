import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import { DatabaseSync } from 'node:sqlite';
import { createHash, generateKeyPairSync, sign } from 'node:crypto';
import { Reactor } from '../../src/raft/abi.js';
import { encodeCanonical, decodeCanonical } from '../../src/raft/codec.js';
import { MemberActor } from '../../src/raft/actor.js';
import { RaftEffectsFacade } from '../../src/raft/effects.js';
import { R2Store, sealFrame } from '../../src/store.js';
import { boundLink, deliveredLink } from '../../src/link/effects.js';
import { encode as encodeFrame } from '../../src/frame.js';
import { directoryEvaluator, RaftDirectoryFacade } from '../../src/raft/directory.js';

const out = process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p09';
const enc = new TextEncoder(), dec = new TextDecoder();
const bytes = value => enc.encode(encodeCanonical(value));
const sha = value => createHash('sha256').update(value).digest('hex');
const segment = value => Buffer.from(value).toString('hex');
class Host {
  constructor() { this.db = new DatabaseSync(':memory:'); this.sql = { exec: (q, ...p) => { const rows = this.db.prepare(q).all(...p); return { toArray: () => rows, [Symbol.iterator]: () => rows[Symbol.iterator]() }; } }; }
  transactionSync(fn) { this.db.exec('BEGIN'); try { const result = fn(); this.db.exec('COMMIT'); return result; } catch (e) { this.db.exec('ROLLBACK'); throw e; } }
  async sync() {}
}
class Native {
  constructor() {
    this.child = spawn(out + '/native-peer', [], { stdio: ['pipe','pipe','inherit'], env: { ...process.env, GOMEMLIMIT: '32MiB' } }); this.buffer = Buffer.alloc(0);
    this.child.stdout.on('data', b => { this.buffer = Buffer.concat([this.buffer, b]); this.pump(); });
    this.child.on('exit', code => { if (this.waiter) { clearTimeout(this.waiter.timer); this.waiter.reject(new Error(`native exited ${code}`)); } });
  }
  pump() { if (!this.waiter || this.buffer.length < 4) return; const n = this.buffer.readUInt32LE(0); assert(n <= 2 * 1024 * 1024); if (this.buffer.length < n + 4) return; const b = this.buffer.subarray(4, n + 4); this.buffer = this.buffer.subarray(n + 4); const w = this.waiter; this.waiter = null; clearTimeout(w.timer); w.resolve(new Uint8Array(b)); }
  call(b) { assert(!this.waiter); return new Promise((resolve, reject) => { this.waiter = { resolve, reject, timer: setTimeout(() => reject(new Error('native ABI timeout')), 5000) }; const h = Buffer.alloc(4); h.writeUInt32LE(b.length); this.child.stdin.write(h); this.child.stdin.write(b); this.pump(); }); }
  async close() { this.child.stdin.end(); if (this.child.exitCode === null) await new Promise(resolve => this.child.once('exit', resolve)); }
}

test('R1 R2 R4 R14 R16 R17 R21 hosted facade uses committed Go policy and native-identical application bytes', { timeout: 30000 }, async () => {
  const reactor = new Reactor(new WebAssembly.Module(readFileSync(out + '/core.wasm'))), native = new Native();
  const commands = [], calls = [], hostStores = [], actors = [], queue = [], reads = [];
  const cluster = 'directory-p09', keys = Object.fromEntries(['a','b','effects','root','joiner'].map(id => [id, generateKeyPairSync('ed25519')])), sequences = { a: 0, b: 0, effects:0, joiner:0 };
  const root = keys.root.publicKey.export({format:'der',type:'spki'}).subarray(-32);
  const initial = [{ namespace:'state',key:'identity',data:bytes({V:1,identity:'id_'+sha(root).slice(0,32),signing_key:root.toString('base64')}) }];
  for (const id of ['a','b','effects']) initial.push({ namespace: 'enrollment', key: 'actor/' + segment(id), data: bytes({ cluster, key: keys[id].publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64'), personal: id !== 'effects', ...(id === 'effects' ? {effects:true} : {}) }) });
  const size = writes => writes.reduce((n, r) => n + 12 + enc.encode(r.namespace).length + enc.encode(r.key).length + r.data.length, 0);
  const evaluate = directoryEvaluator(reactor, cluster, 8 * 1024 * 1024);
  const results = [[],[],[]];
  const accept = (id, effects) => { queue.push(...effects.send); reads.push(...effects.reads); };
  const pump = async blocked => { for (let count = 0; queue.length; count++) { assert(count < 2000); const m = queue.shift(); if (blocked) continue; accept(Number(m.to), await actors[Number(m.to) - 1].input(2, m.bytes)); } };
  const leader = () => actors.findIndex(a => a.status.role === 2) + 1;
  const command = (actor, change, now = 1000) => {
    const sequence = ++sequences[actor], c = { schema: 1, cluster, actor, session: 'api', sequence: String(sequence), request_digest: '', now, change, signature: null };
    if (sequence > 1) c.ack = String(sequence - 1);
    c.request_digest = sha(bytes({ cluster, actor, session: c.session, sequence: c.sequence, change }));
    c.signature = sign(null, Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'), bytes(c)]), keys[actor].privateKey).toString('base64');
    return bytes(c);
  };
  try {
    for (let i = 0; i < 3; i++) {
      const host = new Host(); hostStores.push(host);
      // This test-only genesis is supplied at the
      // first committed application entry, identically on all three stores.
      const evaluator = (raw, read, app) => {
        let base = [];
        if (!read('state', 'identity')) base = initial;
        const local = (ns, key) => base.find(r => r.namespace === ns && r.key === key)?.data ?? read(ns, key);
        const traced = { call(b) { const result = reactor.call(b); calls.push([b.slice(), result.slice()]); return result; } };
        const result = directoryEvaluator(traced, cluster, 8 * 1024 * 1024)(raw, local, { ...app, size: app.size + size(base) });
        results[i].push(decodeCanonical(dec.decode(result.result)));
        return { ...result, writes: [...base, ...result.writes] };
      };
      const actor = new MemberActor(host, reactor, evaluator); actors.push(actor);
      accept(i + 1, await actor.init({ cluster, id: String(i + 1), seed: String(i + 19), bootstrap: ['1','2','3'] }));
    }
    await pump(); for (let n = 0; n < 20 && !leader(); n++) { for (let i = 0; i < 3; i++) accept(i + 1, await actors[i].input(1)); await pump(); }
    assert(leader());
    let blocked = false;
    const facade = new RaftDirectoryFacade({
      async commit(raw) { const id = leader(), before = results[id - 1].length; commands.push(raw); accept(id, await actors[id - 1].input(3, raw)); await pump(blocked); if (results[id - 1].length === before) throw new Error('unavailable'); return results[id - 1].at(-1); },
      async barrierRead(id) { const l = leader(); accept(l, await actors[l - 1].input(4, id)); await pump(blocked); if (!reads.find(r => r.id === id)) throw new Error('unavailable'); return actors[l - 1].store.read('state', 'cell/' + segment('chat')); },
    });
    const effects = new RaftEffectsFacade({
      commitEffect: (change,now) => facade.command(command('effects',change,now)),
      async barrierRecord(key) { await facade.read('effect-read-'+commands.length); return actors[leader()-1].store.read(key.namespace,key.key); },
    }, cluster);
    const stored=new Map(); let failPut=true;
    const bucket={async put(id,b) {if(failPut) throw new Error('blob write failed');stored.set(id,b.slice());},async get(id,opts) {const b=stored.get(id);if(!b)return null;const offset=opts?.range?.offset??0,length=opts?.range?.length??b.length;return {arrayBuffer:async()=>b.slice(offset,offset+length).buffer};}};
    const sqlHost=new Host();hostStores.push(sqlHost);const store=new R2Store(bucket,sqlHost.sql,cluster);
    const rid='1'.repeat(64), f=await sealFrame(encodeFrame('0'.repeat(32),[{rid,bytes:enc.encode('AGEO\x01sealed-object')}]));
    const receipt={device:'a',head:'sealed-one',head_rid:rid};let durable=0;
    await assert.rejects(store.put(f),/blob write failed/);
    await assert.rejects(effects.upload(store,receipt,async()=>{durable++;},1000));
    assert.equal(durable,0);failPut=false;await store.put(f);
    const receiptID=await effects.upload(store,receipt,async()=>{durable++;},1000);assert.equal(durable,1);
    let r = await facade.command(command('a', { create: { cell: 'chat', init: { head: 'sealed-one', class: 'chat', size: '1', keys: {} }, receipt: receiptID } })); assert.equal(r.code, 'ok'); assert.equal(r.view.cell.lease.fence, '1');
    const take = command('b', { acquire: { cell: 'chat', force: true, expect: { device: 'a', fence: '1' } } });
    r = await facade.command(take); assert.equal(r.code, 'ok'); assert.equal(r.view.cell.lease.fence, '2');
    assert.deepEqual(await facade.command(take), r, 'exact retry');
    r = await facade.command(command('a', { publish: { cell: 'chat', publish: { fence: '1', old_head: 'wrong', head: 'bad', size: '0', class: '', pending: 0 }, receipt: '' } })); assert.equal(r.code, 'fence_stale');
    r = await facade.command(command('a', { set_vault: { old: '', new: 'sealed-vault' } })); assert.equal(r.code, 'ok');
    r = await facade.command(command('b', { set_vault: { old: '', new: 'wrong' } })); assert.equal(r.code, 'cas');
    const delayed = command('b', { release: { cell: 'chat', fence: '2', quiet: false } });
    r = await facade.command(command('a', { revoke: { device: 'b' } })); assert.equal(r.code, 'not_found');
    // Enrollment and device metadata are distinct; the device's first metadata
    // write preserves relay-owned fields before revocation is committed.
    r = await facade.command(command('a', { put_device: { id: 'b', device: { V: 1, name: 'c2VhbGVk', added_by: '', revoked: false, caps: { os: '', arch: '', sandbox: null, container: null, gpu: null, cow: '' } } } })); assert.equal(r.code, 'ok');
    r = await facade.command(command('a', { revoke: { device: 'b' } })); assert.equal(r.code, 'ok');
    r = await facade.command(delayed); assert.equal(r.code, 'revoked');
    const publicKey=keys.joiner.publicKey.export({format:'der',type:'spki'}).subarray(-32), device='dev_'+sha(publicKey).slice(0,32);
    let pending={code:'k7m2q9xd',device,pubkey:publicKey.toString('base64url'),expires_at:10000,platform:'linux',state:'pending',grant:null,decided_at:0};
    let loseDelivery=true;
    const box={async bind(c){pending=boundLink(pending,c);return {ok:pending};},async deliver(c,id,state,grant,target){pending=deliveredLink(pending,c,id,state,grant,1000,target);if(loseDelivery){loseDelivery=false;throw new Error('lost delivery reply');}return {ok:null};}};
    await effects.admit(box,'k7m2q9xd',1000);
    await assert.rejects(effects.deliver(box,'k7m2q9xd',1000),/request_gone/);assert.equal(pending.state,'pending');
    const cert={V:1,device:publicKey.toString('hex'),created:1,sig:''};cert.sig=sign(null,Buffer.from('codeaf/device-cert/v1\n'+JSON.stringify(cert)),keys.root.privateKey).toString('hex');
    const decision={code:'k7m2q9xd',device,record:{V:1,name:'c2VhbGVk',added_by:'',revoked:false,caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''}},grant:'c2VhbGVk',cert:Buffer.from(JSON.stringify(cert)).toString('base64url'),deny:false};
    r=await facade.command(command('a',{decide_join:decision}));assert.equal(r.code,'ok');
    assert(actors[leader()-1].store.read('enrollment','actor/'+segment(device)));
    await assert.rejects(effects.deliver(box,'k7m2q9xd',1000),/lost delivery reply/);assert.equal(pending.grant,decision.grant);
    await effects.deliver(box,'k7m2q9xd',1000);await effects.deliver(box,'k7m2q9xd',1000);
    const outbox=decodeCanonical(dec.decode(actors[leader()-1].store.read('outbox','join/'+segment(decision.code))));assert.equal(outbox.outcome,'delivered');
    const row = await facade.read('fresh'); assert.equal(decodeCanonical(dec.decode(row)).head, 'sealed-one');
    for (const actor of actors) assert.deepEqual(actor.store.read('state', 'cell/' + segment('chat')), row);
    blocked = true; await assert.rejects(facade.read('minority'), /unavailable/);
    // Replay every complete application ABI call against the exact native Go
    // binary. This includes dependency requests, signatures and refusal bytes.
    for (const [input, expected] of calls) assert.deepEqual(await native.call(input), expected);
    assert(calls.length > 50); assert.equal(reactor.metrics().wasi.poll_oneoff ?? 0, 0);
  } finally { await native.close(); for (const h of hostStores) h.db.close(); }
});
