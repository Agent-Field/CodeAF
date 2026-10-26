import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createHash,createPrivateKey,sign} from 'node:crypto';
import {hostedFixture} from './hosted-fixture.js';

const fixture=JSON.parse(readFileSync(new URL('../../../../internal/raftdirectory/testdata/v1-large-requests.json',import.meta.url)));
const sha=b=>createHash('sha256').update(b).digest('hex');
const send=(h,c,read=false)=>h.mf.dispatchFetch('http://local'+c.uri,{method:read?'GET':c.method,headers:read?c.read_headers:c.headers,...(read?{}:{body:c.body})});
async function exact(h,c,read=false){
 assert.equal(Buffer.byteLength(c.body),c.name==='over-command'?65537:1048575);
 assert.equal(sha(c.body),c.body_sha256);
 const r=await send(h,c,read);assert.equal(r.status,200,(await r.clone().text()).slice(0,200));
 const view=await r.json(),init=JSON.parse(c.body);
 assert.equal(view.cell.title,init.title);assert.equal(view.cell.head,init.head);assert.equal(view.cell.lease.fence,1);assert.deepEqual(view.cell.frames,init.frames);assert.deepEqual(view.cell.keys,init.keys);assert.equal(view.cell.parent_cell,init.parent_cell);assert.equal(view.cell.orphan_turns,init.orphan_turns);assert.equal(view.cell.size,init.size);
}

// R25/R3 preserve complete archived bodies through the real public workerd
// route, all three durable stores and recovery after the final commit.
test('R25 archived requests above 64 KiB and just under 1 MiB commit in workerd',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true,rootLabel:'root',now:fixture.now});
 try{
  await h.start();
  for(const c of fixture.cases)await exact(h,c);
  const before=await h.control({op:'metrics'});console.log(JSON.stringify({peakCoreBytes:before.peakMemory,activeHandles:before.active,reservedBytes:before.reservedBytes}));
  assert.equal(before.active,3,'cold object recreation leaked live handles');
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  for(const node of ['1','2','3']){const state=await h.control({op:'staging',cluster,node});assert.equal(state.rows.n,0);assert.equal(state.app.version,'2');}
  const metrics=await h.control({op:'metrics'});assert(metrics.peakMemory<=metrics.coreBudget,JSON.stringify(metrics));
  console.log(JSON.stringify({peakCoreBytes:metrics.peakMemory,coreBudget:metrics.coreBudget}));
  for(let round=0;round<2;round++)for(const node of ['1','2','3']){const cold=await h.control({op:'replace-activation',cluster,node});assert.equal(cold.after,cold.before);assert(cold.refused);assert.equal(cold.oldHandle,0);assert.equal(cold.active,3);}
  for(const c of fixture.cases)await exact(h,c,true);
  await h.start();for(const c of fixture.cases)await exact(h,c,true);
 }finally{await h.close();}
});

// R25/R3/R27 require a process cut between durable body pages to expose no
// partial cell and leave only one bounded, replaceable staging slot.
test('R25 interrupted workerd staging stays atomic and bounded after restart',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true,rootLabel:'root',now:fixture.now}),c=fixture.cases[1];
 try{
  const cut={needle:'result=await this.commit(command);requireOK(result.value.code);',after:"if(Array.isArray(raw)&&command===raw[16])throw new Error('interrupted body staging');"};
  await h.start(cut);
  for(let attempt=0;attempt<2;attempt++){
   const failed=await send(h,c);assert(failed.status>=500,`interruption returned ${failed.status}`);await failed.text();
   await h.start();
   const absent=await send(h,c,true);assert.equal(absent.status,404);await absent.text();
   const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
   for(const node of ['1','2','3']){const state=await h.control({op:'staging',cluster,node});assert(state.rows.n>0);assert(state.rows.n<=33);assert(state.rows.bytes<=1048576+65536);assert.equal(state.app.version,'0');}
   if(attempt===0)await h.start(cut);
  }
  await exact(h,c);
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  for(const node of ['1','2','3']){const state=await h.control({op:'staging',cluster,node});assert.equal(state.rows.n,0);assert.equal(state.app.version,'1');}
 }finally{await h.close();}
});

// R25/R27 keep an abandoned staging slot inert: its raw body pages are not
// coordination records, so every maintenance turn that scans past them must
// still complete while they wait for the next request to replace them.
test('R25 abandoned workerd staging does not break hosted maintenance',{timeout:90000},async()=>{
 const h=await hostedFixture({fixture:true,rootLabel:'root',now:fixture.now}),c=fixture.cases[1];
 try{
  const cut={needle:'result=await this.commit(command);requireOK(result.value.code);',after:"if(Array.isArray(raw)&&command===raw[16])throw new Error('interrupted body staging');"};
  await h.start(cut);
  const failed=await send(h,c);assert(failed.status>=500,`interruption returned ${failed.status}`);await failed.text();
  await h.start();
  const absent=await send(h,c,true);assert.equal(absent.status,404);await absent.text();
  const cluster=JSON.parse((await h.control({op:'source'})).gate).config.cluster;
  const staged=await h.control({op:'staging',cluster,node:'1'});assert(staged.rows.n>0,'the interruption left no staged pages');
  const turns=await h.control({op:'leadership-ticks',ticks:8});
  assert.deepEqual(turns.errors,[],'maintenance failed while staged pages remained');
  await exact(h,c);
 }finally{await h.close();}
});


