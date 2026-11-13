import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import { DatabaseSync } from 'node:sqlite';
import { generateKeyPairSync,createHash,sign } from 'node:crypto';
import { Reactor } from '../../src/raft/abi.js';
import { MemberActor } from '../../src/raft/actor.js';
import { directoryEvaluator } from '../../src/raft/directory.js';
import { encodeCanonical,decodeCanonical } from '../../src/raft/codec.js';
import { coordinationPolicy,CommittedHolds,MemberAlarms,QuiescenceCoordinator } from '../../src/raft/liveness.js';

const out=process.env.CODEAF_RAFT_PROOF_DIR??'/tmp/codeaf-p14';
const enc=new TextEncoder(),dec=new TextDecoder(),bytes=v=>enc.encode(encodeCanonical(v));
class Host {
 constructor(){this.db=new DatabaseSync(':memory:');this.sql={exec:(q,...p)=>{const rows=this.db.prepare(q).all(...p);return {toArray:()=>rows,[Symbol.iterator]:()=>rows[Symbol.iterator]()}}};this.alarms=[];}
 transactionSync(fn){this.db.exec('BEGIN');try{const r=fn();this.db.exec('COMMIT');return r}catch(e){this.db.exec('ROLLBACK');throw e}}
 async sync(){}
 async setAlarm(at){this.alarms.push(at)}
 async deleteAlarm(){this.alarms.push(null)}
}
class Native {
 constructor(){this.child=spawn(out+'/native-peer',[],{stdio:['pipe','pipe','inherit'],env:{...process.env,GOMEMLIMIT:'32MiB'}});this.buffer=Buffer.alloc(0);
 this.child.stdout.on('data',b=>{this.buffer=Buffer.concat([this.buffer,b]);if(!this.waiter||this.buffer.length<4)return;const n=this.buffer.readUInt32LE();if(this.buffer.length<n+4)return;const v=this.buffer.subarray(4,n+4);this.buffer=this.buffer.subarray(n+4);const w=this.waiter;this.waiter=null;clearTimeout(w.timer);w.resolve(new Uint8Array(v))});
 this.child.on('exit',code=>{if(this.waiter){clearTimeout(this.waiter.timer);this.waiter.reject(Error('native exited '+code))}});}
 call(b){return new Promise((resolve,reject)=>{assert(!this.waiter);this.waiter={resolve,reject,timer:setTimeout(()=>reject(Error('native timeout')),5000)};const h=Buffer.alloc(4);h.writeUInt32LE(b.length);this.child.stdin.write(h);this.child.stdin.write(b)})}
 async close(){this.child.stdin.end();if(this.child.exitCode===null)await new Promise(r=>this.child.once('exit',r))}
}

