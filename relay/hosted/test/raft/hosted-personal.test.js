import test from 'node:test';
import assert from 'node:assert/strict';
import {sign,createHash} from 'node:crypto';
import {hostedFixture} from './hosted-fixture.js';
const enc=new TextEncoder(),sha=b=>createHash('sha256').update(b).digest('hex');
const token=n=>n.toString(16).padStart(64,'0');
async function enroll(h,c,store=token(1),protocol=1){const r=await h.request(c,'POST','/v2/cluster/enroll',{store,protocol});assert.equal(r.status,200,await r.clone().text());return r.json();}
async function joined(h,c,store=token(1)){await enroll(h,c,store);return enroll(h,c,store);}
function signed(c,g,{messages=null,applied,ack=['0','0','0'],generation=g.generation}={}){
 const r={...(applied?{applied:String(applied)}:{}),scope:g.scope,node:g.node,incarnation:g.incarnation,generation,messages,signature:null,ack};
 r.signature=sign(null,Buffer.from('codeaf/raft/exchange/1\n'+JSON.stringify(r)),c.key.privateKey).toString('base64');return JSON.stringify(r);
}
async function exchange(h,raw){return h.mf.dispatchFetch('http://local/v2/cluster/exchange',{method:'POST',body:raw});}
function envelope(c,g,payload,sequence='1'){
 const e={scope:g.scope,from:g.node,to:'1',incarnation:g.incarnation,destination:'1',sequence,chunk:null,digest:sha(payload),payload:Buffer.from(payload).toString('base64'),signature:null};
 e.signature=sign(null,Buffer.from('codeaf/raft/envelope/1\n'+JSON.stringify(e)),c.key.privateKey).toString('base64');return e;
}
const nextFrame=ws=>new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('no peer response')),5000);ws.addEventListener('message',e=>{clearTimeout(timer);resolve(JSON.parse(new TextDecoder().decode(e.data)));},{once:true});});

