import test from 'node:test';
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { encode } from '../../src/frame.js';
import { setTimeout as delay } from 'node:timers/promises';
import { hostedFixture } from './hosted-fixture.js';
// H1/H3/R25 drive the exported Worker rather than internal fixture controls.
// The two independently certified clients never possess an anchor/effects key.
test('H1 H3 R1 R16 R25 public Worker commits v1 compatibility through three anchors',{timeout:60000},async()=>{
 const h=await hostedFixture();const {identity,client,call}=h;const a=client(),b=client();
 try{
  await h.start();
  const device={V:1,name:'c2VhbGVk',added_by:identity,revoked:false,caps:{os:'linux',arch:'amd64',sandbox:null,container:null,gpu:null,cow:'none'}};
  for(const c of [a,b]){const r=await call(c,'PUT','/v1/dir/devices/'+c.device,device);assert.equal(r.status,204,JSON.stringify(r));}
  const init={head:'saved',class:'work',size:7,keys:{key:{[identity]:'wrapped'}},frames:['sealed-frame']};
  const created=await call(a,'POST','/v1/dir/cells/chat',init);assert.equal(created.status,200,JSON.stringify(created));assert.equal(created.body.cell.lease.fence,1);
  const blind=await call(b,'POST','/v1/dir/cells/chat/acquire',{force:true});assert.deepEqual(blind,{status:409,body:{err:'lease_held'}});
  const observed={device:a.device,fence:1},raced=await Promise.all([b,a].map(c=>call(c,'POST','/v1/dir/cells/chat/acquire',{force:true,expect:observed})));
  assert.deepEqual(raced.map(r=>r.status).sort(),[200,409]);assert.equal(raced.find(r=>r.status===409).body.err,'lease_held');
  const owner=raced[0].status===200?b:a,winner=raced.find(r=>r.status===200);assert.equal(winner.body.cell.lease.fence,2);
  assert.equal((await call(a,'POST','/v1/dir/cells/chat/publish',{fence:1,old_head:'saved',head:'stale',size:8})).status,409);
  {const r=await call(owner,'POST','/v1/dir/cells/chat/publish',{fence:2,old_head:'saved',head:'winner',size:8,frames:[]});assert.equal(r.status,200,JSON.stringify(r));}
  assert.equal((await call(owner,'POST','/v1/dir/cells/chat/release',{fence:2})).status,204);
  await h.start();
  const cell=await call(a,'GET','/v1/dir/cells/chat');assert.equal(cell.status,200,JSON.stringify(cell));assert.equal(cell.body.cell.head,'winner');assert.equal(cell.body.cell.lease.expires,0);
  assert.equal((await call(a,'POST','/v1/dir/cells/chat/acquire',{force:true})).status,200);
  assert.equal((await call(a,'POST','/v1/dir/devices/'+b.device+'/revoke')).status,204);
  assert.deepEqual(await call(b,'GET','/v1/dir/list'),{status:401,body:{err:'revoked'}});
 }finally{await h.close();}
});

// H1/H2/R3/R8/R24 forbid recreating a published voter from the source backup.
test('H1 H2 R3 R8 R24 public Worker excludes lost stores and refuses a lone voter',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();const made=await h.call(a,'POST','/v1/dir/cells/chat',{head:'saved',class:'work',size:0,keys:{}});assert.equal(made.status,200);
  const source=await h.control({op:'source'}),cluster=JSON.parse(source.gate).config.cluster;
  await h.control({op:'discard-member',cluster,node:'1'});await h.start();
  const won=await h.call(a,'POST','/v1/dir/cells/chat/acquire',{force:true,expect:{device:a.device,fence:1}});assert.equal(won.status,200,JSON.stringify(won));assert.equal(won.body.cell.lease.fence,2);
  assert.equal(await h.control({op:'has-member',cluster,node:'1'}),false);
  await h.control({op:'discard-member',cluster,node:'2'});await h.start();
  assert.equal((await h.call(a,'GET','/v1/dir/cells/chat')).status,503);
  for(const node of ['1','2'])assert.equal(await h.control({op:'has-member',cluster,node}),false);
  assert.equal(JSON.parse((await h.control({op:'source'})).gate).stage,'raft-owned');
 }finally{await h.close();}
});

