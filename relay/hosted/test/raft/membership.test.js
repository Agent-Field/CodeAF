import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import { DatabaseSync } from 'node:sqlite';
import { generateKeyPairSync, createHash, sign } from 'node:crypto';
import { Reactor } from '../../src/raft/abi.js';
import { MemberActor } from '../../src/raft/actor.js';
import { directoryEvaluator } from '../../src/raft/directory.js';
import { committedRegistry, provisionedRegistry } from '../../src/raft/membership.js';
import { encodeCanonical } from '../../src/raft/codec.js';

const out = process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p12';
const enc = new TextEncoder(), dec = new TextDecoder(), bytes = v => enc.encode(encodeCanonical(v));
const sha = b => createHash('sha256').update(b).digest('hex');
const segment = s => Buffer.from(s).toString('hex');
class Host {
  constructor() { this.db = new DatabaseSync(':memory:'); this.sql = { exec: (q, ...p) => { const rows = this.db.prepare(q).all(...p); return { toArray: () => rows, [Symbol.iterator]: () => rows[Symbol.iterator]() }; } }; }
  transactionSync(fn) { this.db.exec('BEGIN'); try { const r = fn(); this.db.exec('COMMIT'); return r; } catch (e) { this.db.exec('ROLLBACK'); throw e; } }
  async sync() {}
}
class Native {
  constructor() {
    this.child = spawn(out + '/native-peer', [], { stdio: ['pipe','pipe','inherit'], env: { ...process.env, GOMEMLIMIT: '32MiB' } }); this.buffer = Buffer.alloc(0);
    this.child.stdout.on('data', b => { this.buffer = Buffer.concat([this.buffer,b]); if (!this.waiter || this.buffer.length < 4) return; const n = this.buffer.readUInt32LE(); if (this.buffer.length < n + 4) return; const r = this.buffer.subarray(4,n+4); this.buffer = this.buffer.subarray(n+4); const w = this.waiter; this.waiter = null; clearTimeout(w.timer); w.resolve(new Uint8Array(r)); });
    this.child.on('exit', code => { if (this.waiter) { clearTimeout(this.waiter.timer); this.waiter.reject(Error('native exited ' + code)); } });
  }
  call(b) { return new Promise((resolve,reject) => { assert(!this.waiter); this.waiter = { resolve,reject,timer:setTimeout(() => reject(Error('native timeout')),5000) }; const h = Buffer.alloc(4); h.writeUInt32LE(b.length); this.child.stdin.write(h); this.child.stdin.write(b); }); }
  async close() { this.child.stdin.end(); if (this.child.exitCode === null) await new Promise(r => this.child.once('exit',r)); }
}

