import test from 'node:test';
import assert from 'node:assert/strict';
import { CommittedHolds, MemberAlarms } from '../../src/raft/liveness.js';

// R22's protocol bridge must wait for commit, preserve sample times, close on
// unavailable and keep presence pongs separate from committed hold receipts.
test('R22 committed hold receipts and failure', async()=>{
 const policy={renew_every:30000,evidence_window:25000,max_renew_holds:2,covered:()=>false};
 let complete, begin, calls=[], sent=[],failed=0; const started=new Promise(r=>begin=r);
 const host={barrierRecord:async()=>null,commitEffect:(change,now)=>{calls.push([change,now]);begin();return new Promise(r=>complete=r)}};
 const bridge=new CommittedHolds(host,policy);
 const socket={device:'a',seenAt:1000,holds:[{cell:'c',fence:'7'}],send:b=>sent.push(JSON.parse(b)),failed:()=>failed++};
 bridge.opened(1000,true);
 const pending=bridge.renew(1000,[socket]);await started;assert.equal(sent.length,0);
 complete({code:'ok',index:'42',now:1000,items:[{cell:'c',code:'ok',view:{cell:{lease:{fence:'7',expires:91000}}}}]});
 assert.equal(await pending,31000);assert.equal(sent[0].holds[0].index,'42');assert.equal(calls[0][0].renew_holds.seen_at,1000);
 await bridge.renew(2000,[socket]);assert.equal(calls.length,1);
 await bridge.renew(31000,[socket]);assert.equal(calls.length,1,'stale evidence renewed');
 socket.seenAt=61000;host.commitEffect=async()=>{throw Error('minority')};
 await bridge.renew(61000,[socket]);assert.equal(failed,1);assert.equal(sent.length,1);
 assert.equal(await bridge.renew(91000,[]),null);
});

test('R26 R27 alarms stop only after dispatch and durable sleep',async()=>{
 const ops=[],storage={setAlarm:async at=>ops.push(['arm',at]),deleteAlarm:async()=>ops.push(['stop'])};
 let sleeping=false;
 const actor={serial:async fn=>fn(),scheduleInQueue:async(now,wall,busy)=>({send:[],plan:{sleeping,after:5000},now,wall,busy})};
 const timer=new MemberAlarms(storage,actor,async e=>ops.push(['dispatch',e.busy]),async()=>false);
 await timer.run(0,1000);assert.deepEqual(ops,[['dispatch',false],['arm',6000]]);
 sleeping=true;await timer.run(60000,61000);assert.deepEqual(ops.slice(-2),[['dispatch',false],['stop']]);
 const failed=new MemberAlarms(storage,actor,async()=>{throw Error('backpressure')},async()=>true);
 const n=ops.length;await assert.rejects(failed.run(1,1));assert.equal(ops.length,n,'failed dispatch claimed timer completion');
});

test('R27 hold evidence overflow is paged without starving later sockets',async()=>{
 let calls=0;const policy={renew_every:30000,evidence_window:25000,max_renew_holds:2,covered:()=>false};
 const bridge=new CommittedHolds({barrierRecord:async()=>null,commitEffect:async change=>{calls++;return {code:'ok',index:'1',items:[]}}},policy);
 const sockets=Array.from({length:5},(_,i)=>({device:'d'+i,seenAt:1000,holds:[{cell:'c'+i,fence:'1'}],send(){}}));
 bridge.opened(1000,true);
 assert.equal(await bridge.renew(1000,sockets),1000);assert.equal(calls,2);
 assert.equal(await bridge.renew(1000,sockets),1000);assert.equal(calls,4);
 assert.equal(await bridge.renew(1000,sockets),31000);assert.equal(calls,5);
});
