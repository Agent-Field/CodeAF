import WebSocket from 'ws';
import test from 'node:test';
import assert from 'node:assert/strict';
import { hostedFixture } from './hosted-fixture.js';
import { encode } from '../../src/frame.js';
import { createHash, randomBytes, sign } from 'node:crypto';
import { encodeCanonical } from '../../src/raft/codec.js';

test('warm blob and directory reads reuse confirmed authority without proposals or barriers',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  await h.control({op:'reset-metrics'});
  const raw=encode('0'.repeat(32),[{rid:'a'.repeat(64),bytes:new TextEncoder().encode('AGEO\x01sealed')}]);
  assert.equal((await h.call(a,'POST','/v1/store/frames',raw)).status,200);
  assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  const metrics=await h.control({op:'metrics'});
  assert.equal(metrics.proposals,0);
  assert.equal(metrics.barriers,0);
  assert.equal(metrics.authorizations,2,'each request authorizes once');
 }finally{await h.close();}
});

test('native enrollment cannot revive a certificate expired at an observed read time',{timeout:30000},async()=>{
 const base=Date.now(),h=await hostedFixture({fixture:true,now:base}),a=h.client(),worker=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  const certificate=h.delegate({cluster,key:worker.key.publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64'),made:base-1000,expires:base+15_000,cells:['chat'],learner:false});
  await h.control({op:'shift-clock',records:30_000});
  const observed=await h.call(a,'GET','/v1/dir/list');assert(observed.body.now>certificate.expires);
  await h.control({op:'shift-clock',records:-20_000});
  const fields={cluster,actor:a.device,session:'clock-floor',sequence:'1',change:{enroll_worker:{certificate,grants:{chat:'gQ=='}}}};
  const command={schema:1,...fields,certificate:Buffer.from(JSON.stringify(a.cert)).toString('base64url'),request_digest:createHash('sha256').update(encodeCanonical(fields)).digest('hex'),now:Date.now(),signature:null};
  command.signature=sign(null,Buffer.from('codeaf.directory.command.v1\0'+encodeCanonical(command)),a.key.privateKey).toString('base64');
  await h.control({op:'reset-metrics'});
  const result=await h.api(a,{cluster,action:'submit',incarnation:'0',command});
  assert.equal((await h.control({op:'metrics'})).presenceCommits,1,'native ingress commits a compatible device_seen clock witness');
  assert.equal(result.status,200,JSON.stringify(result));assert.equal(result.body.result.code,'bad_request');
 }finally{await h.close();}
});

test('proposal-free reads retain their observed time across clock regression and recovery',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true,now:Date.now()}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  await h.control({op:'shift-clock',records:30_000});
  const first=await h.call(a,'GET','/v1/dir/list');assert.equal(first.status,200);
  await h.control({op:'shift-clock',records:-20_000});
  const second=await h.call(a,'GET','/v1/dir/list');assert.equal(second.status,200);assert(second.body.now>=first.body.now);
  await h.start();const recovered=await h.call(a,'GET','/v1/dir/list');assert.equal(recovered.status,200);assert(recovered.body.now>=first.body.now);
 }finally{await h.close();}
});

test('expired cached authority cannot answer from an isolated leader',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  await h.control({op:'expire-read-quorum'});
  const read=await h.call(a,'GET','/v1/dir/list');assert.equal(read.status,503);assert.equal(read.body.err,'unreachable');
 }finally{await h.close();}
});

test('a selected member recovering cannot reuse its previous activation certificate',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  await h.control({op:'replace-read-member'});
  const read=await h.call(a,'GET','/v1/dir/list');assert.equal(read.status,503);assert.equal(read.body.err,'unreachable');
  assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
 }finally{await h.close();}
});

