import test from 'node:test';
import assert from 'node:assert/strict';
import { hostedFixture } from './hosted-fixture.js';
import { encode } from '../../src/frame.js';

test('blob requests do not synchronously commit advisory device presence',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();
  assert.equal((await h.call(a,'PUT','/v1/dir/devices/'+a.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  await h.control({op:'reset-metrics'});
  const rid='a'.repeat(64),frame=encode('0'.repeat(32),[{rid,bytes:new TextEncoder().encode('AGEO\x01sealed')}]);
  assert.equal((await h.call(a,'POST','/v1/store/frames',frame)).status,200);
  const get=await h.request(a,'GET','/v1/store/objects/'+rid);assert.equal(get.status,200);await get.arrayBuffer();
  assert.equal((await h.call(a,'POST','/v1/store/has',{rids:[rid]})).status,200);
  assert.equal((await h.control({op:'metrics'})).presenceCommits,0);
  assert.equal((await h.control({op:'idle-schedule'})).sleeping,true);
  assert.equal((await h.control({op:'metrics'})).presenceCommits,1,'one maintenance commit covers all three requests');
  const list=await h.call(a,'GET','/v1/dir/list');assert.equal(list.status,200);assert(list.body.devices[a.device].last_seen>0);
 }finally{await h.close();}
});

test('watch departure coalesces presence without a synchronous proposal',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'PUT','/v1/dir/devices/'+a.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  const watch=await h.request(a,'GET','/v1/dir/watch',undefined,{headers:{upgrade:'websocket'}});assert.equal(watch.status,101);
  const socket=watch.webSocket;socket.accept();
  await h.control({op:'reset-metrics'});
  const closed=new Promise(resolve=>socket.addEventListener('close',resolve,{once:true}));socket.close(1000);await closed;
  assert.equal((await h.control({op:'metrics'})).presenceCommits,0);
  assert.equal((await h.control({op:'idle-schedule'})).sleeping,true);
  assert.equal((await h.control({op:'metrics'})).presenceCommits,1);
 }finally{await h.close();}
});

test('queued presence cannot restore a device revoked during a streamed take',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();
 try{
  await h.start();
  for(const c of [a,b])assert.equal((await h.call(c,'PUT','/v1/dir/devices/'+c.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  const sealed=new Uint8Array(2<<20);sealed.set(new TextEncoder().encode('AGEO\x01sealed'));
  const frame=encode('0'.repeat(32),[{rid:'b'.repeat(64),bytes:sealed}]);
  const put=await h.call(a,'POST','/v1/store/frames',frame);assert.equal(put.status,200);
  const take=await h.request(b,'GET','/v1/store/frames/'+put.body.frame);assert.equal(take.status,200);
  const reader=take.body.getReader(),first=await reader.read();assert(first.value.length>0);
  assert.equal((await h.call(a,'POST','/v1/dir/devices/'+b.device+'/revoke')).status,204);
  assert.deepEqual(await h.call(b,'GET','/v1/store/stats'),{status:401,body:{err:'revoked'}});
  let bytes=first.value.length;for(;;){const chunk=await reader.read();if(chunk.done)break;bytes+=chunk.value.length;}
  assert.equal(bytes,frame.length);
  assert.equal((await h.control({op:'idle-schedule'})).sleeping,true);
  assert.deepEqual(await h.call(b,'GET','/v1/dir/list'),{status:401,body:{err:'revoked'}});
  const retained=await h.request(a,'GET','/v1/store/frames/'+put.body.frame);assert.equal(retained.status,200);
  assert.deepEqual(new Uint8Array(await retained.arrayBuffer()),frame);
 }finally{await h.close();}
});


test('a committed revoke interrupts a held store poll before its upload finishes',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();let upload,poll;
 const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 try{
  await h.start();
  for(const c of [a,b])assert.equal((await h.call(c,'PUT','/v1/dir/devices/'+c.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  await h.control({op:'hold-upload'});
  upload=h.call(a,'POST','/v1/store/frames',encode('0'.repeat(32),[{rid:'c'.repeat(64),bytes:new TextEncoder().encode('AGEO\x01sealed')}]));
  let started=false;for(let n=0;n<500&&!started;n++){await delay(10);started=await h.control({op:'upload-started'});}
  assert(started,'the upload reached its actual storage call before Has begins');
  poll=h.call(b,'POST','/v1/store/has',{rids:['c'.repeat(64)]});
  let waiting=false;for(let n=0;n<100&&!waiting;n++){await delay(10);waiting=await h.control({op:'has-waiting'});}
  assert(waiting,'the authenticated store poll reached its real in-flight upload wait');
  assert.equal((await h.call(a,'POST','/v1/dir/devices/'+b.device+'/revoke')).status,204);
  assert.equal(await Promise.race([poll.then(r=>r.status),delay(750).then(()=>null)]),401,'revocation releases a held poll on commit');
 }finally{await h.control({op:'release-upload'}).catch(()=>{});await Promise.allSettled([upload,poll].filter(Boolean));await h.close();}
});

test('revoking an upload keeps other devices waiting until its actual write finishes',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();let upload,poll;
 const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 try{
  await h.start();
  for(const c of [a,b])assert.equal((await h.call(c,'PUT','/v1/dir/devices/'+c.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  await h.control({op:'hold-upload'});
  const rid='d'.repeat(64);
  upload=h.call(b,'POST','/v1/store/frames',encode('0'.repeat(32),[{rid,bytes:new TextEncoder().encode('AGEO\x01sealed')}]));
  let started=false;for(let n=0;n<500&&!started;n++){await delay(10);started=await h.control({op:'upload-started'});}
  assert(started,'the upload reached its actual storage call before Has begins');
  poll=h.call(a,'POST','/v1/store/has',{rids:[rid]});
  let waiting=false;for(let n=0;n<100&&!waiting;n++){await delay(10);waiting=await h.control({op:'has-waiting'});}
  assert(waiting,'the other device is waiting for the actual upload');
  assert.equal((await h.call(a,'POST','/v1/dir/devices/'+b.device+'/revoke')).status,204);
  assert.equal(await Promise.race([upload.then(r=>r.status),delay(750).then(()=>null)]),401,'the revoked upload returns without waiting for storage');
  assert.equal(await Promise.race([poll.then(()=>true),delay(100).then(()=>false)]),false,'Has cannot overtake an upload that still writes');
  await h.control({op:'release-upload'});
  assert.deepEqual(await poll,{status:200,body:{have:[true]}});
 }finally{await h.control({op:'release-upload'}).catch(()=>{});await Promise.allSettled([upload,poll].filter(Boolean));await h.close();}
});