// R22/R26 exercise production Go policy and actual persisted WASI actors. All
// ABI inputs, including timer decisions, replay byte-for-byte natively.
test('R22 R26 R27 committed renewal, durable sleep, cold wake and native parity',{timeout:30000},async()=>{
 const reactor=new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))),native=new Native(),calls=[];
 const runtime={call(b){const r=reactor.call(b);calls.push([b.slice(),r.slice()]);return r}};
 const policy=coordinationPolicy(runtime);
 const cluster='p14-hosted',actors=[],hosts=[],configs=[],queue=[],results=[];
 const keys=Object.fromEntries(['a','b','ingress','anchor'].map(id=>[id,generateKeyPairSync('ed25519')]));
 const pub=id=>keys[id].publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 const genesis=Object.keys(keys).map(id=>({namespace:'enrollment',key:'actor/'+Buffer.from(id).toString('hex'),data:bytes({cluster,key:pub(id),personal:id==='a'||id==='b',...(id==='ingress'?{effects:true}:{}),...(id==='anchor'?{anchor:true}:{})})}));
 genesis.push({namespace:'state',key:'identity',data:bytes({V:1,identity:'fixture'})});
 const size=rows=>rows.reduce((n,r)=>n+12+r.namespace.length+r.key.length+r.data.length,0);
 const accept=e=>queue.push(...e.send),leader=()=>actors.findIndex(a=>a.status.role===2);
 const pump=async()=>{for(let n=0;queue.length;n++){assert(n<2000);const m=queue.shift();accept(await actors[Number(m.to)-1].input(2,m.bytes))}};
 const add=async id=>{const host=new Host();hosts[id-1]=host;configs[id-1]={cluster,id:String(id),seed:String(100+id),bootstrap:['1','2','3']};
  const evaluate=(raw,read,app)=>{const base=read('state','identity')?[]:genesis;const r=directoryEvaluator(runtime,cluster,8*1024*1024)(raw,(ns,k)=>base.find(v=>v.namespace===ns&&v.key===k)?.data??read(ns,k),{...app,size:app.size+size(base)});results[id-1]??=[];results[id-1].push(decodeCanonical(dec.decode(r.result)));return {...r,writes:[...base,...r.writes]}};
  const a=new MemberActor(host,runtime,evaluate);actors[id-1]=a;accept(await a.init(configs[id-1]));};
 const sequences={};
 const command=(change,now=1000000,actor='a')=>{const n=sequences[actor]=(sequences[actor]??0)+1;const c={schema:1,cluster,actor,session:'test',sequence:String(n),...(n>1?{ack:String(n-1)}:{}),request_digest:'',now,change,signature:null};c.request_digest=createHash('sha256').update(bytes({cluster,actor,session:c.session,sequence:c.sequence,change})).digest('hex');c.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),bytes(c)]),keys[actor].privateKey).toString('base64');return bytes(c)};
 const submit=async(raw)=>{const l=leader(),n=results[l]?.length??0;accept(await actors[l].input(3,raw));await pump();assert.equal(results[l].length,n+1);return results[l].at(-1)};
 try{
  for(let id=1;id<=3;id++)await add(id);await pump();for(let n=0;n<20&&leader()<0;n++){for(const a of actors)accept(await a.input(1));await pump()}assert(leader()>=0);
  let r=await submit(command({create:{cell:'chat',v1:true,init:{head:'sealed',class:'host-bound',size:'0',keys:null},receipt:''}}));assert.equal(r.code,'ok');
  const bridge=new CommittedHolds({barrierRecord:async key=>{
   const l=leader();accept(await actors[l].input(4,'renew-read-'+calls.length));await pump();return actors[l].store.read(key.namespace,key.key);
  },commitEffect:async(change,now)=>submit(command(change,now,'ingress'))},policy);
  const receipts=[];bridge.opened(1030000,true);await bridge.renew(1030000,[{device:'a',seenAt:1030000,holds:[{cell:'chat',fence:'1'}],send:s=>receipts.push(JSON.parse(s))}]);
  assert.equal(receipts[0].holds[0].until,1120000);assert(BigInt(receipts[0].holds[0].index)>0n);
  r=await submit(command({release:{cell:'chat',fence:'1',quiet:false}},1030001));assert.equal(r.code,'ok');
  r=await submit(command({renew_holds:{device:'a',seen_at:1030000,items:[{cell:'chat',fence:'1'}]}},1030002,'ingress'));assert.equal(r.items[0].code,'fence_stale');
  for(const a of actors){accept(await a.schedule(0,1030002,false));await pump()}
  const l=leader(),status=actors[l].status;
  r=await submit(command({prepare_quiescence:{barrier:String(status.applied),term:String(status.term),voters:['1','2','3']}},1030003,'anchor'));assert.equal(r.code,'ok');
  for(const a of actors){accept(await a.input(1));await pump()}
  const marker=decodeCanonical(dec.decode(actors[l].store.read('membership','quiescence'))),acks=[];
  for(const a of actors)acks.push(await a.sleepAck(marker.index,marker.term));
  let e=await actors[l].schedule(60000,1030003,false,acks.slice(0,2));accept(e);await pump();assert.equal(e.plan.sleeping,false);
  for(let n=0;n<actors.length;n++){
   const alarms=new MemberAlarms(hosts[n],actors[n],async e=>{accept(e)},async()=>false);
   e=await alarms.run(60000,1030003,acks);await pump();assert.equal(e.plan.sleeping,true);assert.equal(hosts[n].alarms.at(-1),null);
  }
  // One cold member cannot reinstate the old certificate after eviction.
  const cold=(l+1)%3,old=actors[cold],host=hosts[cold],evaluate=old.evaluate;
  await old.close();actors[cold]=new MemberActor(host,runtime,evaluate);accept(await actors[cold].init(configs[cold]));await pump();
  e=await actors[cold].schedule(0,1030004,false,acks);accept(e);await pump();assert.equal(e.plan.sleeping,false);
  accept(await actors[l].input(4,'first-wake-read'));await pump();
  e=await actors[l].schedule(60001,1030004,false,acks);accept(e);await pump();assert.equal(e.plan.sleeping,false);assert.equal(e.plan.wake.length,3);
  r=await submit(command({acquire:{cell:'chat',force:false,expect:{device:'a',fence:'1'}}},1030005,'b'));assert.equal(r.code,'ok');assert.equal(r.view.cell.lease.fence,'2');
  r=await submit(command({release:{cell:'chat',fence:'2',quiet:false}},1030006,'b'));assert.equal(r.code,'ok');
  let sleeps=0,barriers=0;
  const driver=new QuiescenceCoordinator({
   maintain:async()=>false,
   schedule:(elapsed,now,busy)=>actors[leader()].schedule(elapsed,now,busy),
   dispatch:async e=>{accept(e);await pump()},
   wake:async voters=>assert.equal(voters.length,3),
   idleBarrier:async()=>{barriers++;accept(await actors[leader()].input(15,'idle-auto'));await pump()},
   prepare:async()=>{
    const s=actors[leader()].status;
    const r=await submit(command({prepare_quiescence:{barrier:String(s.applied),term:String(s.term),voters:['1','2','3']}},1030007,'anchor'));assert.equal(r.code,'ok');
    accept(await actors[leader()].input(1));await pump();return decodeCanonical(dec.decode(actors[leader()].store.read('membership','quiescence')));
   },
   appliedAcks:async m=>{const a=[];for(const actor of actors)a.push(await actor.sleepAck(m.index,m.term));return a},
   finish:async(m,acks,elapsed,now)=>{for(const actor of actors){const e=await actor.schedule(elapsed,now,false,acks);accept(e);await pump();assert(e.plan.sleeping);sleeps++}},
  });
  const automatic=await driver.run(120002,1030007);assert(automatic.prepare);assert.equal(barriers,1);assert.equal(sleeps,3);
  const start=calls.length;assert(start>100);for(const [request,expected]of calls)assert.deepEqual(await native.call(request),expected,'R14 native/WASI scheduling divergence');
 }finally{await native.close();for(const h of hosts)h.db.close()}
});
