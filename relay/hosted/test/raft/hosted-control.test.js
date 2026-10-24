import test from 'node:test';
import assert from 'node:assert/strict';
import { stat } from 'node:fs/promises';
import { join } from 'node:path';
import { hostedFixture } from './hosted-fixture.js';

// R6/R9/R23/R30 keep hosted member and leadership doors absent until their
// runtime can serve an external member. Refusing avoids an intent that
// cannot finish. Restart runs the same bundle over the same persistent state.
test('R6 R9 R23 R30 hosted member doors stay absent across a runtime restart',{timeout:30000},async()=>{
 const h=await hostedFixture({base:'/fabric'}),a=h.client();
 try{
  await h.start();
  for(const path of ['/v2/cluster/control','/v2/cluster/enroll','/v2/cluster/exchange','/v2/cluster/socket']){
   for(const method of ['GET','POST']){
    const r=await h.request(a,method,path);
    assert.equal(r.status,404,path);await r.text();
   }
  }
  const bundle=await stat(join(h.dir,'worker.js'));
  await h.start();
  const restarted=await stat(join(h.dir,'worker.js'));
  assert.equal(restarted.mtimeNs??restarted.mtimeMs,bundle.mtimeNs??bundle.mtimeMs,'restart rebuilt the program');
  for(const path of ['/v2/cluster/control','/v2/cluster/enroll','/v2/cluster/exchange','/v2/cluster/socket']){
   const r=await h.request(a,'GET',path);
   assert.equal(r.status,404,path);await r.text();
  }
 }finally{await h.close();}
});
