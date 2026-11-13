import test from 'node:test';
import assert from 'node:assert/strict';
import {hostedFixture} from './hosted-fixture.js';

// R26/R27 advisory sweeps must stay independent of quorum and router recovery.
test('R26 R27 cold presence sweeps leave a sleeping learner group untouched',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();const store='a'.repeat(64);
  for(let n=0;n<2;n++){const r=await h.call(a,'POST','/v2/cluster/enroll',{store,protocol:1});assert.equal(r.status,200);}
  const idle=await h.control({op:'idle-schedule'});assert.equal(idle.sleeping,true);assert.equal(idle.recurringMemberCalls,0);
  const before=await h.control({op:'quiet-state'});assert.equal(before.maintenance,false);assert.equal(before.alarm,null);assert.deepEqual(before.memberAlarms,{'1':null,'2':null,'3':null});
  const cold=await h.control({op:'evict-idle'});assert.equal(cold.ready,false);
  const schedule=await h.control({op:'presence-schedule'});assert.equal(schedule.sweeps,0,'no events viewer scheduled presence work');
  await h.control({op:'reset-metrics'});
  for(let n=0;n<24;n++)await h.control({op:'presence-turn'});
  const metrics=await h.control({op:'metrics'}),after=await h.control({op:'quiet-state'});
  assert.equal(after.ready,false);assert.equal(after.generation,before.generation);assert.equal(after.alarm,null);
  assert.equal(metrics.turns,0);assert.equal(metrics.writes,0);assert.equal(metrics.peerOut,0);
  assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  assert.equal((await h.control({op:'quiet-state'})).ready,true);
  console.log('R26 R27 cold sweeps',JSON.stringify({sweeps:metrics.sweeps,memberCalls:metrics.turns,writes:metrics.writes,peerFrames:metrics.peerOut}));
 }finally{await h.close();}
});

// R27 runtime warnings remain observable, bounded and free of signed material.
test('R27 local runtime diagnostics preserve warnings and redact credentials',async()=>{
 const {FixtureLog}=await import('./fixture-log.js'),lines=[],log=new FixtureLog(line=>lines.push(line));
 log.warn('connection reset by peer; codeaf-cert: private certificate');
 log.error(new Error('workerd failed; signature='+ 'a'.repeat(80)));
 log.capture('log','workerd raw connection reset');
 log.capture('error','{"authorization":"tiny-secret"}');
 log.capture('error','{"body":"private body"}');
 for(let n=0;n<100;n++)log.capture('error','bounded failure');
 assert.equal(lines.length,64);assert(log.bytes<=16384);
 assert.match(lines[0],/connection reset/);assert.match(lines[2],/raw connection reset/);assert.doesNotMatch(lines.join('\n'),/private certificate|a{80}|tiny-secret|private body/);
});

// R4/R26 a sleeping coordinator's remembered leader has no read authority.
test('R4 R26 first read after cold idle requires a surviving majority',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  assert.equal((await h.control({op:'idle-schedule'})).sleeping,true);
  await h.control({op:'evict-minority'});
  const r=await h.request(a,'GET','/v1/dir/list');assert.equal(r.status,503);await r.text();
 }finally{await h.close();}
});

// R27 capture the actual local runtime stream, not only an injected logger.
test('R27 workerd warnings and errors reach bounded local test diagnostics',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true});
 try{
  await h.start();await h.control({op:'diagnostic'});
  for(let n=0;n<100&&!h.diagnostics.lines.some(line=>line.includes('local runtime error capture'));n++)await new Promise(resolve=>setTimeout(resolve,10));
  assert(h.diagnostics.lines.some(line=>line.includes('local runtime warning capture')));
  assert(h.diagnostics.lines.some(line=>line.includes('local runtime error capture')));
 }finally{await h.close();}
});

// R16/R26 timer metadata cannot authorize irreversible identity erasure.
test('R16 R26 stale retirement deadline rechecks committed authority',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client();
 try{
  await h.start();assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
  assert.equal((await h.control({op:'idle-schedule'})).sleeping,true);
  await h.control({op:'evict-idle'});await h.control({op:'stale-retirement'});
  assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200,'a timer hint erased an unretired identity');
 }finally{await h.close();}
});
