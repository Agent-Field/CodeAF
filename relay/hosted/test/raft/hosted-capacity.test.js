import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync,createHash,sign} from 'node:crypto';
import {hostedFixture} from './hosted-fixture.js';

const sha=b=>createHash('sha256').update(b).digest('hex');
function household(h){
 const root=generateKeyPairSync('ed25519'),key=generateKeyPairSync('ed25519');
 const pub=root.publicKey.export({format:'der',type:'spki'}).subarray(-32),dev=key.publicKey.export({format:'der',type:'spki'}).subarray(-32);
 const cert={V:1,device:dev.toString('hex'),created:1000,sig:''};
 cert.sig=sign(null,Buffer.from('codeaf/device-cert/v1\n'+JSON.stringify(cert)),root.privateKey).toString('hex');
 const identity='id_'+sha(pub).slice(0,32);
 return {identity,control:(op,records)=>h.control({identity,op,records}),async request(method='GET',path='/v1/dir/list',body){
  const raw=body===undefined?'':JSON.stringify(body),at=String(Date.now());
  const headers={'Codeaf-Identity':pub.toString('base64url'),'Codeaf-Cert':Buffer.from(JSON.stringify(cert)).toString('base64url'),'Codeaf-Time':at,'Codeaf-Sig':sign(null,Buffer.from('codeaf-req-v1\n'+method+'\n'+path+'\n'+at+'\n'+sha(raw)),key.privateKey).toString('base64url')};
  const r=await h.mf.dispatchFetch('http://local'+path,{method,headers,...(raw?{body:raw}:{})}),text=await r.text();
  return {status:r.status,headers:r.headers,body:text?JSON.parse(text):null};
 }};
}
async function idle(a){const result=await a.control('idle-schedule');assert.equal(result.sleeping,true);assert.equal(result.recurringMemberCalls,0);return result;}
async function fiveHouseholds(h,save=false){
 const households=[];
 for(let n=0;n<5;n++){
  const a=household(h);households.push(a);
  assert.equal((await a.request()).status,200,'first List for household '+(n+1));
  if(save&&n===0){
   assert.equal((await a.request('POST','/v1/dir/cells/saved',{head:'retained-head',class:'work',size:7,keys:{}})).status,200);
   assert.equal((await a.request('POST','/v1/dir/cells/saved/release',{fence:1})).status,204);
  }
  await idle(a);
 }
 return households;
}

test('r4.1 five sequential sleeping households fit in one production isolate',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{await h.start();await fiveHouseholds(h);const m=await h.control({op:'metrics'});assert(m.memory+m.reservedBytes<=m.coreBudget);}
 finally{await h.close();}
});

test('r4.2 the earliest released household recovers saved state through a fresh majority',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();const [a]=await fiveHouseholds(h,true),before=await a.control('retention-state');
  assert(Object.values(before).every(s=>!s.active),'sleeping members were retained');
  const cell=await a.request('GET','/v1/dir/cells/saved');assert.equal(cell.status,200);assert.equal(cell.body.cell.head,'retained-head');assert.equal(cell.body.cell.lease.expires,0);
  const after=await a.control('retention-state');console.log('durable wake',JSON.stringify({before,after}));
  assert(Object.values(after).filter(s=>s.active&&BigInt(s.term)>BigInt(before['1'].term)).length>=2,'read did not establish a fresh majority');
  for(const state of Object.values(after))assert(state.recoveries.length>=2&&state.recoveries.every(role=>role===0),'recovery assumed leadership');
  await idle(a);await a.control('evict-minority');assert.equal((await a.request()).status,503,'a remembered leader answered without a majority');
 }finally{await h.close();}
});

test('r4.3 incomplete sleep retains busy members and full admission retries after sleep',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true}),busy=[];
 try{
  await h.start();const sleeping=household(h);assert.equal((await sleeping.request()).status,200);await idle(sleeping);
  let next,refusal;
  for(let n=0;n<5;n++){
   const a=household(h),reply=await a.request();
   if(reply.status!==200){next=a;refusal=reply;break;}
   const states=await a.control('retention-state','remember');a.busyNodes=Object.keys(states).filter(node=>states[node].active);assert(a.busyNodes.length>=2);
   busy.push(a);await a.control('pause-public');
  }
  assert(next,'busy activation exceeded the admission budget');assert(busy.length>0);
  const before=await h.control({op:'metrics'});
  assert.equal(before.coreBudget,64*1024*1024);assert.equal(before.reservedBytes,before.active*6*1024*1024);
  assert(before.memory+before.reservedBytes+6*1024*1024>before.coreBudget);
  assert.equal(refusal.status,503);assert.deepEqual(refusal.body,{err:'unreachable',retry_after:1});assert.equal(refusal.headers.get('retry-after'),'1');
  const waking=await sleeping.request();assert.equal(waking.status,503,'released household wake must retry admission');assert.deepEqual(waking.body,refusal.body);
  const after=await h.control({op:'metrics'});assert.equal(after.active,before.active);assert.equal(after.reservedBytes,before.reservedBytes);
  for(const a of busy){const states=await a.control('retention-state');assert(a.busyNodes.every(node=>states[node].active),'unfinished sleep was evicted');}
  await busy[0].control('resume-public');await idle(busy[0]);
  for(const a of busy.slice(1)){const states=await a.control('retention-state');assert(a.busyNodes.every(node=>states[node].active),'another household was evicted');}
  const released=await h.control({op:'metrics'}),retry=await next.request();
  const compact=m=>({active:m.active,reservedBytes:m.reservedBytes,memory:m.memory});
  console.log('busy admission',JSON.stringify({before:compact(before),released:compact(released),retry:retry.status,final:compact(await h.control({op:'metrics'}))}));
  assert.equal(retry.status,200,'released reservations did not admit the retry');
  assert(busy.length>=2);await busy[1].control('resume-public');await idle(busy[1]);
  assert.equal((await sleeping.request()).status,200,'refused wake did not recover after capacity became available');
 }finally{for(const a of busy)await a.control('resume-public').catch(()=>{});await h.close();}
});

test('r4.4 released members stay silent, fence stale references and wake on signed input',{timeout:60000},async()=>{
 const h=await hostedFixture({fixture:true}),a=household(h);
 try{
  await h.start();assert.equal((await a.request()).status,200);await a.control('retention-state','remember');await idle(a);
  const stale=await a.control('retention-state','stale');
  for(const state of Object.values(stale)){assert.deepEqual(state.refused,[true,true,true]);assert.equal(state.handle,0);assert.equal(state.stopped,true);}
  const before=await a.control('quiet-state');assert.equal(before.alarm,null);assert.equal(before.maintenance,false);assert.deepEqual(before.memberAlarms,{'1':null,'2':null,'3':null});
  await a.control('evict-idle');await h.control({op:'reset-metrics'});
  for(let n=0;n<24;n++)await a.control('presence-turn');
  const metrics=await h.control({op:'metrics'}),quiet=await a.control('quiet-state');
  for(const key of ['turns','http','peerIn','peerOut','writes','active','reservedBytes'])assert.equal(metrics[key],0,key);
  assert.equal(quiet.generation,before.generation);assert.equal(quiet.alarm,null);assert.deepEqual(quiet.memberAlarms,before.memberAlarms);
  assert.equal((await a.request()).status,200);assert.equal((await a.control('quiet-state')).maintenance,true);
 }finally{await h.close();}
});
