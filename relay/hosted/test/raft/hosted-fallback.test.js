import test from 'node:test';
import assert from 'node:assert/strict';
import {HostedIdentity} from '../../src/raft/hosted.js';

// R7 authority can be unavailable throughout an election. The two surviving
// voters still need their ordinary clocks and a retained maintenance alarm.
test('R7 pending membership cannot stop surviving follower election clocks',async()=>{
 const clocks=[],alarms=[],reads=[];
 const members=new Map(['1','2'].map(id=>[id,{
  drainRecovery:async()=>null,scheduled:async()=>null,
  schedule:async elapsed=>{clocks.push([id,elapsed]);return {plan:{},status:{applied:1n,term:2n,leader:0n,role:0}};},
 }]));
 members.set('3',{drainRecovery:async()=>{throw Error('standby unavailable');}});
 const host=Object.assign(Object.create(HostedIdentity.prototype),{
  asleep:false,wakeVersion:0,started:0,elapsed:0,operations:1,membershipPending:true,
  manifest:{config:{anchors:['1','2','3']}},members,states:new Map(),queue:[],reads:[],waiters:new Set(),progressVersion:0,
  holds:{next:null},policy:{tick_every:5000},forwarding:{pending:new Map(),socket:()=>null},
  peers:{view:{'4':{voter:true}},boxes:new Map(),snapshots:new Map(),retryTargets:new Set()},
  pump:async()=>{},reconcileMembership:async()=>{reads.push('read');throw Error('quorum unavailable');},armAlarm:async at=>alarms.push(at),
 });
 await host.maintenance(5000,5000);
 assert.deepEqual(clocks,[['1',5000],['2',5000]]);
 assert.equal(reads.length,0);assert.deepEqual(alarms,[10000]);
});

// R23/R26 a forwarding request or undelivered voter work must finish before
// the group may sleep; a disconnected learner cannot keep the voters awake.
test('R23 R26 pending forwarding and voting delivery prevent idle suppression',()=>{
 const host=Object.assign(Object.create(HostedIdentity.prototype),{
  operations:1,queue:[],members:new Map([['1',{}]]),forwarding:{pending:new Map()},
  peers:{view:{'4':{voter:true},'5':{voter:false}},boxes:new Map([['4',{queues:[[],[],[]]}],['5',{queues:[[{}],[],[]]}]]),snapshots:new Map(),retryTargets:new Set()},
 });
 assert.equal(host.coordinationBusy(),false);
 host.forwarding.pending.set('request',{});assert.equal(host.coordinationBusy(),true);host.forwarding.pending.clear();
 host.peers.boxes.get('4').queues[0].push({});assert.equal(host.coordinationBusy(),true);host.peers.boxes.get('4').queues[0]=[];
 host.peers.retryTargets.add('4');assert.equal(host.coordinationBusy(),true);host.peers.retryTargets.clear();
 host.peers.snapshots.set('4',{});assert.equal(host.coordinationBusy(),true);host.peers.snapshots.clear();
 host.peers.pending=1;assert.equal(host.coordinationBusy(),true);host.peers.pending=0;
 host.peers.view['1']={voter:true};host.peers.boxes.set('1',{queues:[[{}],[],[]]});assert.equal(host.coordinationBusy(),true);host.peers.boxes.get('1').queues[0]=[];
 assert.equal(host.coordinationBusy(),false);
});

// R26/R27 dropped learner responses cannot cancel a voter sleep marker by
// starting an unrelated quorum read; a dropped voting round still retries.
test('R26 R27 learner backpressure cannot invalidate an idle voting barrier',async()=>{
 const {PersonalPeers}=await import('../../src/raft/peers.js');
 const peers=Object.assign(Object.create(PersonalPeers.prototype),{
  refresh:async()=>{},blocked:new Set(),retryTargets:new Set(),
  view:{'1':{voter:true},'3':{voter:false},'4':{voter:true}},
  boxes:new Map([['4',{queues:[[],[{}],[]]}]]),limits:{Batch:1024,Slots:[2,1,1]},
  host:{asleep:false,codec:{inspect:()=>({snapshot:new Uint8Array()})},input:async()=>{}},
 });
 await peers.send({from:'3',to:'4',bytes:new Uint8Array()});assert.equal(peers.retryTargets.size,0);
 await peers.send({from:'1',to:'4',bytes:new Uint8Array()});assert(peers.retryTargets.has('4'));
});
