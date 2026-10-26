import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import { DatabaseSync } from 'node:sqlite';
import { createHash, generateKeyPairSync, sign, createCipheriv } from 'node:crypto';
import { Reactor } from '../../src/raft/abi.js';
import { encodeCanonical, decodeCanonical } from '../../src/raft/codec.js';
import { MemberActor } from '../../src/raft/actor.js';
import { directoryEvaluator } from '../../src/raft/directory.js';
import { committedRegistry } from '../../src/raft/membership.js';
import { workerCell, workerAssignment } from '../../src/raft/worker.js';

const out = process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p16';
const enc = new TextEncoder(), dec = new TextDecoder();
const bytes = value => enc.encode(encodeCanonical(value));
const sha = value => createHash('sha256').update(value).digest('hex');
const segment = value => Buffer.from(value).toString('hex');
const caps = {os:'linux',arch:'arm64',sandbox:null,container:null,gpu:null,cow:'none'};
const seal = text => { const cipher = createCipheriv('aes-256-gcm',Buffer.alloc(32,3),Buffer.alloc(12,7)); return Buffer.concat([Buffer.alloc(12,7),cipher.update(text),cipher.final(),cipher.getAuthTag()]).toString('base64'); };
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


test('R18 R19 R21 assignment claims, delayed decisions and uploaded heads match native Go', { timeout:30000 }, async () => {
 const reactor = new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))), native = new Native(), calls = [];
 const runtime = {call(b) {const r=reactor.call(b);calls.push([b.slice(),r.slice()]);return r;}};
 const cluster='worker-p15', keys=Object.fromEntries(['owner','root','worker','effects','x1','x2','x3'].map(id=>[id,generateKeyPairSync('ed25519')]));
 const pub = id => keys[id].publicKey.export({format:'der',type:'spki'}).subarray(-32);
 const identity='id_'+sha(pub('root')).slice(0,32), worker='dev_'+sha(pub('worker')).slice(0,32);
 const rows=[{namespace:'state',key:'identity',data:bytes({V:1,identity,signing_key:pub('root').toString('base64')})},{namespace:'enrollment',key:'actor/'+segment('owner'),data:bytes({cluster,key:pub('owner').toString('base64'),personal:true})}];
 rows.push({namespace:'enrollment',key:'actor/'+segment('effects'),data:bytes({cluster,key:pub('effects').toString('base64'),personal:false,effects:true})});
 const actors=[],hosts=[],queue=[],results=[[],[],[]],reads=[];let serial=0;
 const configs=[1,2,3].map(id=>({cluster,id:String(id),seed:String(60+id),bootstrap:['1','2','3']}));
 const initialSize=rows.reduce((n,r)=>n+12+r.namespace.length+r.key.length+r.data.length,0);
 const accept=e=>{queue.push(...e.send);reads.push(...e.reads);};
 const pump=async()=>{for(let n=0;queue.length;n++){assert(n<2000);const m=queue.shift();if(Number(m.to)>actors.length)continue;accept(await actors[Number(m.to)-1].input(2,m.bytes));}};
 const leader=()=>actors.findIndex(a=>a.status.role===2);
 const evaluator=i=>(raw,read,app)=>{
  const base=read('state','identity')?[]:rows;
  const r=directoryEvaluator(runtime,cluster,8*1024*1024)(raw,(ns,key)=>base.find(r=>r.namespace===ns&&r.key===key)?.data??read(ns,key),{...app,size:app.size+(base.length?initialSize:0)});
  results[i].push(decodeCanonical(dec.decode(r.result)));return {...r,writes:[...base,...r.writes]};
 };
 const command=(change,{signer='owner',inc=0n,now=1000,session='req-'+(++serial)}={})=>{
  const actor=signer==='worker'?worker:signer, sequence='1';
  const fields={cluster,actor,session,sequence,change,...(inc?{worker_incarnation:String(inc)}:{})};
  const c={schema:1,...fields,request_digest:sha(bytes(fields)),now,signature:null};
  c.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),bytes(c)]),keys[signer].privateKey).toString('base64');return bytes(c);
 };
 const submit=async raw=>{const i=leader(),n=results[i].length;try {accept(await actors[i].input(3,raw));await pump();} catch(e) {throw new Error('assignment fixture operation '+Object.keys(decodeCanonical(dec.decode(raw)).change).join(',')+': '+e.message);}assert.equal(results[i].length,n+1);return results[i].at(-1);};
 const send=async(change,opts,code='ok')=>{const r=await submit(command(change,opts));assert.equal(r.code,code);return r;};
 const read=async cell=>{const id='read-'+(++serial),i=leader();accept(await actors[i].input(4,id));await pump();assert(reads.some(r=>r.id===id));return decodeCanonical(dec.decode(workerCell(runtime,actors[i].store,cluster,worker,cell,1n)));};
 const workrow=()=>decodeCanonical(dec.decode(actors[leader()].store.read('state','worker/'+segment(worker))));
 try {
  for(let i=0;i<3;i++){const h=new Host();hosts.push(h);const a=new MemberActor(h,runtime,evaluator(i));actors.push(a);accept(await a.init(configs[i]));}
  await pump();for(let n=0;n<30&&leader()<0;n++){for(const a of actors)accept(await a.input(1));await pump();}assert(leader()>=0);
  const cert={v:1,identity,cluster,key:pub('worker').toString('base64'),made:1000,expires:100000,cells:['child-one','child-two'],learner:false,signature:null};
  cert.signature=sign(null,Buffer.concat([Buffer.from('codeaf/worker-cert/v1\0'),bytes(cert)]),keys.root.privateKey).toString('base64');
  await send({enroll_worker:{certificate:cert,grants:{'child-one':seal('selected key one'),'child-two':seal('selected key two')}}});
  await send({register_worker:{previous:'0',incarnation:'1',caps,sealed_labels:seal('private name')}},{signer:'worker'});
  const beat={worker,incarnation:'1',sequence:'1',seen_at:1000,work:null,signature:null};
  beat.signature=sign(null,Buffer.concat([Buffer.from('codeaf/worker-beat/v1\0'),bytes({cluster,beat})]),keys.worker.privateKey).toString('base64');
  assert.equal((await send({worker_beat_batch:{items:[beat]}})).workers[0].code,'ok');
  await send({create:{v1:true,cell:'parent',init:{head:'parent-head',class:'',size:'0',keys:null},receipt:''}});
  const spec=cell=>({id:'run-'+cell,revision:'1',cell,parent_cell:'parent',expect:{device:'owner',fence:'1'},target:worker,incarnation:'1',expires:90000,sealed_payload:seal('private task path /private/repo shell prompt credential')});
  const ref=v=>({id:v.id,revision:v.revision,cell:v.cell,target:v.target,incarnation:v.incarnation});
  const assigned=[];
  for(const cell of cert.cells){
   await send({create:{v1:true,cell,init:{head:'head-'+cell,parent_cell:'parent',class:'',size:'0',keys:null},receipt:''}});
   const v=spec(cell);await send({assign:v});assigned.push(v);
  }
  // A decision authenticated before cancellation must still lose at APPLY.
  const delayed=command({claim_assignment:{ref:ref(assigned[1]),expect:assigned[1].expect}},{signer:'worker',inc:1n});
  await send({cancel_assignment:{ref:ref(assigned[1])}});
  assert.equal((await submit(delayed)).code,'conflict');
  const first=assigned[0],claim=command({claim_assignment:{ref:ref(first),expect:first.expect}},{signer:'worker',inc:1n});
  let r=await submit(claim);assert.equal(r.code,'ok');assert.equal(r.view.cell.lease.fence,'2');const claimed=r;
  assert.deepEqual(await submit(claim),claimed);
  const readRun=async()=>{const id='run-read-'+(++serial),i=leader();accept(await actors[i].input(4,id));await pump();assert(reads.some(r=>r.id===id));return decodeCanonical(dec.decode(workerAssignment(runtime,actors[i].store,cluster,worker,1n,ref(first))));};
  r=await readRun();assert.equal(r.code,'ok');assert.equal(r.assignment.status,'claimed');assert(r.grant);
  const publish={cell:first.cell,publish:{fence:'2',old_head:'head-'+first.cell,head:'result',size:'0',class:'',pending:0},receipt:''};
  await send({publish},{signer:'worker',inc:1n},'bad_request');
  // The missing blob's receipt is absent; a retry after durable upload uses
  // the SAME Go receipt check. R2 failure itself has its existing targeted gate.
  const receipt={device:worker,cell:first.cell,worker_incarnation:'1',head:'result',head_rid:'rid'};
  const receiptID=sha(bytes(receipt));
  await send({record_upload:receipt},{signer:'effects'});
  publish.receipt=receiptID;await send({publish},{signer:'worker',inc:1n});
  await send({acquire:{cell:'parent',force:true,expect:{device:'owner',fence:'1'}}});
  await send({finish_assignment:{ref:ref(first),fence:'2',head:'result'}},{signer:'worker',inc:1n});
  const parent=actors[leader()].store.read('state','cell/'+segment('parent'));
  const p=decodeCanonical(dec.decode(parent));assert.equal(p.head,'parent-head');assert.equal(p.lease.fence,'2');
  // r3.5
  const released={...spec('child-two'),id:'released-run'};
  await send({assign:released});
  const counts=a=>decodeCanonical(dec.decode(a.store.read('sessions','counts')));
  const beforeRelease=counts(actors[leader()]);
  const scoped=(kind,change)=>command(change,{signer:'worker',inc:1n,session:'worker-'+kind+'-'+sha(bytes(ref(released)))});
  const releasedClaim=scoped('claim',{claim_assignment:{ref:ref(released),expect:released.expect}});
  const releaseClaimed=await submit(releasedClaim);assert.equal(releaseClaimed.code,'ok');
  assert.deepEqual(await submit(releasedClaim),releaseClaimed);
  const releasedFinish=scoped('finish',{finish_assignment:{ref:ref(released),fence:releaseClaimed.view.cell.lease.fence,head:releaseClaimed.view.cell.head}});
  assert.equal((await submit(releasedFinish)).code,'ok');
  assert.equal((await submit(releasedFinish)).code,'ok');
  const releasedCell=actors[leader()].store.read('state','cell/'+segment(released.cell));
  assert.deepEqual(await submit(releasedClaim),releaseClaimed);
  assert.deepEqual(actors[leader()].store.read('state','cell/'+segment(released.cell)),releasedCell);
  for(const a of actors)assert.deepEqual(counts(a),beforeRelease);
  for(const a of actors)await a.checkpoint();
  const reopened=(leader()+1)%3;await actors[reopened].close();actors[reopened]=new MemberActor(hosts[reopened],runtime,evaluator(reopened));accept(await actors[reopened].init(configs[reopened]));await pump();
  assert.deepEqual(counts(actors[reopened]),beforeRelease);
  assert.deepEqual(await submit(claim),claimed);
  assert.equal((await readRun()).assignment.status,'finished');
  await send({revoke:{device:worker}});assert.equal((await readRun()).code,'revoked');
  for(const a of actors){const raw=a.store.read('state','assignment/'+segment(first.id));for(const sentinel of ['private task','/private/repo','shell prompt','credential'])assert(!Buffer.from(raw).includes(Buffer.from(sentinel)));}
  for(const[input,expected]of calls)assert.deepEqual(await native.call(input),expected);
  assert(calls.length>100);assert.equal(reactor.metrics().wasi.poll_oneoff??0,0);
 }finally{await native.close();for(const h of hosts)h.db.close();}
});
