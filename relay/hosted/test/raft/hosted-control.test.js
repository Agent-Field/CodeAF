import test from 'node:test';
import assert from 'node:assert/strict';
import { stat } from 'node:fs/promises';
import { join } from 'node:path';
import { hostedFixture } from './hosted-fixture.js';

// R9/R12/R23/R30 expose authenticated learner doors with explicit personal leadership.
test('R9 R12 R23 R30 hosted member doors and same-bundle restart',{timeout:30000},async()=>{
 const h=await hostedFixture({base:'/fabric'}),a=h.client();
 try{
  await h.start();
  let r=await h.request(a,'GET','/v2/cluster/control');
  assert.equal(r.status,200,await r.clone().text());
  let status=await r.json();assert.equal(status.self,'0');assert.equal(status.leadership_unavailable,true);
  assert.deepEqual(status.configuration.voters,['1','2','3']);
  const enroll=async()=>{const r=await h.request(a,'POST','/v2/cluster/enroll',{store:'a'.repeat(64),protocol:1});assert.equal(r.status,200,await r.clone().text());return r.json();};
  const first=await enroll(),second=await enroll();assert.equal(first.node,second.node);assert.notEqual(second.barrier,'0');
  r=await h.request(a,'GET','/v2/cluster/control');status=await r.json();assert.equal(status.self,first.node);assert(status.configuration.learners.includes(first.node));assert(!status.ready?.includes(first.node));assert(!status.leadership_unavailable);
  r=await h.request(a,'POST','/v2/cluster/control');assert.equal(r.status,400);await r.text();
  for(const path of ['control','enroll','exchange','socket','forward']){
   const r=await h.mf.dispatchFetch('http://local/fabric/v2/cluster/'+path,{method:path==='enroll'||path==='exchange'?'POST':'GET'});assert([400,403].includes(r.status),path+' '+r.status);await r.text();
  }
  const bundle=await stat(join(h.dir,'worker.js'));
  await h.start();
  const restarted=await stat(join(h.dir,'worker.js'));
  assert.equal(restarted.mtimeNs??restarted.mtimeMs,bundle.mtimeNs??bundle.mtimeMs,'restart rebuilt the program');
  const returned=await enroll();assert.equal(returned.node,first.node);assert(BigInt(returned.generation)>BigInt(first.generation));
 }finally{await h.close();}
});