// R9/R12/R16 preserve admission proof, retry identity and permanent retirement.
test('R9 R12 R16 hosted admission retries, wipes, protocol and revoked keys',{timeout:60000},async()=>{
 const h=await hostedFixture(),a=h.client(),b=h.client();
 try{
  await h.start();const first=await joined(h,a),same=await joined(h,a);assert.equal(same.node,first.node);assert.equal(same.barrier,first.barrier);
  const replacement=await joined(h,a,token(2));assert(BigInt(replacement.node)>BigInt(first.node));
  let r=await h.request(a,'POST','/v2/cluster/enroll',{store:token(1),protocol:1});assert.equal(r.status,403);await r.text();
  r=await exchange(h,signed(a,first));assert.equal(r.status,403);await r.text();
  r=await h.request(a,'POST','/v2/cluster/enroll',{store:token(3),protocol:2});assert.equal(r.status,409);assert.equal((await r.text()).trim(),'this codeaf build must be updated before it can join this cluster; your saved chats are still here');
  await h.call(b,'PUT','/v1/dir/devices/'+b.device,{V:1,name:'sealed'});
  const other=await joined(h,b,token(4));assert.notEqual(other.node,replacement.node);
  assert.equal((await h.call(a,'POST','/v1/dir/devices/'+b.device+'/revoke')).status,204);
  r=await h.request(b,'POST','/v2/cluster/enroll',{store:token(5),protocol:1});assert.equal(r.status,403);assert.equal((await r.text()).trim(),'this computer is not allowed to join this cluster');
  r=await exchange(h,signed(b,other));assert.equal(r.status,403);await r.text();
  const worker=h.client();worker.cert=h.delegate({cluster:replacement.scope.cluster,key:worker.key.publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64'),made:Date.now()-1000,expires:Date.now()+60000,cells:['chat'],learner:true});r=await h.request(worker,'POST','/v2/cluster/enroll',{store:token(6),protocol:1});assert.equal(r.status,403);await r.text();
 }finally{await h.close();}
});

// R9/R27 count actual live admissions; no record is deleted to admit the 33rd.
test('R9 R27 hosted learner capacity is 32',{timeout:120000},async()=>{
 const h=await hostedFixture();
 try{
  await h.start();let last;
  for(let n=1;n<=32;n++)last=await joined(h,h.client(),token(n));
  const r=await h.request(h.client(),'POST','/v2/cluster/enroll',{store:token(33),protocol:1});assert.equal(r.status,429);assert.equal((await r.text()).trim(),'this cluster has reached its enrollment limit; remove a joined computer or ask the relay operator for capacity');
  assert.equal(last.node,'35');
 }finally{await h.close();}
});

// R9/R12 recover configuration from committed admission without another request.
test('R9 R12 hosted activation repairs the enrollment/configuration cut',{timeout:30000},async()=>{
 const h=await hostedFixture(),a=h.client();
 try{
  await h.start({needle:'this.membershipPending=true;',after:"throw new Error('interrupted admission');"});
  const r=await h.request(a,'POST','/v2/cluster/enroll',{store:token(1),protocol:1});assert.equal(r.status,500);await r.text();
  await h.start();const status=await h.call(a,'GET','/v2/cluster/control');assert.equal(status.status,200,JSON.stringify(status));assert.equal(status.body.self,'4');assert.deepEqual(status.body.configuration.learners,['4']);assert.equal(status.body.membership.next_node,'5');assert(!status.body.membership.pending);
  const grant=await enroll(h,a);assert.equal(grant.node,'4');assert.notEqual(grant.barrier,'0');
 }finally{await h.close();}
});

// R23 validates the native wire, cursor bounds, authentication and durable fences.
test('R23 hosted exchange and hibernating sockets reject foreign, local and stale traffic',{timeout:60000},async()=>{
 const h=await hostedFixture(),a=h.client();
 try{
  await h.start();const g=await joined(h,a);
  let r=await exchange(h,signed(a,g,{ack:['999','0','0']}));assert.equal(r.status,409);assert.deepEqual(await r.json(),{error:'cursor'});
  for(const type of [0,1,2,11,15,16,19,20,21,22]){
   const payload=Uint8Array.of(8,type,16,1,24,Number(g.node),32,1),e=envelope(a,g,payload,String(type+1));
   r=await exchange(h,signed(a,g,{messages:[e]}));assert.equal(r.status,400,'local/proposal/storage type '+type);await r.text();
  }
  r=await exchange(h,signed(a,g,{applied:(1n<<64n)-1n}));assert.equal(r.status,400);await r.text();
  r=await exchange(h,signed(h.client(),g));assert.equal(r.status,403);await r.text();
  const foreign={...g,scope:{...g.scope,identity:'id_'+'e'.repeat(32)}};
  r=await exchange(h,signed(a,foreign));assert.equal(r.status,403);await r.text();
  r=await exchange(h,signed(a,g)+' ');assert.equal(r.status,400);await r.text();
  const raw=signed(a,g),headers={Upgrade:'websocket','Codeaf-Cluster-Auth':Buffer.from(raw).toString('base64url')};
  const peer=await h.mf.dispatchFetch('http://local/v2/cluster/socket',{headers});assert.equal(peer.status,101);const ws=peer.webSocket;ws.accept();const initial=await nextFrame(ws);assert.equal(initial.generation,g.generation);
  const appResponse=Uint8Array.of(8,4,16,1,24,Number(g.node),32,1);
  const busy=[1,2,3].map(n=>envelope(a,g,appResponse,String(n)));
  const rejected=nextFrame(ws);ws.send(enc.encode(signed(a,g,{messages:busy})));assert.deepEqual((await rejected).rejected,['1','2','3']);
  const live=nextFrame(ws);ws.send(enc.encode(signed(a,g)));assert.equal((await live).generation,g.generation,'backpressure closed the peer socket');

  const forward=await h.mf.dispatchFetch('http://local/v2/cluster/forward',{headers});assert.equal(forward.status,101);forward.webSocket.accept();forward.webSocket.close();ws.close();
  await h.start();r=await exchange(h,signed(a,g));assert.equal(r.status,409);assert.deepEqual(await r.json(),{error:'generation'});
  const refreshed=await enroll(h,a);assert(BigInt(refreshed.generation)>BigInt(g.generation));assert.equal(refreshed.node,g.node);
 }finally{await h.close();}
});

// R9/R27 a vanished learner cannot gate public commitment, compaction or sleep.
test('R9 R26 R27 stalled hosted learner cannot keep voters awake or delay a write',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();const grant=await joined(h,a);
  assert.equal((await h.call(a,'POST','/v1/dir/cells/while-gone',{head:'saved',class:'work',size:0,keys:{}})).status,200);
  assert.equal((await h.call(a,'POST','/v1/dir/cells/while-gone/release',{fence:1})).status,204);
  const read=await h.call(a,'GET','/v1/dir/cells/while-gone');assert.equal(read.body.cell.head,'saved');
  const socket=await h.mf.dispatchFetch('http://local/v2/cluster/socket',{headers:{Upgrade:'websocket','Codeaf-Cluster-Auth':Buffer.from(signed(a,grant)).toString('base64url')}});assert.equal(socket.status,101);
  const dying=socket.webSocket;dying.accept();dying.send(enc.encode(signed(a,grant,{messages:[envelope(a,grant,Uint8Array.of(8,9,16,1,24,Number(grant.node),32,1))]})));dying.close();
  assert.equal((await h.call(a,'POST','/v1/dir/vault',{old:'',new:'after-interrupted-peer'})).status,204);

  const idle=await h.control({op:'idle-schedule'});assert.equal(idle.sleeping,true);assert.equal(idle.recurringMemberCalls,0);assert.equal(idle.appliedBefore,idle.appliedAfter);assert.equal(idle.membershipCalls,0,'settled accounts ran recurring membership reconciliation');
  const quiet=await exchange(h,signed(a,grant));assert.equal(quiet.status,200);assert.deepEqual((await quiet.json()).messages,[]);
  const asleep=await h.control({op:'peer-idle'});assert.equal(asleep.sleeping,true);assert.equal(asleep.snapshots,0);assert.deepEqual(asleep.bytes,[0,0,0]);
  assert.equal((await h.call(a,'POST','/v1/dir/vault',{old:'after-interrupted-peer',new:'after-sleep'})).status,204);
  const list=await h.call(a,'GET','/v1/dir/list');assert.equal(list.body.identity.vault,'after-sleep');
 }finally{await h.close();}
});

