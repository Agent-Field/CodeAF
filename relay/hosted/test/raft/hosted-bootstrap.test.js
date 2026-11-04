import test from 'node:test';
import assert from 'node:assert/strict';
import { hostedFixture } from './hosted-fixture.js';

test('the first write does not recover peers outside the committed singleton configuration',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();
  assert.equal((await h.call(a,'PUT','/v1/dir/devices/'+a.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  const metrics=await h.control({op:'metrics'});
  assert.equal(metrics.idleReads,0,'the initial transport view has no prior delivery to refresh');
  assert.equal(metrics.peerFailures,0,'unconfigured anchors have no failed Raft delivery to recover');
 }finally{await h.close();}
});

test('a new identity commits its first write without either additional anchor',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start({needle:'  member(id){',after:"if(String(id)!=='1')throw new Error('fixture: additional anchors unavailable');"});
  const made=await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}});
  assert.equal(made.status,200,JSON.stringify(made));
  const source=JSON.parse((await h.control({op:'source'})).gate),cluster=source.config.cluster;
  assert.deepEqual(source.birth.voters,['1']);
  for(const node of ['2','3'])assert.equal(await h.control({op:'has-member',cluster,node}),false);
  const read=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(read.status,200);assert.equal(read.body.cell.head,'first');
 }finally{await h.close();}
});


test('background formation preserves the first write through joint consensus and restart',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}})).status,200);
  let state;
  for(let turn=0;turn<16;turn++){
    state=await h.control({op:'formation-turn'});
    if(state.registry)break;
  }
  assert.equal(state.registry,true,JSON.stringify(state));assert.deepEqual(state.conf.voters,['1','2','3']);assert.equal(state.conf.outgoing?.length??0,0);
  await h.start();const read=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(read.status,200);assert.equal(read.body.cell.head,'first');
 }finally{await h.close();}
});

test('recovery after singleton publication retains immutable birth provenance',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start({needle:'if(!owned){await this.source.own(receipts);',after:"throw new Error('fixture: crash after singleton publication');"});
  assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}})).status,500);
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}})).status,200);
  let state;for(let turn=0;turn<16;turn++){state=await h.control({op:'formation-turn'});if(state.registry)break;}
  assert.equal(state.registry,true,JSON.stringify(state));
  assert.deepEqual(JSON.parse((await h.control({op:'source'})).gate).birth.voters,['1']);
 }finally{await h.close();}
});

test('singleton formation keeps renewing live holds while additional anchors remain unavailable',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true,now:Date.now()}),a=h.client(),sockets=[];
 const watch=async()=>{const response=await h.request(a,'GET','/v1/dir/watch?hold=chat:1&receipts=1',undefined,{headers:{upgrade:'websocket'}});assert.equal(response.status,101);response.webSocket.accept();sockets.push(response.webSocket);};
 try{
  await h.start({needle:'  member(id){',after:"if(String(id)!=='1')throw new Error('fixture: additional anchors unavailable');"});
  const created=await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}});assert.equal(created.status,200);
  await watch();let expires=(await h.call(a,'GET','/v1/dir/cells/chat')).body.cell.lease.expires;
  for(let turn=0;turn<4;turn++){
    await h.control({op:'shift-clock',records:20_000});await watch();
    await h.control({op:'shift-clock',records:10_000});await h.control({op:'formation-turn'});
    const cell=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(cell.status,200);assert(cell.body.cell.lease.expires>expires,'maintenance must renew fresh watch evidence during formation');expires=cell.body.cell.lease.expires;
  }
 }finally{for(const socket of sockets)socket.close();await h.close();}
});


test('a lost published singleton is not recreated from the frozen source',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),cut={needle:'  member(id){',after:"if(String(id)!=='1')throw new Error('fixture: additional anchors unavailable');"};
 try{
  await h.start(cut);assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}})).status,200);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  await h.control({op:'discard-unformed-member',cluster,node:'1'});await h.start(cut);
  assert.equal((await h.call(a,'GET','/v1/dir/cells/chat')).status,503);
  assert.equal(await h.control({op:'has-member',cluster,node:'1'}),false);
 }finally{await h.close();}
});

test('formation resumes from durable configuration after every background turn',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'first',class:'work',size:0,keys:{}})).status,200);
  let state;
  for(let turn=0;turn<16;turn++){
    state=await h.control({op:'formation-turn'});await h.start();
    const read=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(read.status,200,JSON.stringify(state));assert.equal(read.body.cell.head,'first');
    if(state.registry)break;
  }
  assert.equal(state.registry,true,JSON.stringify(state));
 }finally{await h.close();}
});

test('an idle singleton releases memory when additional anchors remain unavailable',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),cut={needle:'  member(id){',after:"if(String(id)!=='1')throw new Error('fixture: additional anchors unavailable');"};
 try{
  await h.start(cut);assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  const idle=await h.control({op:'idle-schedule'});assert.equal(idle.sleeping,true);assert.equal(idle.recurringMemberCalls,0);
  assert.equal((await h.control({op:'metrics'})).active,0);
  assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200,'the durable singleton can wake through its actual quorum');
 }finally{await h.close();}
});
