import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {generateKeyPairSync,sign,verify} from 'node:crypto';
import {HostCodec,Reactor} from '../../src/raft/abi.js';
import {PersonalForward} from '../../src/raft/forward.js';
import {encodeCanonical} from '../../src/raft/codec.js';
import {HostedIdentity} from '../../src/raft/hosted.js';
import {PersonalPeers,peerLimits} from '../../src/raft/peers.js';
const runtime=new Reactor(new WebAssembly.Module(readFileSync(process.env.CODEAF_RAFT_PROOF_DIR+'/core.wasm')));
const keys=()=>{const pair=generateKeyPairSync('ed25519');return {...pair,raw:pair.publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64'),private:pair.privateKey.export({type:'pkcs8',format:'der'}).toString('base64')};};
function rig(){
 const anchor=keys(),personal=keys(),scope={identity:'identity',cluster:'cluster',epoch:'1',protocol:1},sent=[];
 const ws={deserializeAttachment:()=>({kind:'forward',node:'4',generation:'2',auth:''}),send:raw=>sent.push(raw)};
 const host=Object.assign(Object.create(HostedIdentity.prototype),{runtime,tail:Promise.resolve(),pending:0,operationTail:Promise.resolve(),operations:0,waiters:new Set(),provisioning:{anchors:[anchor.private]},manifest:{config:{anchors:['1','2','3']}},peers:{scope,generation:'2',limits:peerLimits(runtime),view:{'4':{key:personal.raw}},sockets:()=>[ws],validate(){}}});
 host.forwarding=new PersonalForward(host);
 const reply=(id,barrier=10)=>{
  const value={scope,generation:'2',from:'4',to:'1',id,action:'read',barrier,status:{Leader:4,Term:2,Applied:barrier},applied:barrier,signature:null};
  value.signature=sign(null,Buffer.from('codeaf/cluster/forward/1\n'+JSON.stringify(value)),personal.privateKey).toString('base64');
  const raw=Buffer.from(JSON.stringify(value));return raw.buffer.slice(raw.byteOffset,raw.byteOffset+raw.byteLength);
 };
 return {host,ws,sent,anchor,personal,reply};
}

// R6 public completion releases the member turn, preserving peer progress.
test('R6 a forwarded public wait leaves coordinator and peer replies live',async()=>{
 const r=rig();let completed=false;
 const waiting=r.host.operation(()=>r.host.forward('4',{action:'read',id:'one'})).then(reply=>{completed=true;return reply;});
 await r.host.serial(()=>{});await r.host.serial(()=>{});
 assert.equal(completed,false);assert.equal(r.host.forwarding.pending.size,1);
 const frame=JSON.parse(new TextDecoder().decode(r.sent[0]));const signature=Buffer.from(frame.signature,'base64');frame.signature=null;
 assert(verify(null,Buffer.from('codeaf/cluster/forward/1\n'+JSON.stringify(frame)),r.anchor.publicKey,signature));
 r.host.forwarding.frame(r.ws,r.reply('one'));
 const result=await waiting;assert.equal(result.barrier,'10');assert.equal(r.host.forwarding.pending.size,0);
});

// R6 signatures bind the exact reply, connection, scope and request token.
test('R6 altered or cross-connection replies cannot complete a public request',async()=>{
 const r=rig(),token=await r.host.serial(()=>r.host.forwarding.begin('4',{action:'read',id:'two'}));
 const raw=r.reply('two'),value=JSON.parse(new TextDecoder().decode(raw));value.barrier++;
 assert.throws(()=>r.host.forwarding.frame(r.ws,new TextEncoder().encode(JSON.stringify(value)).buffer));
 r.host.forwarding.frame({deserializeAttachment:r.ws.deserializeAttachment},raw);assert.equal(r.host.forwarding.pending.size,1);
 r.host.forwarding.frame(r.ws,raw);assert.equal((await token.promise).barrier,'10');
});

// R6/R27 application admission has the existing transport inflight bound.
test('R6 R27 application inflight admission stays bounded and disconnection is retryable',async()=>{
 const r=rig(),tokens=[];
 for(let n=0;n<r.host.peers.limits.Inflight;n++)tokens.push(await r.host.forwarding.begin('4',{action:'read',id:'bounded-'+n}));
 await assert.rejects(r.host.forwarding.begin('4',{action:'read',id:'overflow'}));
 r.host.forwarding.departed(r.ws);assert((await Promise.all(tokens.map(t=>t.promise))).every(v=>v===null));assert.equal(r.host.forwarding.pending.size,0);
});

// R6 a stale address cannot allocate a new request or supply cached success.
test('R6 stale external hints retry the original request ID',async()=>{
 const calls=[];let target='4';
 const host=Object.assign(Object.create(HostedIdentity.prototype),{members:new Map([['1',{}]]),leader:()=>null,leaderID:()=>target,input:async()=>{target='5';},pump:async()=>{},forward:async(node,work)=>{calls.push({node,...work});if(node==='4')throw Object.assign(Error('retry'),{forwardID:work.id});return {id:work.id,barrier:'12'};}});
 const result=await host.remote({action:'read',id:'original'});assert.equal(result.barrier,'12');assert.deepEqual(calls.map(c=>[c.node,c.id]),[['4','original'],['5','original']]);
});

// R6/R22 committed renewal preparation retains exact unsigned ownership fences.
test('R6 R22 hosted effects preserve canonical renewal fences',async()=>{
 const r=rig(),fence='9007199254740993';
 r.host.provisioning.effects=Buffer.from(r.anchor.private,'base64').subarray(-32).toString('base64');
 r.host.manifest.config.cluster='cluster';
 r.host.commit=async raw=>({value:JSON.parse(new TextDecoder().decode(raw))});
 const command=await r.host.commitEffect({renew_holds:{device:'personal',seen_at:1000,items:[{cell:'cell',fence}]}},1000);
 assert.equal(command.change.renew_holds.items[0].fence,fence);
 assert.equal(command.change.renew_holds.seen_at,1000);
});

// R6 a changed local leader must perform the original request's fresh read.
test('R6 stale external hint retries the same ReadIndex context on a hosted leader',async()=>{
 let local=false;const inputs=[];
 const host=Object.assign(Object.create(HostedIdentity.prototype),{reads:[],peers:{limits:peerLimits(runtime)},elect:async()=>{},leader:()=>local?'1':undefined,remote:async work=>{assert.equal(work.id,'same-read');local=true;throw Error('stale address');},input:async(node,tag,id)=>{inputs.push({node,tag,id});if(tag===4)host.reads.push({node,id,index:9n});},pump:async()=>{}});
 const read=await host.barrier('same-read');assert.equal(read.index,9n);
 assert.deepEqual(inputs.filter(i=>i.tag===4),[{node:'1',tag:4,id:'same-read'}]);
});

// R6 a missing old connection still reserves one ID before refreshing its hint.
test('R6 a stale disconnected hint cannot allocate a second forwarding ID',async()=>{
 const calls=[];let target='4';
 const host=Object.assign(Object.create(HostedIdentity.prototype),{members:new Map([['1',{}]]),leader:()=>null,leaderID:()=>target,input:async()=>{target='5';},pump:async()=>{},forward:async(node,work)=>{calls.push({node,...work});if(node==='4')throw Error('connection unavailable');return {id:work.id,barrier:'12'};}});
 await host.remote({action:'read'});assert.equal(typeof calls[0].id,'string');assert.equal(calls[0].id,calls[1].id);
});

// R6/R9 a joining home must receive the append after its snapshot without
// blocking delivery of the personal leader's response on the peer queue.
test('R6 R9 HTTP catch-up lets peer progress finish before returning an empty response',{timeout:peerLimits(runtime).ForwardTimeout},async()=>{
 let delivered=false;
 const peer=Object.assign(Object.create((await import('../../src/raft/peers.js')).PersonalPeers.prototype),{tail:Promise.resolve(),pending:0,view:{'5':{voter:false}},refresh:async()=>{},validate:()=>{},wire:()=>new TextEncoder().encode(JSON.stringify({messages:delivered?['append']:[]})),exchange:async()=>({request:{node:'5',applied:'14',messages:[]},raw:new TextEncoder().encode('{"messages":[]}'),rejected:false})});
 const host={peers:peer,asleep:false,leader:()=>undefined,leaderID:()=> '4',replica:()=> '1',states:new Map([['1',{applied:15n}]]),forwarding:{socket:()=>({})},forward:async(node,work)=>{assert.equal(node,'4');assert.equal(work.action,'poke');await peer.serial(()=>{delivered=true;});return {barrier:'15'};}};
 peer.host=host;
 const result=await peer.httpExchange(new Uint8Array());assert.deepEqual(JSON.parse(new TextDecoder().decode(result.raw)).messages,['append']);
});

// R6/R9 admitting another home cannot discard the elected computer's in-flight
// quorum reply while publishing the new home’s transport authority.
test('R6 R9 enrollment retains admitted traffic between unchanged members',async()=>{
 const r=rig(),members={'1':{incarnation:'1',key:r.anchor.raw,voter:true,revoked:false},'4':{incarnation:'1',key:r.personal.raw,voter:true,revoked:false}};
 let view=members;
 const host={runtime,config:()=>({scope:r.host.peers.scope}),states:new Map([['1',{applied:13n}]]),leader:()=>undefined,replica:()=> '1',leaderID:()=> '4',members:new Map([['1',{refreshEnrollment:async()=>view}]]),ctx:{storage:{put:async()=>{},sync:async()=>{}}},voterHint:['1','4'],forwarding:{socket:()=>null},input:async()=>{},hash:()=>'',asleep:false};
 const peers=new PersonalPeers(host,'2');host.peers=peers;peers.sockets=()=>[];await peers.refresh();
 peers.enqueue([{from:'4',to:'1',sequence:'7',payload:'AQ=='}]);
 const before=structuredClone(peers.deliveries('1')),used=[...peers.used];
 view={...members,'5':{incarnation:'1',key:r.personal.raw,voter:false,revoked:false}};host.states.get('1').applied=14n;
 await peers.refresh();
 assert.deepEqual(peers.deliveries('1'),before);assert.deepEqual(peers.used,used);assert(peers.members().some(p=>p.node==='5'));
 view={...view,'4':{...members['4'],revoked:true}};host.states.get('1').applied=15n;await peers.refresh();
 assert.deepEqual(peers.deliveries('1'),[]);assert.deepEqual(peers.used,[0,0,0]);
});

// R6/R9 concurrent publication cannot restore an older transport authority.
test('R6 R9 overlapping membership refreshes leave the latest committed view',async()=>{
 const first=Promise.withResolvers(),second=Promise.withResolvers();let calls=0;
 const old={'1':{incarnation:'1',key:'anchor',voter:true,revoked:false}},newer={...old,'5':{incarnation:'1',key:'personal',voter:false,revoked:false}};
 const host={runtime,config:()=>({scope:rig().host.peers.scope}),states:new Map([['1',{applied:13n}]]),leader:()=>undefined,replica:()=> '1',leaderID:()=> '4',members:new Map([['1',{refreshEnrollment:()=>++calls===1?first.promise:second.promise}]]),ctx:{storage:{put:async()=>{},sync:async()=>{}}},voterHint:['1'],forwarding:{socket:()=>null},input:async()=>{},asleep:false};
 const peers=new PersonalPeers(host,'2');host.peers=peers;peers.sockets=()=>[];
 const earlier=peers.refresh();host.states.get('1').applied=14n;const later=peers.refresh();
 second.resolve(newer);await Promise.resolve();await Promise.resolve();first.resolve(old);
 await Promise.all([earlier,later]);assert(peers.members().some(p=>p.node==='5'));
});

// R6/R27 a saturated transport may drop a read round, but draining its bounded
// queue must obtain a new quorum round without holding the peer queue.
test('R6 R27 a drained voter queue retries a dropped quorum round',async()=>{
 const r=rig(),view={'1':{incarnation:'1',key:r.anchor.raw,voter:true,revoked:false},'4':{incarnation:'1',key:r.personal.raw,voter:true,revoked:false}},reply=Promise.withResolvers(),actions=[];
 const host={runtime,codec:new HostCodec(runtime),config:()=>({scope:r.host.peers.scope}),states:new Map([['1',{applied:13n}]]),leader:()=>undefined,replica:()=> '1',leaderID:()=> '4',members:new Map([['1',{refreshEnrollment:async()=>view}]]),ctx:{storage:{put:async()=>{},sync:async()=>{}}},voterHint:['1','4'],forwarding:{socket:()=>({}),begin:async(node,work)=>{actions.push([node,work.action]);return {id:'retry',promise:reply.promise};}},serial:fn=>fn(),input:async()=>{},pump:async()=>{},wakeMaintenance:async()=>{},asleep:false};
 const peers=new PersonalPeers(host,'2');host.peers=peers;peers.sockets=()=>[];await peers.refresh();actions.length=0;
 peers.anchors=async()=>{};peers.chunks=async()=>{};peers.push=async()=>{};peers.validate=raw=>({request:JSON.parse(new TextDecoder().decode(raw)),obsolete:new Set()});
 for(let n=0;n<peers.limits.Slots[0];n++)peers.enqueue([{from:'1',to:'4',sequence:String(n+1),payload:'CAkQBBgBIAM='}]);
 const deliveries=peers.deliveries('4');await peers.send({from:'1',to:'4',bytes:Uint8Array.of(8,9,16,4,24,1,32,3)});assert.equal(peers.deliveries('4').length,peers.limits.Slots[0]);
 const request=new TextEncoder().encode(JSON.stringify({node:'4',applied:'13',ack:[deliveries.at(-1).cursor,'0','0'],messages:[]}));
 await peers.exchange(request);await peers.serial(()=>{});assert.deepEqual(actions,[['4','poke']]);
 await peers.exchange(request);assert.deepEqual(actions,[['4','poke']]);reply.resolve(null);
});

// R6 a leadership change between evidence and dispatch resumes through a fresh
// barrier instead of turning the successful election into a refusal.
test('R6 leadership planning retries a route that changed during its member RPC',async()=>{
 let leader='1',polled=false;
 const host=Object.assign(Object.create(HostedIdentity.prototype),{
  barrierRecord:async()=>new TextEncoder().encode(encodeCanonical({revision:'3',leadership:{node:'4',standby:'3',restore:false,barrier:'10'}})),
  leader:()=>leader,members:new Map([['1',{leadership:async()=>{throw Error('raftcore: operation requires the elected leader');}}]]),
  forward:async()=>({evidence:{node:'4'}}),input:async(id,tag)=>{assert.equal(id,'1');assert.equal(tag,0);polled=true;leader=undefined;},pump:async()=>{},
 });
 assert.equal(await host.reconcileLeadership(),true);assert(polled);
});

// R6 a hosted address that becomes stale must preserve the same public read
// context when the elected computer takes over during the short dispatch.
test('R6 stale hosted hint retries the original ReadIndex context on the personal leader',async()=>{
 let local=true;const ids=[];
 const host=Object.assign(Object.create(HostedIdentity.prototype),{members:new Map([['1',{}]]),peers:{limits:peerLimits(runtime)},elect:async()=>{},leader:()=>local?'1':undefined,leaderID:()=>local?'1':'4',
  input:async(node,tag,id)=>{if(tag===4){ids.push(id);throw Error('raftcore: operation requires the elected leader');}assert.equal(tag,0);local=false;},pump:async()=>{},
  remote:async work=>{ids.push(work.id);return {barrier:'17'};},appliedReplica:async index=>{assert.equal(index,'17');return {node:'1',index:17n};},
 });
 assert.equal((await host.barrier('original-local-read')).index,17n);assert.deepEqual(ids,['original-local-read','original-local-read']);
});

// R6 a changed local address cannot replace an already signed proposal.
test('R6 stale hosted hint forwards the original signed proposal unchanged',async()=>{
 const command=Uint8Array.of(1,2,3);let local=true;const proposals=[];
 const host=Object.assign(Object.create(HostedIdentity.prototype),{members:new Map([['1',{}]]),peers:{refresh:async()=>{},limits:peerLimits(runtime)},elect:async()=>{},leader:()=>local?'1':undefined,leaderID:()=>local?'1':'4',
  input:async(node,tag,raw)=>{if(tag===3){proposals.push([...raw]);throw Error('raftcore: operation requires the elected leader');}assert.equal(tag,0);local=false;},pump:async()=>{},
  remote:async work=>{proposals.push([...Buffer.from(work.command,'base64')]);return {applied:'18',result:{code:'ok'}};},appliedReplica:async index=>assert.equal(index,'18'),
 });
 assert.equal((await host.commit(command)).value.code,'ok');assert.deepEqual(proposals,[[1,2,3],[1,2,3]]);
});

// R6 losing the hosted leader after dispatch also resumes the same operation.
test('R6 an unfinished hosted read follows the new leader with its original context',async()=>{
 let local=true,waits=0;const ids=[];
 const host=Object.assign(Object.create(HostedIdentity.prototype),{members:new Map([['1',{}]]),reads:[],peers:{limits:peerLimits(runtime)},elect:async()=>{},externalVoter:()=>true,leader:()=>local?'1':undefined,leaderID:()=>local?'1':'4',
  input:async(node,tag,id)=>{if(tag===4)ids.push(id);else assert([0,10].includes(tag));},pump:async()=>{},progress:async()=>{if(waits++)throw Error('read did not follow the new leader');local=false;},
  remote:async work=>{ids.push(work.id);return {barrier:'17'};},appliedReplica:async()=>({node:'1',index:17n}),
 });
 assert.equal((await host.barrier('pending-local-read')).index,17n);assert.deepEqual(ids,['pending-local-read','pending-local-read']);
});

// R6 an admitted proposal keeps its signature and session through a leader change.
test('R6 an unfinished hosted proposal follows the new leader without rewriting it',async()=>{
 const command=Uint8Array.of(1,2,3);let local=true,waits=0;const proposals=[];
 const host=Object.assign(Object.create(HostedIdentity.prototype),{members:new Map([['1',{hostResult:async()=>null}]]),peers:{refresh:async()=>{},limits:peerLimits(runtime)},elect:async()=>{},externalVoter:()=>true,leader:()=>local?'1':undefined,leaderID:()=>local?'1':'4',
  input:async(node,tag,raw)=>{if(tag===3){proposals.push([...raw]);return {proposalAfter:1n};}assert.equal(tag,0);},pump:async()=>{},progress:async()=>{if(waits++)throw Error('proposal did not follow the new leader');local=false;},
  remote:async work=>{proposals.push([...Buffer.from(work.command,'base64')]);return {applied:'18',result:{code:'ok'}};},appliedReplica:async()=>{},
 });
 assert.equal((await host.commit(command)).value.code,'ok');assert.deepEqual(proposals,[[1,2,3],[1,2,3]]);
});