test('R6 R7 R9 R11 R12 R13 R14 R16 R17 hosted learner intents, exact retries, tombstones and native policy parity', { timeout:30000 }, async () => {
 const reactor = new Reactor(new WebAssembly.Module(readFileSync(out + '/core.wasm'))), native = new Native(), calls = [];
 const policy = { call(b) { const result = reactor.call(b); calls.push([b.slice(),result.slice()]); return result; } };
 const keys = Object.fromEntries(['a','b','x1','x2','x3'].map(id => [id,generateKeyPairSync('ed25519')]));
 const pub = id => keys[id].publicKey.export({ format:'der',type:'spki' }).subarray(-32).toString('base64');
 const cluster = 'p12-hosted', hosts = [], actors = [], queue = [], seq = {a:0,b:0}, results = [];
 const anchors = [1,2,3].map(node => ({node:String(node),device:'',key:pub('x'+node),incarnation:'1',protocol:1,anchor:true,retired:false,ready_index:'0'}));
 const genesis = ['a','b'].flatMap(id => [
  {namespace:'enrollment',key:'actor/'+segment(id),data:bytes({cluster,key:pub(id),personal:true})},
  {namespace:'state',key:'device/'+segment(id),data:bytes({V:1,name:'',added_by:'',revoked:false,caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''}})},
 ]);
 genesis.push({namespace:'state',key:'identity',data:bytes({V:1,identity:'test'})});
 const size = rows => rows.reduce((n,r) => n+12+r.namespace.length+r.key.length+r.data.length,0);
 const configs = [], absent = new Set();
 const accept = e => queue.push(...e.send);
 const pump = async () => { for (let n=0;queue.length;n++) { assert(n<2000); const m=queue.shift(), id=Number(m.to); if (!actors[id-1] || absent.has(id)) continue; accept(await actors[id-1].input(2,m.bytes)); } };
 const leader = () => actors.findIndex((a,i) => !absent.has(i+1) && a.status.role===2);
 const add = async id => {
  const host = new Host(); hosts.push(host);
  const evaluator = (raw,read,app) => {
   const base = read('state','identity') ? [] : genesis;
   const r = directoryEvaluator(policy,cluster,8*1024*1024)(raw,(ns,k) => base.find(r => r.namespace===ns && r.key===k)?.data ?? read(ns,k),{...app,size:app.size+size(base)});
   if (!results[id-1]) results[id-1]=[]; results[id-1].push(JSON.parse(dec.decode(r.result))); return {...r,writes:[...base,...r.writes]};
  };
  const config = {cluster,id:String(id),seed:String(id+44),bootstrap:id<=3?['1','2','3']:[]}; configs[id-1]=config;
  const actor = new MemberActor(host,policy,evaluator); actors[id-1]=actor; accept(await actor.init(config));
 };
 const command = (change,signer='a') => {
  const actor=signer.startsWith('x')?'anchor/'+signer.slice(1):signer;
  const sequence=seq[actor]=(seq[actor]??0)+1, c={schema:1,cluster,actor,session:'api',sequence:String(sequence),request_digest:'',now:1000,change,signature:null};
  if (sequence>1)c.ack=String(sequence-1);
  c.request_digest=sha(bytes({cluster,actor:c.actor,session:c.session,sequence:c.sequence,change}));
  c.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),bytes(c)]),keys[signer].privateKey).toString('base64');return bytes(c);
 };
 const submit = async raw => {const i=leader(),n=results[i]?.length??0;accept(await actors[i].input(3,raw));await pump();assert.equal(results[i].length,n+1);return results[i].at(-1);};
 const registry = () => committedRegistry(policy,actors[leader()].store);
 const header = () => JSON.parse(dec.decode(actors[leader()].store.read('membership','registry')));
 const settle = async () => {
  let i=leader(),e=await actors[i].membership();accept(e);await pump();e=await actors[i].membership();accept(e);await pump();assert.equal(e.settle,true);
  const h=header();const r=await submit(command({settle_membership:{revision:h.revision,node:h.pending.node}}));assert.equal(r.code,'ok');return r;
 };
 try {
  for (let id=1;id<=3;id++)await add(id);
  await pump();for (let n=0;n<30&&leader()<0;n++){for(const a of actors)accept(await a.input(1));await pump();}assert(leader()>=0);
  assert.equal(Object.keys(provisionedRegistry(policy,{anchors,bootstrap:['1','2','3']})).length,3);
  assert.throws(()=>provisionedRegistry(policy,{anchors:anchors.slice(0,2),bootstrap:['1','2','3']}));
  let r=await submit(command({init_membership:{anchors,limit:4096}}));assert.equal(r.code,'ok');assert.equal(Object.keys(registry()).length,3);
  let h=header();const enroll=command({enroll_member:{node:h.next_node,revision:h.revision,device:'b',protocol:1}});
  r=await submit(enroll);assert.equal(r.code,'ok');assert.equal(r.membership.node,'4');assert.equal(r.membership.barrier,'0');
  assert.deepEqual(await submit(enroll),r);
  await add(4);absent.add(4);const lagged=(leader()+1)%3;absent.add(lagged+1);accept(await actors[leader()].membership());await pump();h=header();r=await submit(command({settle_membership:{revision:h.revision,node:h.pending.node}}));assert.equal(r.code,'ok');absent.delete(lagged+1);for(let n=0;n<3;n++){accept(await actors[leader()].input(1));await pump();}for(let n=0;n<3;n++)assert.deepEqual(actors[n].store.read('membership','registry'),actors[leader()].store.read('membership','registry'),'mixed configuration/settlement batch diverged');assert.equal(Object.keys(registry()).length,4);assert.equal(registry()['4'].voter,false);
  const i=leader(),snapshot=await actors[i].checkpoint(),info=actors[i].codec.inspect(3,snapshot),id=actors[i].store.hash(info.data);
  await actors[3].begin(info.data);const m=actors[i].store.generation(id).manifest;
  for(let n=0;n<Number(m.chunks);n++){const b=actors[i].store.chunk(id,n);await actors[3].chunk(id,n,b,actors[i].store.hash(b));}
  await actors[3].complete(id);assert.deepEqual(await actors[3].joinStatus(r.membership.barrier),{ready:false});
  absent.delete(4);accept(await actors[i].input(12,{peer:4n,snapshot:true,success:false}));
  for(let n=0;n<12&&actors[3].status.applied<actors[i].status.applied;n++){accept(await actors[i].input(1));await pump();}
  assert.deepEqual(await actors[3].joinStatus(r.membership.barrier),{ready:true});
  // Recover the learner from its SQLite bytes into a new runtime handle.
  await actors[3].close();const restoredEvaluate=(...args)=>{const r=directoryEvaluator(policy,cluster,8*1024*1024)(...args);results[3]??=[];results[3].push(JSON.parse(dec.decode(r.result)));return r;};const restored=new MemberActor(hosts[3],policy,restoredEvaluate);actors[3]=restored;accept(await restored.init(configs[3]));await pump();assert.deepEqual(await restored.joinStatus(r.membership.barrier),{ready:true});
  // R6/R7 use actual Go policy, persisted wasm actors and the production
  // planner. The native peer replays every signed application evaluation.
  h=header();r=await submit(command({request_leadership:{revision:h.revision,node:'4',device:'b'}}));assert.equal(r.code,'ok');
  const tickAll=async()=>{for(let n=0;n<actors.length;n++)if(!absent.has(n+1))accept(await actors[n].input(1));await pump();};
  const reconcileLead=async()=>{
   for(let n=0;n<40;n++){
    if(leader()<0){await tickAll();continue;}
    const h=header(),l=h.leadership;if(!l)return;
    if(!l.restore&&l.settled&&leader()===3){
     accept(await actors[3].input(4,'leadership-proof'));await pump();assert.equal((await actors[3].leadershipReceipt()).node,'4');
     const r=await submit(command({complete_leadership:{revision:h.revision}},'x1'));assert.equal(r.code,'ok');return;
    }
    const target=Number(l.restore?l.standby:l.node)-1;let evidence;
    try{evidence=await actors[target].leadershipEvidence(h.revision,l.barrier);}catch{await tickAll();continue;}
    const e=await actors[leader()].leadership(evidence);accept(e);await pump();
    if(e.plan.settle){const r=await submit(command({settle_leadership:{revision:h.revision}},'x1'));assert.equal(r.code,'ok');}
   }
   assert.fail('leadership did not finish');
  };
  await reconcileLead();assert.equal(leader(),3);assert.equal(registry()['3'].voter,false);assert(header().leadership.completed);
  absent.add(4);for(let n=0;n<30&&leader()<0;n++)await tickAll();assert([0,1].includes(leader()));
  h=header();let plan=await actors[leader()].leadership({node:'0',revision:h.revision,applied:'0',protocol:1,durable:false});assert(plan.plan.fallback);assert.equal(plan.plan.transfer,'0');
  r=await submit(command({restore_anchors:{revision:h.revision}},'x1'));assert.equal(r.code,'ok');await tickAll();await reconcileLead();assert.equal(registry()['4'].voter,false);assert.equal(header().leadership,undefined);
  absent.delete(4);await tickAll();assert.notEqual(leader(),3);
  h=header();absent.add(4);r=await submit(command({remove_member:{revision:h.revision,node:'4',device:'b'}}));assert.equal(r.code,'ok');await settle();assert.equal(registry()['4'].revoked,true);
  await actors[leader()].checkpoint();assert(actors[leader()].store.read('tombstones','node/4'));
  h=header();r=await submit(command({enroll_member:{node:h.next_node,revision:h.revision,device:'b',protocol:1}}));assert.equal(r.code,'ok');assert.equal(r.membership.node,'5');
  r=await submit(command({revoke:{device:'b'}}));assert.equal(r.code,'ok');assert.equal(registry()['5'].revoked,true);
  await assert.rejects(actors[leader()].leadership({node:'5',revision:header().revision,applied:'0',protocol:1,durable:true}),/leadership target is unavailable/);
  for(const [b,expected] of calls)assert.deepEqual(await native.call(b),expected);
  assert(calls.length>30);
 } finally {await native.close();for(const h of hosts)h.db.close();}
});