// H1/R16/R21/R25 route both blob authorization and rotation through the same
// committed namespace, including the production /fabric signing domain.
test('H1 H3 R16 R21 R25 rotation guards blob writes and erases every replica after grace',{timeout:20000},async()=>{
 const h=await hostedFixture({fixture:true,base:'/fabric'}),a=h.client(),b=h.client();
 try{
  await h.start();const rid='a'.repeat(64),raw=new TextEncoder().encode('AGEO\x01sealed');
  const frame=encode('0'.repeat(32),[{rid,bytes:raw}]);
  assert.equal((await h.call(a,'POST','/v1/store/frames',frame)).status,200);
  assert.equal((await h.request(a,'GET','/v1/store/objects/'+rid)).status,200);
  const rotate=(c,op)=>h.call(c,'POST','/v1/identity/rotation',{V:1,op,...(op==='retire'?{grace_ms:2000}:{})});
  assert.equal((await rotate(a,'freeze')).status,200);
  assert.deepEqual(await h.call(b,'POST','/v1/store/frames',frame),{status:410,body:{err:'rotated'}});
  assert.equal((await h.request(a,'GET','/v1/store/objects/'+rid)).status,200);
  assert.equal((await rotate(b,'thaw')).status,200);
  assert.equal((await rotate(a,'freeze')).status,200);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  for(const node of ['1','2','3'])await h.control({op:'seed-transport-tail',cluster,node});
  const retired=await rotate(a,'retire');assert.equal(retired.status,200);assert.equal(retired.body.rotation.retire_at,retired.body.now+2000);
  const deadline=Date.now()+8000;let gone;
  do{await delay(50);gone=await h.call(a,'GET','/v1/dir/list');}while(gone.status!==410&&Date.now()<deadline);
  assert.equal(gone.status,410,JSON.stringify(gone));
  // Gone is published before external deletion; wait for the resumable owner
  // erasure workflow to finish all stores rather than infer it from HTTP 410.
  let complete=false;
  while(Date.now()<deadline){complete=true;for(const node of ['1','2','3'])if(await h.control({op:'has-member',cluster,node}))complete=false;if(complete)break;await delay(50);}
  assert(complete,'retirement left a replica/snapshot behind');
  for(const node of ['1','2','3'])assert.deepEqual(await h.control({op:'retained-control',cluster,node}),[]);
  assert.equal((await h.call(a,'POST','/v1/store/frames',frame)).status,410);
 }finally{await h.close();}
});

// H3/R16/R21/R25 keep the old public pairing grant door, committing device
// authority before the public request can expose its sealed grant.
test('H3 R16 R21 R25 public v1 link approval and denial use committed authority',{timeout:30000},async()=>{
 const h=await hostedFixture(),a=h.client(),b=h.client();
 try{
  await h.start();
  const open=async()=>{
   const r=await h.mf.dispatchFetch('http://local/v1/link/requests',{method:'POST',body:JSON.stringify({pubkey:Buffer.from(b.cert.device,'hex').toString('base64url'),x25519:randomBytes(32).toString('base64url'),name_sealed:randomBytes(45).toString('base64url'),platform:'linux'})});assert.equal(r.status,201);return r.json();
  };
  const made=await open(),grant=randomBytes(64).toString('base64url'),record={V:1,name:'c2VhbGVk',platform:'linux'};
  const approval={device:record,cert:Buffer.from(JSON.stringify(b.cert)).toString('base64url'),grant};
  const accepted=await h.call(a,'POST','/v1/dir/requests/'+made.code+'/approve',approval);assert.equal(accepted.status,204,JSON.stringify(accepted));
  const read=await h.mf.dispatchFetch('http://local/v1/link/requests/'+made.code),view=await read.json();assert.equal(view.state,'approved');assert.equal(view.grant,grant);
  const list=await h.call(b,'GET','/v1/dir/list');assert.equal(list.status,200);assert.equal(list.body.devices[b.device].name,record.name);
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/requests/'+made.code+'/approve',approval)).status,204);
  const denied=await open();assert.equal((await h.call(a,'POST','/v1/dir/requests/'+denied.code+'/deny')).status,204);
  assert.equal((await (await h.mf.dispatchFetch('http://local/v1/link/requests/'+denied.code)).json()).state,'denied');
 }finally{await h.close();}
});