// R25 preserves the old body limit even when legal JSON needs six-byte HTML
// escaping in the committed logical row and the returned original view.
test('R25 maximum legacy title escaping remains lossless in workerd',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true,rootLabel:'root',now:fixture.now});
 try{
  await h.start();const c=structuredClone(fixture.cases[0]);
  c.uri='/v1/dir/cells/escaped-title';
  const title='<'.repeat(1048576-Buffer.byteLength(JSON.stringify({head:'escaped',title:''})));
  c.body=JSON.stringify({head:'escaped',title});assert.equal(Buffer.byteLength(c.body),1048576);
  const seed=createHash('sha256').update('p17-a').digest(),key=createPrivateKey({key:Buffer.concat([Buffer.from('302e020100300506032b657004220420','hex'),seed]),format:'der',type:'pkcs8'});
  c.headers['Codeaf-Sig']=sign(null,Buffer.from(`codeaf-req-v1\nPOST\n${c.uri}\n${c.headers['Codeaf-Time']}\n${sha(c.body)}`),key).toString('base64url');
  const r=await send(h,c);assert.equal(r.status,200,(await r.clone().text()).slice(0,200));
  const view=await r.json();assert.equal(view.cell.title,title);assert.equal(view.cell.head,'escaped');assert.equal(view.cell.lease.fence,1);
  for(let fence=2;fence<=3;fence++){
   const request=structuredClone(c);request.uri+='/acquire';request.body='{}';
   request.headers['Codeaf-Sig']=sign(null,Buffer.from(`codeaf-req-v1\nPOST\n${request.uri}\n${request.headers['Codeaf-Time']}\n${sha(request.body)}`),key).toString('base64url');
   const resumed=await send(h,request);assert.equal(resumed.status,200,(await resumed.clone().text()).slice(0,200));const held=await resumed.json();assert.equal(held.cell.title,title);assert.equal(held.cell.lease.fence,fence);
  }
  await h.start();const headers={...c.headers};headers['Codeaf-Sig']=sign(null,Buffer.from(`codeaf-req-v1\nGET\n${c.uri}\n${headers['Codeaf-Time']}\n${sha('')}`),key).toString('base64url');
  const read=await h.mf.dispatchFetch('http://local'+c.uri,{headers});assert.equal(read.status,200,(await read.clone().text()).slice(0,200));assert.equal((await read.json()).cell.title,title);
  const metrics=await h.control({op:'metrics'});assert.equal(metrics.active,3);assert(metrics.peakMemory<=metrics.coreBudget,JSON.stringify(metrics));console.log(JSON.stringify({escapedTitleCoreBytes:metrics.peakMemory,activeHandles:metrics.active}));
 }finally{await h.close();}
});

// R25/R27 keep an ordinary small resume usable after a large frame plan was
// retained. Validation and visibility must not exhaust the reactor by decoding
// several complete copies of the same immutable metadata in one transaction.
test('R25 small legacy resumes preserve large retained frame plans in workerd',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true,rootLabel:'root',now:fixture.now}),c=fixture.cases[1];
 try{
  await h.start();await exact(h,c);
  const seed=createHash('sha256').update('p17-a').digest(),key=createPrivateKey({key:Buffer.concat([Buffer.from('302e020100300506032b657004220420','hex'),seed]),format:'der',type:'pkcs8'});
  for(let fence=2;fence<=3;fence++){
   const request=structuredClone(c);request.uri+='/acquire';request.body='{}';
   request.headers['Codeaf-Sig']=sign(null,Buffer.from(`codeaf-req-v1\nPOST\n${request.uri}\n${request.headers['Codeaf-Time']}\n${sha(request.body)}`),key).toString('base64url');
   const response=await send(h,request);assert.equal(response.status,200,(await response.clone().text()).slice(0,200));
   const view=await response.json();assert.equal(view.cell.lease.fence,fence);assert.deepEqual(view.cell.frames,JSON.parse(c.body).frames);
  }
  const metrics=await h.control({op:'metrics'});console.log(JSON.stringify({resumedFrameCoreBytes:metrics.peakMemory,activeHandles:metrics.active}));assert.equal(metrics.active,3);assert(metrics.peakMemory<=metrics.coreBudget);
 }finally{await h.close();}
});
