import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createHash,createPrivateKey,sign} from 'node:crypto';
import {hostedFixture} from './hosted-fixture.js';
const archive=JSON.parse(readFileSync(new URL('../../../../internal/raftdirectory/testdata/v1-large-requests.json',import.meta.url)));
const seed=createHash('sha256').update('p17-a').digest();
const key=createPrivateKey({key:Buffer.concat([Buffer.from('302e020100300506032b657004220420','hex'),seed]),format:'der',type:'pkcs8'});
const sha=raw=>createHash('sha256').update(raw).digest('hex');
const fixture=()=>hostedFixture({fixture:true,rootLabel:'root',now:archive.now,entryPoint:'test/raft/upgrade-worker.js'});
async function request(h,method,uri,body='',legacy=false,want=200){
 const headers={...archive.cases[1].headers};
 headers['Codeaf-Sig']=sign(null,Buffer.from(`codeaf-req-v1\n${method}\n${uri}\n${headers['Codeaf-Time']}\n${sha(body)}`),key).toString('base64url');
 if(legacy)headers['Fixture-Legacy']='1';
 const began=performance.now();
 const r=await h.mf.dispatchFetch('http://local'+uri,{method,headers,...(body?{body}:{})});
 const text=await r.text();if(!legacy)console.log(method+' '+uri+' HTTP '+r.status+' in '+Math.round(performance.now()-began)+' ms');assert.equal(r.status,want,method+' '+uri+': '+text.slice(0,180));return text?JSON.parse(text):null;
}

// r1.1
 test('r1.1 accepted near-limit legacy body survives hosted upgrade byte-for-byte',{timeout:60000},async()=>{
 const h=await fixture(),c=archive.cases[1];
 try{
  await h.start();assert.equal(Buffer.byteLength(c.body),1048575);
  const before=await request(h,c.method,c.uri,c.body,true);
  const source=await h.control({op:'source'});assert.equal(source.gate,undefined);assert(Buffer.byteLength(source.originals.find(r=>r.kind==='cells').doc)>1048576);
  const oldList=await request(h,'GET','/v1/dir/list','',true);
  const after=await request(h,'GET',c.uri);assert.deepEqual(after.cell,before.cell);
  const list=await request(h,'GET','/v1/dir/list');assert.deepEqual(list.cells['near-http-limit'],oldList.cells['near-http-limit']);
  const held=await request(h,'POST',c.uri+'/acquire','{}');assert.equal(held.cell.lease.fence,2);
  await request(h,'POST',c.uri+'/release','{"fence":2}',false,204);
  const released=await request(h,'GET',c.uri);assert.equal(released.cell.lease.fence,2);assert.equal(released.cell.lease.expires,0);
  await h.start();assert.deepEqual((await request(h,'GET',c.uri)).cell,released.cell);
 }finally{await h.close();}
});

// r1.3
for(const below of [false,true])test('r1.3 hosted imported '+(below?'nearly full':'grandfathered')+' directory completes membership and keeps saved cells usable',{timeout:60000},async()=>{
 const h=await fixture();
 try{
  await h.start();const n=below?28:30;
  const body=JSON.stringify({head:'head',class:'',title:'s'.repeat(300000),size:0,keys:null});assert.equal(Buffer.byteLength(body),300058);
  const created=await request(h,'POST','/v1/dir/cells/chat-0',body,true);
  const rowBytes=Buffer.byteLength(JSON.stringify(created.cell)),records=[];
  let sum=n*rowBytes;
  for(let i=1;i<n;i++){
   const cell=structuredClone(created.cell);
   if(below&&i===n-1){const trim=sum-(8388608-2048);cell.title=cell.title.slice(0,cell.title.length-trim);sum-=trim;}
   records.push({kind:'cells',id:'chat-'+i,doc:cell});
  }
  // Each additional row is an exact copy of the old handler's accepted record.
  // Bulk fixture storage keeps source setup separate from upgrade execution.
  await h.control({op:'seed',records});
  console.log('import source cell bytes='+sum);
  assert(below?sum<8388608&&sum>=8384512:sum>8388608,'wrong imported size '+sum);
  const source=await h.control({op:'source'}),list=await request(h,'GET','/v1/dir/list');assert.equal(Object.keys(list.cells).length,n);
  const owned=JSON.parse((await h.control({op:'source'})).gate);assert.equal(owned.stage,'raft-owned');
  for(const node of ['1','2','3']){const m=await h.control({op:'member',cluster:owned.config.cluster,node});assert(BigInt(m.app.applied)>1n);}
  const uri='/v1/dir/cells/chat-0',view=await request(h,'GET',uri);
  let noise='';for(let i=0;noise.length<4096;i++)noise+=createHash('sha256').update(String(i)).digest('base64url');
  const refused=await request(h,'POST','/v1/dir/cells/growth',JSON.stringify({head:'growth',title:noise.slice(0,4096)}),false,507);assert.equal(refused.err,'capacity');
  await request(h,'POST',uri+'/publish',JSON.stringify({fence:1,old_head:'head',head:'next',size:0,class:'',title:view.cell.title,pending:0}));
  await request(h,'POST',uri+'/release','{"fence":1}',false,204);
  assert.equal((await request(h,'GET',uri)).cell.head,'next');
  assert.deepEqual((await h.control({op:'source'})).originals,source.originals);
 }finally{await h.close();}
});
