import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync,createHash,sign } from 'node:crypto';
import { encodeCanonical } from '../../src/raft/codec.js';
import { hostedFixture } from './hosted-fixture.js';

const hash=b=>createHash('sha256').update(b).digest('hex');
const encoded=v=>Buffer.from(encodeCanonical(v));
// R16/R18 use public ingress and original signatures, never direct actor apply.
// The same scoped reads recheck expiry/incarnation after a new quorum barrier.
test('R16 R18 production hosted scoped ingress preserves grants and conditional claims',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'sealed',class:'work',size:0,keys:{}})).status,200);
  assert.equal((await h.call(a,'POST','/v1/dir/cells/chat/release',{fence:1})).status,204);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  const key=generateKeyPairSync('ed25519'),pub=key.publicKey.export({format:'der',type:'spki'}).subarray(-32),worker='dev_'+hash(pub).slice(0,32);
  const cert=h.delegate({cluster,key:pub.toString('base64'),made:Date.now()-1000,expires:Date.now()+600000,cells:['chat'],learner:false});
  const w={key,device:worker};let incarnation='0';const sequence={};
  const command=(c,change)=>{
   const n=sequence[c.device]=(sequence[c.device]??0)+1;
   const cmd={schema:1,cluster,actor:c.device,session:'integration',sequence:String(n),...(n>1?{ack:String(n-1)}:{}),now:Date.now(),change,request_digest:'',signature:null};
   if(c===w&&incarnation!=='0')cmd.worker_incarnation=incarnation;
   if(c===a)cmd.certificate=Buffer.from(JSON.stringify(a.cert)).toString('base64url');
   cmd.request_digest=hash(encoded({...(cmd.worker_incarnation?{worker_incarnation:cmd.worker_incarnation}:{}),cluster,actor:cmd.actor,session:cmd.session,sequence:cmd.sequence,change}));
   cmd.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),encoded(cmd)]),c.key.privateKey).toString('base64');return cmd;
  };
  const api=(c,q)=>h.api(c,{cluster,incarnation:c===w?incarnation:'0',...(c===w?{worker:cert}:{}),...q});
  const submit=async(c,change,want='ok')=>{const r=await api(c,{action:'submit',command:command(c,change)});assert.equal(r.status,200,JSON.stringify(r));assert.equal(r.body.result.code,want);return r.body.result;};
  await submit(a,{enroll_worker:{certificate:cert,grants:{chat:'gYKD'}}});
  await submit(w,{register_worker:{previous:'0',incarnation:'1',caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''},sealed_labels:null}});incarnation='1';
  const selected=await api(w,{action:'cell',cell:'chat'});assert.equal(selected.status,200,JSON.stringify(selected));assert.equal(selected.body.view.cell.head,'sealed');assert.equal(selected.body.grant,'gYKD');
  assert.notEqual((await api(w,{action:'cell',cell:'outside'})).status,200);
  const beat={worker,incarnation:'1',sequence:'1',seen_at:Date.now(),work:null,signature:null};beat.signature=sign(null,Buffer.from('codeaf/worker-beat/v1\0'+encodeCanonical({cluster,beat})),key.privateKey).toString('base64');
  await submit(w,{worker_beat_batch:{items:[beat]}});
  const spec={id:'opaque',revision:'1',cell:'chat',parent_cell:'',expect:{device:a.device,fence:'1'},target:worker,incarnation:'1',expires:Date.now()+60000,sealed_payload:'kQ=='};
  await submit(a,{assign:spec});
  const ref={id:'opaque',revision:'1',cell:'chat',target:worker,incarnation:'1'};
  const pending=await api(w,{action:'assignment',assignment:ref});assert.equal(pending.status,200,JSON.stringify(pending));assert.equal(pending.body.assignment.status,'pending');
  const claim=command(w,{claim_assignment:{ref,expect:spec.expect}});
  const injected={...claim,certificate:'private-field',signature:null};
  injected.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),encoded(injected)]),key.privateKey).toString('base64');
  assert.notEqual((await api(w,{action:'submit',command:injected})).status,200,'R15 invalid certificate reached the log');
  for(let n=0;n<2;n++){const r=await api(w,{action:'submit',command:claim});assert.equal(r.status,200);assert.equal(r.body.result.view.cell.lease.fence,'2');}
  assert.equal((await h.call(a,'POST','/v1/dir/cells/chat/acquire',{force:true,expect:{device:worker,fence:2}})).status,200);
  const current=await api(w,{action:'assignment',assignment:ref});assert.equal(current.status,200);assert.equal(current.body.assignment.fence,'2');assert.equal(current.body.view.cell.lease.fence,'3');
  await submit(w,{rotate:{op:'freeze'}},'unauthorized');
  incarnation='2';assert.notEqual((await api(w,{action:'cell',cell:'chat'})).status,200);incarnation='1';
  await submit(a,{drain_worker:{worker,incarnation:'1'}});
  await h.start();assert.equal((await api(w,{action:'cell',cell:'chat'})).status,200,'restart lost selected grant');
  assert.equal((await h.call(a,'POST','/v1/dir/devices/'+worker+'/revoke')).status,204);
  assert.notEqual((await api(w,{action:'cell',cell:'chat'})).status,200);
  const delayed=await api(w,{action:'submit',command:claim});assert.equal(delayed.status,200);assert.equal(delayed.body.result.code,'revoked');
 }finally{await h.close();}
});

// R23/R26/R27 advance ten idle minutes without sleeping the laptop. The fixture
// clock driver is internal; every counted member call is production work.
test('R23 R26 R27 production hosted ten-minute idle has no recurring member work',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  // R26 recovers the same durable identity before starting the idle clock.
  await h.start();
  const measured=await h.control({op:'idle-schedule'});
  assert.equal(measured.sleeping,true,JSON.stringify(measured));
  assert.equal(measured.idleMilliseconds,600000);assert.equal(measured.recurringMemberCalls,0);assert.equal(measured.appliedBefore,measured.appliedAfter);
  console.log('R23 R27 ten-minute idle',JSON.stringify(measured));
 }finally{await h.close();}
});
