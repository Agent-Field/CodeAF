import test from 'node:test';
import assert from 'node:assert/strict';
import { stat } from 'node:fs/promises';
import { join } from 'node:path';
import { hostedFixture } from './hosted-fixture.js';

// R6/R30 keep hosted leadership absent rather than accepting an intent that
// cannot finish. Restart runs the same bundle over the same persistent state.
test('R6 R30 hosted control stays absent across a runtime restart',{timeout:30000},async()=>{
 const h=await hostedFixture({base:'/fabric'}),a=h.client();
 try{
  await h.start();
  for(const method of ['GET','POST']){
   const r=await h.request(a,method,'/v2/cluster/control');
   assert.equal(r.status,404);await r.text();
  }
  const bundle=await stat(join(h.dir,'worker.js'));
  await h.start();
  const restarted=await stat(join(h.dir,'worker.js'));
  assert.equal(restarted.mtimeNs??restarted.mtimeMs,bundle.mtimeNs??bundle.mtimeMs,'restart rebuilt the program');
  const r=await h.request(a,'GET','/v2/cluster/control');
  assert.equal(r.status,404);await r.text();
 }finally{await h.close();}
});
