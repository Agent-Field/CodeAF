import test from 'node:test';
import assert from 'node:assert/strict';
import {encode} from '../../src/frame.js';
import {createHash,sign} from 'node:crypto';
import {encodeCanonical} from '../../src/raft/codec.js';
import {hostedFixture} from './hosted-fixture.js';

test('formed identity retries cold anchors after temporary activation capacity clears',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const c=h.client();
  assert.equal((await h.call(c,'POST','/v1/dir/cells/retained',{head:'retained-head',class:'work',size:7,keys:{}})).status,200);
  assert.equal((await h.call(c,'POST','/v1/dir/cells/retained/release',{fence:1})).status,204);
  await h.control({op:'form-cluster'});
  const formed=await h.call(c,'GET','/v2/cluster/control');assert.equal(formed.status,200);
  assert.deepEqual(formed.body.configuration.voters.map(String),['1','2','3']);
  assert.equal((await h.control({op:'idle-schedule'})).sleeping,true);
  await h.control({op:'evict-idle'});await h.control({op:'transient-capacity',records:true});
  assert.equal((await h.call(c,'GET','/v1/dir/cells/retained')).status,503,'one formed voter cannot authorize a read');
  await h.control({op:'transient-capacity',records:false});
  const recovered=await h.call(c,'GET','/v1/dir/cells/retained');
  assert.equal(recovered.status,200,'healthy anchors must be retried without replacing the coordinator');
  assert.equal(recovered.body.cell.head,'retained-head');
 }finally{await h.close();}
});

test('ordinary member maintenance during a confirmed directory read does not refuse the read',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const c=h.client();
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'interrupt-next-read'});
  const raced=await h.call(c,'GET','/v1/dir/list');
  assert.equal(raced.status,200,'safe maintenance during the read must refresh authority and finish');
 }finally{await h.close();}
});

for(const mode of ['no-quorum','advance'])test('interrupted read fails closed with '+mode,{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const c=h.client();
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'interrupt-next-read',records:mode});
  const refused=await h.call(c,'GET','/v1/dir/list');
  assert.equal(refused.status,503);assert.equal(refused.body.err,'unreachable');
 }finally{await h.close();}
});

test('continuous harmless maintenance permits a fresh quorum-confirmed directory read',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const c=h.client();
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'interrupt-next-read',records:'always'});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200,'healthy quorum must remain readable during ordinary member maintenance');
 }finally{await h.close();}
});

test('interrupted upload authorization retries without repeating blob I/O',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const c=h.client();
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'interrupt-next-read'});
  const frame=encode('0'.repeat(32),[{rid:'a'.repeat(64),bytes:new TextEncoder().encode('AGEO\x01sealed')}]);
  assert.equal((await h.call(c,'POST','/v1/store/frames',frame)).status,200);
  const stats=await h.call(c,'GET','/v1/store/stats');assert.equal(stats.status,200);assert.equal(stats.body.puts,1);
 }finally{await h.close();}
});

test('serving identity retries anchors removed after a temporary peer RPC failure',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const c=h.client();
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
  await h.control({op:'transient-peer-failure',records:true});
  assert.equal((await h.call(c,'POST','/v1/dir/cells/pending',{head:'pending-head',class:'work',size:7,keys:{}})).status,503);
  await h.control({op:'transient-peer-failure',records:false});
  assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200,'healthy anchors must recover without evicting a serving coordinator');
 }finally{await h.close();}
});

test('a revoke after selecting a row is enforced before a fresh-authority response finishes',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();
 try{
  await h.start();
  for(const c of [a,b])assert.equal((await h.call(c,'PUT','/v1/dir/devices/'+c.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  assert.equal((await h.call(a,'POST','/v1/dir/cells/retained',{head:'retained-head',class:'work',size:7,keys:{}})).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(b,'GET','/v1/dir/cells/retained')).status,200);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  const fields={cluster,actor:a.device,session:'fresh-read-revoke',sequence:'1',change:{revoke:{device:b.device}}};
  const command={schema:1,...fields,certificate:Buffer.from(JSON.stringify(a.cert)).toString('base64url'),request_digest:createHash('sha256').update(encodeCanonical(fields)).digest('hex'),now:Date.now(),signature:null};
  command.signature=sign(null,Buffer.from('codeaf.directory.command.v1\0'+encodeCanonical(command)),a.key.privateKey).toString('base64');
  await h.control({op:'interrupt-next-read',records:{command:Buffer.from(encodeCanonical(command)).toString('base64'),after:4}});
  assert.deepEqual(await h.call(b,'GET','/v1/dir/cells/retained'),{status:401,body:{err:'revoked'}});
 }finally{await h.close();}
});

test('a revoke during fresh authorization cannot admit a new watch',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),b=h.client();
 try{
  await h.start();
  for(const c of [a,b])assert.equal((await h.call(c,'PUT','/v1/dir/devices/'+c.device,{V:1,name:'c2VhbGVk',platform:'linux'})).status,204);
  assert.equal((await h.call(a,'POST','/v1/dir/cells/retained',{head:'retained-head',class:'work',size:7,keys:{}})).status,200);
  await h.control({op:'form-cluster'});
  assert.equal((await h.call(b,'GET','/v1/dir/cells/retained')).status,200);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  const fields={cluster,actor:a.device,session:'fresh-read-revoke',sequence:'1',change:{revoke:{device:b.device}}};
  const command={schema:1,...fields,certificate:Buffer.from(JSON.stringify(a.cert)).toString('base64url'),request_digest:createHash('sha256').update(encodeCanonical(fields)).digest('hex'),now:Date.now(),signature:null};
  command.signature=sign(null,Buffer.from('codeaf.directory.command.v1\0'+encodeCanonical(command)),a.key.privateKey).toString('base64');
  await h.control({op:'interrupt-next-read',records:{command:Buffer.from(encodeCanonical(command)).toString('base64'),after:3}});
  const response=await h.request(b,'GET','/v1/dir/watch',undefined,{headers:{Upgrade:'websocket'}});
  assert.equal(response.status,401);
  assert.deepEqual(await response.json(),{err:'revoked'});
 }finally{await h.close();}
});
