import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { mkdtemp, writeFile, copyFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generateKeyPairSync, createHash, sign } from 'node:crypto';
import { encodeCanonical } from '../../src/raft/codec.js';
import { openWorkerd } from './workerd.js';

// R11/R12/R16 exercise the ACTUAL hosted peer and committed-registry doors.
// A learner remains absent during writes/compaction and both process restarts.
test('R9 R11 R12 R13 R16 R26 R27 actual workerd admits only committed learners and retains retired authority across restarts',{timeout:45000},async()=>{
 const proof=process.env.CODEAF_RAFT_PROOF_DIR??'/tmp/codeaf-p12',out=await mkdtemp(join(tmpdir(),'p12-wasm-')),disk=await mkdtemp(join(tmpdir(),'p12-disk-'));
 const bundle=await build({entryPoints:['test/raft/membership-worker.js'],bundle:true,format:'esm',target:'es2022',external:['core.wasm','cloudflare:*'],write:false});
 await writeFile(join(out,'worker.js'),bundle.outputFiles[0].contents);await copyFile(join(proof,'core.wasm'),join(out,'core.wasm'));await writeFile(join(out,'storage-worker.js'),'export class ProofStore{}');
 const keys=Object.fromEntries(['a','b','1','2','3'].map(id=>[id,generateKeyPairSync('ed25519')])),pub=id=>keys[id].publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 const cluster='p12-workerd',sha=b=>createHash('sha256').update(b).digest('hex'),bytes=v=>Buffer.from(encodeCanonical(v)),scope={identity:cluster,cluster,epoch:'1',protocol:1};
 const anchors=['1','2','3'].map(node=>({node,device:'',key:pub(node),incarnation:'1',protocol:1,anchor:true,retired:false,ready_index:'0'}));
 const genesis=['a','b'].flatMap(id=>[{namespace:'enrollment',key:'actor/'+Buffer.from(id).toString('hex'),data:bytes({cluster,key:pub(id),personal:true}).toString('base64')},{namespace:'state',key:'device/'+Buffer.from(id).toString('hex'),data:bytes({V:1,name:'',added_by:'',revoked:false,caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''}}).toString('base64')}]);
 genesis.push({namespace:'state',key:'identity',data:bytes({V:1,identity:'test'}).toString('base64')});
 let server,transport=0;const sequences={};const status={},queue=[];
 const restart=async()=>{if(server)await server.dispose();server=await openWorkerd(out,disk,true);};
 const call=async(id,op,extra={})=>{const r=await fetch(server.origin+'/'+id,{method:'POST',body:JSON.stringify({op,...extra}),signal:AbortSignal.timeout(5000)});assert.equal(r.status,200);const v=await r.json();if(v?.error)throw Error(v.error);return v;};
 const accept=(id,r)=>{if(r?.status)status[id]=r.status;queue.push(...(r?.send??[]).map(m=>({...m,from:id})));};
 const pump=async()=>{for(let n=0;queue.length;n++){assert(n<2000);const m=queue.shift();if(Number(m.to)>3)continue;
  const e={scope,from:String(m.from),to:String(m.to),incarnation:'1',destination:'1',sequence:String(++transport),chunk:null,digest:sha(Buffer.from(m.bytes,'base64')),payload:m.bytes,signature:null};
  e.signature=sign(null,Buffer.from('codeaf/raft/envelope/1\n'+JSON.stringify(e)),keys[e.from].privateKey).toString('base64');
  accept(Number(m.to),await call(m.to,'peer',{value:Buffer.from(JSON.stringify(e)).toString('base64')}));
 }};
 const leader=()=>Number(Object.keys(status).find(id=>status[id].role===2));
 const command=(change,signer='a')=>{const actor=signer==='a'?'a':'anchor/'+signer,n=sequences[actor]=(sequences[actor]??0)+1,c={schema:1,cluster,actor,session:'api',sequence:String(n),request_digest:'',now:1000,change,signature:null};if(n>1)c.ack=String(n-1);c.request_digest=sha(bytes({cluster,actor,session:'api',sequence:c.sequence,change}));c.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),bytes(c)]),keys[signer].privateKey).toString('base64');return bytes(c).toString('base64');};
 const submit=async raw=>{const i=leader(),before=BigInt(status[i].applied);accept(i,await call(i,'input',{tag:3,value:raw}));await pump();assert(BigInt(status[i].applied)>before,'dead learner stalled commitment');};
 const header=()=>call(leader(),'header');
 const settle=async()=>{const i=leader();accept(i,await call(i,'membership'));await pump();const p=await call(i,'membership');accept(i,p);await pump();assert(p.settle);const h=await header();await submit(command({settle_membership:{revision:h.revision,node:h.pending.node}},String(i)));};
 const tick=async()=>{for(const id of [1,2,3])accept(id,await call(id,'input',{tag:1}));await pump();};
 try{
  await restart();for(const id of [1,2,3])accept(id,await call(id,'init',{config:{cluster,id:String(id),seed:String(id+20),bootstrap:['1','2','3'],anchors,scope,genesis}}));
  await pump();for(let n=0;n<30&&!leader();n++)await tick();assert(leader());
  await assert.rejects(call(leader(),'enrollment',{view:{},applied:'0'}),/absent/);
  await submit(command({init_membership:{anchors,limit:4096}}));
  // R26/R27 use actual DO storage, committed Go markers and the production
  // scheduler. Partial final delivery and process eviction cannot confer reads.
  for(const id of [1,2,3])accept(id,await call(id,'schedule',{elapsed:0,now:1000,busy:false,acks:[]}));await pump();
  const asleepLeader=leader();
  accept(asleepLeader,await call(asleepLeader,'input',{tag:4,value:'idle-barrier'}));await pump();
  await submit(command({prepare_quiescence:{barrier:status[leader()].applied,term:status[leader()].term,voters:['1','2','3']}},String(leader())));
  await tick();
  const marker=JSON.parse(Buffer.from(await call(leader(),'row',{namespace:'membership',key:'quiescence'}),'base64').toString());
  const acks=[];for(const id of [1,2,3])acks.push(await call(id,'sleep-ack',{index:marker.index,term:marker.term}));
  let timed=await call(leader(),'turn',{elapsed:60000,now:Date.now(),busy:false,acks:acks.slice(0,2)});accept(leader(),timed);await pump();assert.equal(timed.plan.sleeping,false);assert(await call(leader(),'alarm-state'));
  for(const id of [1,2,3]){timed=await call(id,'turn',{elapsed:60000,now:Date.now(),busy:false,acks});accept(id,timed);assert.equal(timed.plan.sleeping,true);assert.equal(await call(id,'alarm-state'),null)}await pump();
  for(const id of [1,2,3]){await call(id,'fire-alarm');assert.equal(await call(id,'alarm-state'),null)}
  accept(leader(),await call(leader(),'input',{tag:4,value:'wake-before-alarm'}));await pump();
  const queuedAt=leader();await call(queuedAt,'fire-alarm');const queued=await call(queuedAt,'scheduled');assert(queued?.id);assert.equal(queued.wake.length,3);
  await call(queuedAt,'fire-alarm');assert.deepEqual(await call(queuedAt,'scheduled'),queued);
  await call(queuedAt,'ack-scheduled',{id:'wrong'});assert.deepEqual(await call(queuedAt,'scheduled'),queued);
  await restart();assert.deepEqual(await call(queuedAt,'scheduled'),queued);await call(queuedAt,'ack-scheduled',{id:queued.id});assert.equal(await call(queuedAt,'scheduled'),null);
  const maxPacket=Buffer.alloc(64*1024,7).toString('base64');await call(queuedAt,'retain-fixture',{send:[{to:'1',bytes:maxPacket},{to:'2',bytes:maxPacket}]});
  const large=await call(queuedAt,'scheduled');assert.equal(large.send.length,2);assert.equal(large.send[0].bytes,maxPacket);assert(JSON.stringify(large).length>64*1024);
  await restart();assert.deepEqual(await call(queuedAt,'scheduled'),large);await call(queuedAt,'ack-scheduled',{id:large.id});assert.equal(await call(queuedAt,'scheduled'),null);
for(const id of [1,2,3])accept(id,await call(id,'status'));await pump();
  for(const id of [1,2,3]){timed=await call(id,'schedule',{elapsed:0,now:1000,busy:false,acks});accept(id,timed);assert.equal(timed.plan.sleeping,false)}await pump();
  for(let n=0;n<30&&!leader();n++)await tick();assert(leader());
  let h=await header();const enrolled=command({enroll_member:{node:h.next_node,revision:h.revision,device:'b',protocol:1}});await submit(enrolled);await settle();
  let view=await call(leader(),'registry');assert.equal(view['4'].voter,false);assert.equal(view['4'].revoked,false);
  await submit(command({set_vault:{old:'',new:'sealed-ref'}}));await call(leader(),'checkpoint');
  await restart();for(const id of [1,2,3])accept(id,await call(id,'status'));await pump();for(let n=0;n<30&&!leader();n++)await tick();assert(leader());
  h=await header();assert.equal(h.next_node,'5');view=await call(leader(),'registry');assert.equal(view['4'].voter,false);
  await submit(enrolled);assert.equal((await header()).next_node,'5');
  await assert.rejects(call(leader(),'enrollment',{view:{...view,'4':{...view['4'],voter:true}},applied:status[leader()].applied}),/differs/);
  h=await header();await submit(command({remove_member:{revision:h.revision,node:'4',device:'b'}}));await settle();
  assert.equal((await call(leader(),'registry'))['4'].revoked,true);await call(leader(),'checkpoint');
  await restart();for(const id of [1,2,3])accept(id,await call(id,'status'));await pump();for(let n=0;n<30&&!leader();n++)await tick();
  assert(await call(leader(),'row',{namespace:'tombstones',key:'node/4'}));assert.equal((await call(leader(),'registry'))['4'].revoked,true);
  h=await header();await submit(command({enroll_member:{node:h.next_node,revision:h.revision,device:'b',protocol:1}}));await submit(command({revoke:{device:'b'}}));assert.equal((await call(leader(),'registry'))['5'].revoked,true);
 }finally{if(server)await server.dispose();await rm(out,{recursive:true,force:true});await rm(disk,{recursive:true,force:true});}
});