// H1/H2/R13 retain a healthy member when delayed transport carries a snapshot
// older than the checkpoint it has already durably applied.
test('H1 H2 R13 delayed old snapshot cannot exclude a caught-up hosted member',{timeout:20000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'current',class:'work',size:0,keys:{}})).status,200);
  assert.equal(await h.control({op:'stale-snapshot'}),3);
  const row=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(row.status,200);assert.equal(row.body.cell.head,'current');
 }finally{await h.close();}
});

// H2/R14/R24 retain the exact legacy uint64 JSON bytes even for inventory rows
// whose size exceeds JavaScript's safe integer range. Opaque map words remain
// opaque; only the typed Go projector changes protocol number representation.
test('H2 R14 R24 imported wide legacy numbers survive the public cell and list wires',{timeout:20000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();const raw=JSON.stringify({V:1,head:'saved',durable_at:1000,class:'work',size:'WIDE',keys:{fence:{size:'opaque'}},lease:{device:a.device,fence:7,expires:0,pending:0}}).replace('"WIDE"','9223372036854775847');
  await h.control({op:'seed',records:[{kind:'cells',id:'chat',raw}]});
  for(const path of ['/v1/dir/cells/chat','/v1/dir/list']){
   const reply=await h.request(a,'GET',path);assert.equal(reply.status,200);const text=await reply.text();assert.match(text,/"size":9223372036854775847/);assert.match(text,/"fence":\{"size":"opaque"\}/);
  }
 }finally{await h.close();}
});

// H1/R3/R4 never turns an in-memory result from an earlier applied occurrence
// into acknowledgment of a new uncommitted legacy proposal on a minority.
test('H1 R3 R4 replayed v1 proof cannot borrow an older result without quorum',{timeout:20000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'saved',class:'work',size:0,keys:{}})).status,200);
  assert.equal((await h.call(a,'GET','/v1/dir/cells/chat')).status,200);
  assert.equal(await h.control({op:'retry-without-quorum'}),'unavailable');
 }finally{await h.close();}
});

// H1/R16/R21 make erasure wait for in-flight bytes without owning the queue
// those uploads need for their final committed authorization check.
test('H1 R16 R21 retirement drains an in-flight upload without blocking its authority check',{timeout:20000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();let uploading;
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  await h.control({op:'hold-upload'});
  uploading=h.request(a,'POST','/v1/store/frames',encode('0'.repeat(32),[{rid:'b'.repeat(64),bytes:new TextEncoder().encode('AGEO\x01sealed')}]));
  assert.equal((await h.call(a,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
  const retired=await h.call(a,'POST','/v1/identity/rotation',{op:'retire',grace_ms:2000});assert.equal(retired.status,200);
  const deadline=Date.now()+8000;let response;
  do{await delay(50);response=await h.call(a,'GET','/v1/dir/list');}while(response.body?.err!=='gone'&&Date.now()<deadline);
  assert.equal(response.body.err,'gone');await h.control({op:'release-upload'});
  assert.equal((await uploading).status,410);
  let complete=false;
  while(Date.now()<deadline){complete=true;for(const node of ['1','2','3'])if(await h.control({op:'has-member',cluster,node}))complete=false;if(complete)break;await delay(50);}
  assert(complete,'erasure retained stores after the upload drained');
 }finally{await h.control({op:'release-upload'}).catch(()=>{});if(uploading)await uploading.catch(()=>{});await h.close();}
});

// H1/R25 use the existing base-path normalizer at BOTH ingress and original
// proof capture, so an operator's equivalent prefix spelling cannot break v1.
test('H1 R25 original proof uses the normalized production prefix',{timeout:20000},async()=>{
 const h=await hostedFixture({base:' fabric/ '}),a=h.client();
 try{await h.start();const r=await h.call(a,'POST','/v1/dir/cells/chat',{head:'saved',class:'work',size:0,keys:{}});assert.equal(r.status,200,JSON.stringify(r));assert.equal((await h.call(a,'GET','/v1/dir/cells/chat')).status,200);}
 finally{await h.close();}
});
