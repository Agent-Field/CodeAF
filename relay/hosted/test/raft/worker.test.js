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
import { workerCell } from '../../src/raft/worker.js';

const out = process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p15';
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


test('R3 R13 R14 R15 R16 R17 R18 delegated worker policy and selected grants match native Go', { timeout:30000 }, async () => {
 const reactor = new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))), native = new Native(), calls = [];
 const runtime = {call(b) {const r=reactor.call(b);calls.push([b.slice(),r.slice()]);return r;}};
 const cluster='worker-p15', keys=Object.fromEntries(['owner','root','worker','x1','x2','x3'].map(id=>[id,generateKeyPairSync('ed25519')]));
 const pub = id => keys[id].publicKey.export({format:'der',type:'spki'}).subarray(-32);
 const identity='id_'+sha(pub('root')).slice(0,32), worker='dev_'+sha(pub('worker')).slice(0,32);
 const rows=[{namespace:'state',key:'identity',data:bytes({V:1,identity,signing_key:pub('root').toString('base64')})},{namespace:'enrollment',key:'actor/'+segment('owner'),data:bytes({cluster,key:pub('owner').toString('base64'),personal:true})}];
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
 const command=(change,{signer='owner',inc=0n,now=1000}={})=>{
  const actor=signer==='worker'?worker:signer, session='req-'+(++serial), sequence='1';
  const fields={cluster,actor,session,sequence,change,...(inc?{worker_incarnation:String(inc)}:{})};
  const c={schema:1,...fields,request_digest:sha(bytes(fields)),now,signature:null};
  c.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),bytes(c)]),keys[signer].privateKey).toString('base64');return bytes(c);
 };
 const submit=async raw=>{const i=leader(),n=results[i].length;accept(await actors[i].input(3,raw));await pump();assert.equal(results[i].length,n+1);return results[i].at(-1);};
 const send=async(change,opts,code='ok')=>{const r=await submit(command(change,opts));assert.equal(r.code,code);return r;};
 const read=async cell=>{const id='read-'+(++serial),i=leader();accept(await actors[i].input(4,id));await pump();assert(reads.some(r=>r.id===id));return decodeCanonical(dec.decode(workerCell(runtime,actors[i].store,cluster,worker,cell,1n)));};
 const workrow=()=>decodeCanonical(dec.decode(actors[leader()].store.read('state','worker/'+segment(worker))));
 try {
  for(let i=0;i<3;i++){const h=new Host();hosts.push(h);const a=new MemberActor(h,runtime,evaluator(i));actors.push(a);accept(await a.init(configs[i]));}
  await pump();for(let n=0;n<30&&leader()<0;n++){for(const a of actors)accept(await a.input(1));await pump();}assert(leader()>=0);
  const cert={v:1,identity,cluster,key:pub('worker').toString('base64'),made:1000,expires:100000,cells:['chat'],learner:true,signature:null};
  cert.signature=sign(null,Buffer.concat([Buffer.from('codeaf/worker-cert/v1\0'),bytes(cert)]),keys.root.privateKey).toString('base64');
  const grant=seal('secret selected-chat key credential /private/repo');
  await send({enroll_worker:{certificate:cert,grants:{chat:grant}}});
  const registration=command({register_worker:{previous:'0',incarnation:'1',caps,sealed_labels:seal('private worker name and scheduling requirement')}},{signer:'worker'});
  const registered=await submit(registration);assert.equal(registered.code,'ok');assert.deepEqual(await submit(registration),registered);
  const beat={worker,incarnation:'1',sequence:'1',seen_at:1001,work:null,signature:null};
  beat.signature=sign(null,Buffer.concat([Buffer.from('codeaf/worker-beat/v1\0'),bytes({cluster,beat})]),keys.worker.privateKey).toString('base64');
  let r=await send({worker_beat_batch:{items:[beat]}},{now:1001});assert.equal(r.workers[0].code,'ok');assert.equal(workrow().available_until,91001);
  await send({create:{v1:true,cell:'chat',init:{head:'head',class:'',size:'0',keys:null},receipt:''}});
  assert.equal((await read('outside')).code,'unauthorized');r=await read('chat');assert.equal(r.code,'ok');assert.equal(r.grant,grant);assert.equal(r.view.cell.head,'head');
  for(const change of [{rotate:{op:'freeze'}},{decide_join:{code:'pair',device:'',record:{V:0,name:'',added_by:'',revoked:false,caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''}},grant:'',cert:'',deny:true}},{request_leadership:{device:worker,node:'4',revision:'1'}},{acquire:{cell:'chat',force:true,expect:{device:'owner',fence:'1'}}},{publish:{cell:'outside',publish:{fence:'1',old_head:'head',head:'bad',size:'0',class:'',pending:0},receipt:''}}])await send(change,{signer:'worker',inc:1n},'unauthorized');
  const anchors=[1,2,3].map(node=>({node:String(node),device:'',key:pub('x'+node).toString('base64'),incarnation:'1',protocol:1,anchor:true,retired:false,ready_index:'0'}));
  await send({init_membership:{anchors,limit:4096}});
  const header=()=>decodeCanonical(dec.decode(actors[leader()].store.read('membership','registry')));
  let h=header();await send({enroll_member:{node:h.next_node,revision:h.revision,device:worker,protocol:1}});
  let i=leader(),e=await actors[i].membership();accept(e);await pump();e=await actors[i].membership();accept(e);await pump();assert(e.settle);
  h=header();await send({settle_membership:{node:h.pending.node,revision:h.revision}});
  let registry=committedRegistry(runtime,actors[leader()].store);assert.equal(registry['4'].revoked,false);assert.equal(registry['4'].voter,false);
  for(let j=0;j<3;j++)await actors[j].checkpoint();
  // Reopen a real stored follower; an active leader is never cache-evicted.
  const reopened=(leader()+1)%3;
  await actors[reopened].close();actors[reopened]=new MemberActor(hosts[reopened],runtime,evaluator(reopened));accept(await actors[reopened].init(configs[reopened]));await pump();
  assert.equal(decodeCanonical(dec.decode(actors[reopened].store.read('state','worker/'+segment(worker)))).grants.chat,grant);
  assert.deepEqual(await submit(registration),registered);assert.equal((await read('chat')).grant,grant);
  await send({drain_worker:{worker,incarnation:'1'}});
  await send({register_worker:{previous:'1',incarnation:'2',caps,sealed_labels:seal('private worker name and scheduling requirement')}},{signer:'worker',inc:1n});
  assert.equal(workrow().draining,true);assert.equal(workrow().available_until,0);
  registry=committedRegistry(runtime,actors[leader()].store);assert.equal(registry['4'].revoked,true,'old peer incarnation');
  assert.equal((await submit(registration)).code,'conflict');
  await send({revoke:{device:worker}});
  await send({release:{cell:'chat',fence:'1',quiet:false}},{signer:'worker',inc:2n},'revoked');
  assert.equal((await read('chat')).code,'revoked');
  for(const a of actors){
   for(const [ns,key] of [['state','worker/'+segment(worker)],['enrollment','actor/'+segment(worker)]]){
    const b=a.store.read(ns,key);assert(b);for(const needle of ['private worker name','/private/repo','selected-chat key'])assert(!Buffer.from(b).includes(Buffer.from(needle)));
   }
  }
  for(const [input,expected]of calls)assert.deepEqual(await native.call(input),expected);
  assert(calls.length>100);assert.equal(reactor.metrics().wasi.poll_oneoff??0,0);
 }finally{await native.close();for(const h of hosts)h.db.close();}
});
