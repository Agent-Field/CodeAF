import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import { DatabaseSync } from 'node:sqlite';
import { Reactor } from '../../src/raft/abi.js';
import { encodeCanonical, decodeCanonical } from '../../src/raft/codec.js';
import { MemberActor } from '../../src/raft/actor.js';
import { directoryEvaluator } from '../../src/raft/directory.js';
import { v1Command } from '../../src/raft/v1.js';

const out = process.env.CODEAF_RAFT_PROOF_DIR;
const enc = new TextEncoder(), dec = new TextDecoder();
const bytes = v => enc.encode(encodeCanonical(v));
const segment = s => Buffer.from(s).toString('hex');

// R14/R16/R25 run the SAME frozen independently signed legacy requests through
// real WASI members and compare every actual Go ABI call with the native core.
test('R14 R16 R25 original v1 requests commit through WASI with native-identical proofs', { timeout: 30000 }, async () => {
  assert(out, 'build the proof artifacts first');
  const fixture = JSON.parse(readFileSync(new URL('../../../../internal/raftdirectory/testdata/v1-requests.json', import.meta.url)));
  const runtime = new Reactor(new WebAssembly.Module(readFileSync(out + '/core.wasm')));
  const calls = [], traced = { call(input) { const result = runtime.call(input); calls.push([input.slice(), result.slice()]); return result; } };
  const child = spawn(out + '/native-peer', [], { stdio: ['pipe','pipe','inherit'], env: { ...process.env, GOMEMLIMIT: '32MiB' } });
  let buffer = Buffer.alloc(0), waiter;
  const pumpNative = () => { if (!waiter || buffer.length < 4) return; const n = buffer.readUInt32LE(); assert(n <= 2 * 1024 * 1024); if (buffer.length < n + 4) return; const b = buffer.subarray(4,n+4); buffer = buffer.subarray(n+4); const w = waiter; waiter = null; clearTimeout(w.timer); w.resolve(new Uint8Array(b)); };
  child.stdout.on('data', b => { buffer = Buffer.concat([buffer,b]); pumpNative(); });
  const native = input => new Promise((resolve,reject) => { assert(!waiter); waiter = {resolve,reject,timer:setTimeout(()=>reject(new Error('native ABI timeout')),5000)}; const h = Buffer.alloc(4); h.writeUInt32LE(input.length); child.stdin.write(h); child.stdin.write(input); pumpNative(); });
  const stores = [], actors = [], queue = [], results = [[],[],[]], cluster = 'p17-v1';
  const first = fixture.cases[0], root = Buffer.from(first.headers['Codeaf-Identity'],'base64url');
  const initial = [{ namespace:'state', key:'identity', data:bytes({ V:1, identity:'id_' + (await import('node:crypto')).createHash('sha256').update(root).digest('hex').slice(0,32), signing_key:root.toString('base64') }) }];
  const b = fixture.cases.find(c => c.name === 'put-b'), cert = JSON.parse(Buffer.from(b.headers['Codeaf-Cert'],'base64url'));
  for (const code of ['1234','5678']) initial.push({namespace:'outbox',key:'join/'+segment(code),data:bytes({device:fixture.actor_b,expires:fixture.now+90000,public_key:Buffer.from(cert.device,'hex').toString('base64')})});
  const size = initial.reduce((n,r)=>n+12+r.namespace.length+r.key.length+r.data.length,0);
  const accept = effects => queue.push(...effects.send);
  const pump = async () => { for (let n=0;queue.length;n++) { assert(n<3000); const m=queue.shift();accept(await actors[Number(m.to)-1].input(2,m.bytes)); } };
  const leader = () => actors.findIndex(a => a.status.role === 2);
  try {
    for (let i=0;i<3;i++) {
      const db = new DatabaseSync(':memory:'); stores.push(db);
      const storage = {sql:{exec(q,...p){const rows=db.prepare(q).all(...p);return {toArray:()=>rows,[Symbol.iterator]:()=>rows[Symbol.iterator]()};}},transactionSync(fn){db.exec('BEGIN');try{const v=fn();db.exec('COMMIT');return v;}catch(e){db.exec('ROLLBACK');throw e;}},async sync(){}};
      const evaluator = (raw,read,app) => {
        const base = read('state','identity') ? [] : initial;
        const result = directoryEvaluator(traced,cluster,8*1024*1024)(raw,(ns,k)=>base.find(r=>r.namespace===ns&&r.key===k)?.data??read(ns,k),{...app,size:app.size+(base.length?size:0)});
        results[i].push(decodeCanonical(dec.decode(result.result)));return {...result,writes:[...base,...result.writes]};
      };
      const actor = new MemberActor(storage,runtime,evaluator);actors.push(actor);accept(await actor.init({cluster,id:String(i+1),seed:String(i+20),bootstrap:['1','2','3']}));
    }
    await pump();for(let n=0;n<30&&leader()<0;n++){for(const actor of actors)accept(await actor.input(1));await pump();}assert(leader()>=0);
    for(const c of fixture.cases){
      const h=c.headers, proof={method:c.method,uri:c.uri,identity_key:Buffer.from(h['Codeaf-Identity'],'base64url').toString('base64'),certificate:h['Codeaf-Cert'],at:h['Codeaf-Time'],signature:Buffer.from(h['Codeaf-Sig'],'base64url').toString('base64'),body:Buffer.from(c.body).toString('base64')};
      const raw=v1Command(traced,cluster,fixture.now,proof), l=leader(), before=results[l].length;
      accept(await actors[l].input(3,raw));await pump();assert.equal(results[l].length,before+1,c.name);
      assert.equal(results[l].at(-1).code,c.name==='blind-force'?'lease_held':'ok',c.name);
    }
    for(const [input,expected] of calls)assert.deepEqual(await native(input),expected);
    assert(calls.length>100);
    for(const actor of actors){const row=decodeCanonical(dec.decode(actor.store.read('state','device/'+segment(fixture.actor_b))));assert.equal(row.revoked,true);assert.equal(actor.store.one("SELECT count(*) AS n FROM records WHERE namespace='sessions'").n,0);}
  } finally {
    child.stdin.end();if(child.exitCode===null)await new Promise(resolve=>child.once('exit',resolve));for(const db of stores)db.close();
  }
});