for(const interruption of ['member recovery','one scheduled interruption','continuous scheduled interruptions']){
 test('link approvals use fresh quorum authority after '+interruption,{timeout:30000},async()=>{
  const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();
  const openLink=async client=>{
   const opened=await h.mf.dispatchFetch('http://local/v1/link/requests',{method:'POST',body:JSON.stringify({pubkey:Buffer.from(client.cert.device,'hex').toString('base64url'),x25519:randomBytes(32).toString('base64url'),name_sealed:randomBytes(45).toString('base64url'),platform:'linux'})});
   assert.equal(opened.status,201);
   const pending=await opened.json();
   return {code:pending.code,approval:{device:{V:1,name:'c2VhbGVk',platform:'linux'},cert:Buffer.from(JSON.stringify(client.cert)).toString('base64url'),grant:randomBytes(64).toString('base64url')}};
  };
  const decision=async code=>{
   const reply=await h.mf.dispatchFetch('http://local/v1/link/requests/'+code);
   assert.equal(reply.status,200);return reply.json();
  };
  try{
   await h.start();
   assert.equal((await h.call(a,'PUT','/v1/dir/devices/'+a.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
   await h.control({op:'form-cluster'});
   const configuration=await h.control({op:'formation-turn'});
   assert.deepEqual(configuration.conf.voters.map(String),['1','2','3']);
   assert.equal(configuration.conf.outgoing?.length??0,0);assert.equal(configuration.forming,false);
   const pending=await openLink(b);
   assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
   if(interruption==='member recovery'){
    const replacement=await h.control({op:'replace-read-member'});
    assert.equal(replacement.after,replacement.before,'recovery preserves the applied index');
    assert.equal(replacement.refused,true,'the old activation remains fenced');
   }else await h.control({op:'interrupt-next-read',records:interruption==='one scheduled interruption'?'once':'always'});
   const approved=await h.call(a,'POST','/v1/dir/requests/'+pending.code+'/approve',pending.approval);
   assert.equal(approved.status,204,'the first approval must succeed: '+JSON.stringify(approved));
   const delivered=await decision(pending.code);
   assert.equal(delivered.state,'approved','the decision is delivered to the link request');
   assert.equal(delivered.grant,pending.approval.grant,'the sealed grant is delivered unchanged');
   assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
   assert.equal((await h.call(b,'GET','/v1/dir/list')).status,200,'the admitted device can read the directory');
   await h.control({op:'interrupt-next-read',records:false});
   assert.equal((await h.call(a,'POST','/v1/dir/devices/'+b.device+'/revoke')).status,204);
   assert.deepEqual(await h.call(b,'GET','/v1/dir/list'),{status:401,body:{err:'revoked'}});
   const next=await openLink(h.client());
   assert.deepEqual(await h.call(b,'POST','/v1/dir/requests/'+next.code+'/approve',next.approval),{status:401,body:{err:'revoked'}});
   assert.equal((await decision(next.code)).state,'pending');
   assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
   await h.control({op:'expire-read-quorum'});
   assert.deepEqual(await h.call(a,'POST','/v1/dir/requests/'+next.code+'/approve',next.approval),{status:503,body:{err:'unreachable',retry_after:1}});
   assert.equal((await decision(next.code)).state,'pending','an isolated leader cannot deliver an approval');
   assert.equal((await h.call(a,'GET','/v1/dir/list')).status,503);
  }finally{await h.close();}
 });
}

test('snapshot traffic invalidates warm authority before the following read',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'saved',class:'work',size:0,keys:{}})).status,200);
  assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  await h.control({op:'reset-metrics'});
  assert.equal(await h.control({op:'stale-snapshot'}),3);
  const read=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(read.status,200);assert.equal(read.body.cell.head,'saved');
  assert((await h.control({op:'metrics'})).barriers>0);
 }finally{await h.close();}
});


test('directory reads reject signatures stale against previously observed time after clock rollback',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true,now:Date.now()}),a=h.client(),path='/v1/dir/list';
 try{
  await h.start();assert.equal((await h.call(a,'GET',path)).status,200);
  const advance=6*60*1000,at=String(Date.now()+advance);
  const signature=sign(null,Buffer.from('codeaf-req-v1\nGET\n'+path+'\n'+at+'\n'+createHash('sha256').update('').digest('hex')),a.key.privateKey).toString('base64url');
  await h.control({op:'shift-clock',records:advance});
  assert.equal((await h.call(a,'GET',path,undefined,{headers:{'Codeaf-Time':at,'Codeaf-Sig':signature}})).status,200);
  await h.control({op:'shift-clock',records:-advance});
  assert.equal((await h.call(a,'GET',path)).status,401,'a regressed ingress clock cannot revive an old signed request');
 }finally{await h.close();}
});


test('a replicated revoke closes the device watch before another public request',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();let socket;
 try{
  await h.start();
  for(const c of [a,b])assert.equal((await h.call(c,'PUT','/v1/dir/devices/'+c.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  const path='/v1/dir/watch',at=String(Date.now()),url=new URL(path,await h.mf.ready);url.protocol='ws:';
  const headers={'Codeaf-Identity':h.rootPublicKey.toString('base64url'),'Codeaf-Cert':Buffer.from(JSON.stringify(b.cert)).toString('base64url'),'Codeaf-Time':at,'Codeaf-Sig':sign(null,Buffer.from('codeaf-req-v1\nGET\n'+path+'\n'+at+'\n'+createHash('sha256').update('').digest('hex')),b.key.privateKey).toString('base64url')};
  socket=new WebSocket(url,{headers,closeTimeout:200});await new Promise((resolve,reject)=>{socket.once('open',resolve);socket.once('error',reject);});
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  const fields={cluster,actor:a.device,session:'external-revoke',sequence:'1',change:{revoke:{device:b.device}}};
  const command={schema:1,...fields,certificate:Buffer.from(JSON.stringify(a.cert)).toString('base64url'),request_digest:createHash('sha256').update(encodeCanonical(fields)).digest('hex'),now:Date.now(),signature:null};
  command.signature=sign(null,Buffer.from('codeaf.directory.command.v1\0'+encodeCanonical(command)),a.key.privateKey).toString('base64');
  const closed=new Promise(resolve=>{const timeout=setTimeout(()=>resolve(null),1500);socket.once('close',code=>{clearTimeout(timeout);resolve(code);});});
  assert.equal(await h.control({op:'direct-commit',records:Buffer.from(encodeCanonical(command)).toString('base64')}),'ok');
  assert.equal(await closed,4401,'committed revocation closes the watch without a sweep or subsequent request');
 }finally{socket?.close();await h.close();}
});