// R23 refuses unknown identities without activating storage, and peer delivery
// remains independent of a public call waiting on its own coordination queue.
test('R23 hosted unknown peer creates no storage and public waits do not block peers',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();const scope={identity:h.identity,cluster:'unknown',epoch:'1',protocol:1},g={scope,node:'4',incarnation:'1',generation:'1'};
  const before=await h.control({op:'transport-state'});
  let r=await exchange(h,signed(a,g));assert.equal(r.status,403);await r.text();
  assert.deepEqual(await h.control({op:'transport-state'}),before);assert.deepEqual(before.keys,[]);assert.deepEqual(before.tables,[]);
  const grant=await joined(h,a);await h.control({op:'pause-public'});
  try{const heartbeat=envelope(a,grant,Uint8Array.of(8,9,16,1,24,Number(grant.node),32,1));r=await exchange(h,signed(a,grant,{messages:[heartbeat]}));assert.equal(r.status,200,await r.clone().text());assert.deepEqual((await r.json()).accepted,['1']);}finally{await h.control({op:'resume-public'});}
  const compacted=await h.control({op:'checkpoint-state'});assert.equal(compacted.retained,0);assert.equal(compacted.snapshot,compacted.applied);
 }finally{await h.close();}
});

// R23 concurrent senders and coordinator recovery cannot reuse a signed sequence.
test('R23 hosted outbound sequences stay unique across concurrency and recovery',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  const before=await h.control({op:'sequence-proof'});assert.equal(new Set(before.sequences).size,before.sequences.length);
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  const after=await h.control({op:'sequence-proof'});assert.equal(new Set(after.sequences).size,after.sequences.length);assert(BigInt(after.sequences[0])>BigInt(before.sequences.at(-1)));
  console.log('R23 durable sequence ceiling',before.ceiling,after.ceiling);
 }finally{await h.close();}
});
