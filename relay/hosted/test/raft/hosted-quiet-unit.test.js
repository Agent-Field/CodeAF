import test from 'node:test';
import assert from 'node:assert/strict';
import {HostedIdentity} from '../../src/raft/hosted.js';

// R26 a real input racing the final timer cancellation retains active work.
test('R26 wake during durable sleep cancellation retains a maintenance alarm',async()=>{
 let began,release;
 const started=new Promise(resolve=>{began=resolve;}),blocked=new Promise(resolve=>{release=resolve;});
 const values=new Map([['raft-maintenance',true]]);let alarm=5000;
 const host=Object.assign(Object.create(HostedIdentity.prototype),{
  asleep:true,wakeVersion:0,alarmTail:Promise.resolve(),maintaining:true,
  policy:{tick_every:5000},holds:{next:null},watchers:{liveness:{rearm(){}}},
  ctx:{storage:{put:async(key,value)=>values.set(key,value),delete:async key=>{began();await blocked;values.delete(key);},sync:async()=>{}}},
  armAlarm:async at=>{alarm=at;},
 });
 const sleep=host.sleepMaintenance(0);await started;
 const wake=host.wakeMaintenance();release();await Promise.all([sleep,wake]);
 assert.equal(host.asleep,false);assert.equal(values.get('raft-maintenance'),true);assert(alarm>Date.now());
});

// R26 an already admitted wake must invalidate an older sleep completion.
test('R26 stale sleep cannot cancel a newer input',async()=>{
 let alarm=5000,writes=0;
 const host=Object.assign(Object.create(HostedIdentity.prototype),{
  asleep:true,wakeVersion:2,alarmTail:Promise.resolve(),ctx:{storage:{delete:async()=>{writes++;}}},armAlarm:async at=>{alarm=at;},
 });
 await host.sleepMaintenance(1);
 assert.equal(host.asleep,false);assert.equal(alarm,5000);assert.equal(writes,0);
});

// R26 a failed durable hint must remain repairable on the next activation.
test('R26 retirement hints retry after an interrupted durable write',async()=>{
 for(const removal of [false,true]){
  const values=new Map(removal?[['raft-retire-at',100]]:[]);let interrupted=true;
  const write=async fn=>{if(interrupted){interrupted=false;throw Error('storage unavailable');}fn();};
  const host=Object.assign(Object.create(HostedIdentity.prototype),{
   retirementKnown:true,retireAt:removal?100:undefined,
   ctx:{storage:{put:(key,value)=>write(()=>values.set(key,value)),delete:key=>write(()=>values.delete(key))}},
  });
  const root=removal?{}:{rotation:{retire_at:100}};
  await assert.rejects(host.rememberRetirement(root),/storage unavailable/);
  await host.rememberRetirement(root);
  assert.equal(values.get('raft-retire-at'),removal?undefined:100);
 }
});
