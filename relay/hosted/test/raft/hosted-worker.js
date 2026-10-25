// Only this loopback fixture exposes source seeding and storage inspection.
// Public requests use the production Worker and the actual identity/member DOs.
export * from '../../src/raft-worker.js';
import production from '../../src/raft-worker.js';
import { IdentityDO as Source } from '../../src/identity.js';
import { RaftMemberDO as Member } from '../../src/raft/member.js';
import { Directory } from '../../src/directory.js';
import { unb64 } from '../../src/raft/storage.js';
import {encodeCanonical} from '../../src/raft/codec.js';
import {CORE_MEMORY_BUDGET} from '../../src/raft/abi.js';
import { runtimeMetrics,sharedRuntime } from '../../src/raft/actor.js';
import {coordinationPolicy} from '../../src/raft/liveness.js';
const measurements={turns:0,wall:[],membership:0,campaigns:0,http:0,peerIn:0,peerOut:0,sweeps:0,writes:0};
async function measured(fn){const start=performance.now();measurements.turns++;try{return await fn();}finally{measurements.wall.push(performance.now()-start);}}
import { policyOf } from '../../src/rules.js';

export class IdentityDO extends Source {
  constructor(ctx,env){
    super(ctx,env);
    const accept=ctx.acceptWebSocket.bind(ctx);
    ctx.acceptWebSocket=(ws,tags)=>{accept(ws,tags);const send=ws.send.bind(ws);ws.send=raw=>{if(ws.deserializeAttachment()?.clusterPeer)measurements.peerOut++;return send(raw);};};
    for(const key of ['put','delete']){const call=ctx.storage[key].bind(ctx.storage);ctx.storage[key]=(...args)=>{measurements.writes++;return call(...args);};}
    for(const key of ['put','delete']){const call=ctx.storage.kv[key].bind(ctx.storage.kv);ctx.storage.kv[key]=(...args)=>{measurements.writes++;return call(...args);};}
    const exec=ctx.storage.sql.exec.bind(ctx.storage.sql);
    ctx.storage.sql.exec=(query,...args)=>{if(!/^\s*(SELECT|PRAGMA)/i.test(query))measurements.writes++;return exec(query,...args);};
  }
  get current(){return this.rebuilt??this;}
  async evictIdle(){
    const old=this.current.host;
    if(old){old.ready=false;old.members.clear();old.states.clear();old.queue=[];old.reads=[];old.peers=null;old.holds=null;}
    this.current.host=null;this.host=null;this.capturing=false;
    this.rebuilt=new Source(this.ctx,this.env);return this.quietState();
  }
  async quietState(){
    const config=await this.ctx.storage.get('raft-provision'),alarms={};
    if(config)for(const node of config.config.anchors)alarms[node]=await this.env.RAFT_MEMBERS.get(this.env.RAFT_MEMBERS.idFromName(config.config.cluster+'/'+node)).alarmState();
    return {ready:!!this.current.host?.ready,generation:await this.ctx.storage.get('raft-router-generation'),maintenance:await this.ctx.storage.get('raft-maintenance')??false,alarm:await this.ctx.storage.getAlarm(),memberAlarms:alarms,sockets:this.ctx.getWebSockets().map(ws=>({peer:!!ws.deserializeAttachment()?.clusterPeer,kind:ws.deserializeAttachment()?.kind,deadline:ws.deserializeAttachment()?.deadline}))};
  }
  async staleRetirement(){await this.ctx.storage.put('raft-retire-at',Date.now()-1);try{await this.presenceTurn();}catch{}return true;}
  async evictMinority(){
    const config=await this.ctx.storage.get('raft-provision'),namespace=this.env.RAFT_MEMBERS;
    const missing=new Set(config.config.anchors.slice(1).map(node=>namespace.idFromName(config.config.cluster+'/'+node).toString()));
    const unavailable=new Proxy({},{get:()=>async()=>{throw new Error('anchor unavailable');}});
    this.rebuilt=new Source(this.ctx,{...this.env,RAFT_MEMBERS:{idFromName:name=>namespace.idFromName(name),get:id=>missing.has(id.toString())?unavailable:namespace.get(id)}});
    return true;
  }
  async presenceSchedule(){
    const watchers=this.current.watchers,clock=watchers.liveness.clock,timestamp=this.ctx.getWebSocketAutoResponseTimestamp.bind(this.ctx),base=Date.now();let now=base,sweeps=0;
    const period=coordinationPolicy(sharedRuntime()).presence_every;
    // Only the loopback clock advances. Socket metadata and the production
    // alarm path still choose every sweep; no consensus timer is substituted.
    watchers.liveness.clock=()=>now;
    this.ctx.getWebSocketAutoResponseTimestamp=ws=>ws.deserializeAttachment()?.clusterPeer?timestamp(ws):new Date(base+Math.floor((now-base)/period)*period);
    try{
      for(;;){const at=watchers.liveness.nextSweep();if(at===undefined||at>base+600000)break;now=at;await this.presenceTurn();sweeps++;}
      return {milliseconds:600000,sweeps};
    }finally{watchers.liveness.clock=clock;this.ctx.getWebSocketAutoResponseTimestamp=timestamp;await this.ctx.storage.deleteAlarm();watchers.liveness.rearm();}
  }
  async presenceTurn(){measurements.sweeps++;return this.rebuilt?this.rebuilt.alarm():super.alarm();}
  alarm(){return this.presenceTurn();}
  webSocketMessage(ws,raw){if(ws.deserializeAttachment()?.clusterPeer)measurements.peerIn++;return this.rebuilt?this.rebuilt.webSocketMessage(ws,raw):super.webSocketMessage(ws,raw);}
  webSocketClose(ws,code){return this.rebuilt?this.rebuilt.webSocketClose(ws,code):super.webSocketClose(ws,code);}
  webSocketError(ws){return this.rebuilt?this.rebuilt.webSocketError(ws):super.webSocketError(ws);}
  async fetch(request){
    const result=await measured(()=>this.rebuilt?this.rebuilt.fetch(request):super.fetch(request));
    if(this.host&&!this.capturing){
      const commit=this.host.commit.bind(this.host);this.host.commit=async raw=>{this.lastCommand=raw.slice();return commit(raw);};this.capturing=true;
    }
    return result;
  }
  async sequenceProof(){return this.host.operation(async()=>{
    const from=this.host.manifest.config.anchors[0],to=this.host.manifest.config.anchors[1],term=Number(this.host.states.get(this.host.leader()).term);
    const payload=Uint8Array.of(8,8,16,Number(to),24,Number(from),32,term);
    const messages=await Promise.all(Array.from({length:16},()=>this.host.envelope(from,to,payload)));
    return {sequences:messages.map(raw=>JSON.parse(new TextDecoder().decode(raw)).sequence),ceiling:await this.ctx.storage.get('raft-transport-sequence')};
  });}
  peerIdle(){return {sleeping:this.host.asleep,bytes:this.host.peers.used,snapshots:this.host.peers.snapshots.size};}
  transportState(){return {keys:[...this.ctx.storage.kv.list()].map(([key])=>key),tables:this.ctx.storage.sql.exec("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_cf_%'").toArray().map(r=>r.name)};}
  async pausePublic(){
    let began;const started=new Promise(resolve=>{began=resolve;});
    this.ctx.waitUntil(this.host.operation(async()=>{began();await new Promise(resolve=>{this.releasePublic=resolve;});}));await started;
  }
  resumePublic(){this.releasePublic?.();}
  async checkpointState(){
    const id=this.host.leader();await this.host.members.get(id).hostCheckpoint();
    return this.host.members.get(id).transportCheckpoint();
  }
  holdUpload(){
    const put=this.host.tenant.store.put.bind(this.host.tenant.store);
    this.pauseUpload=new Promise(resolve=>{this.releaseUpload=resolve;});
    this.host.tenant.store.put=async(...args)=>{this.uploadStarted=true;await this.pauseUpload;return put(...args);};
  }
  releaseHeldUpload(){this.releaseUpload?.();}
  async retryWithoutQuorum(){
    return this.host.operation(async()=>{
      const leader=this.host.leader();for(const id of [...this.host.members.keys()])if(id!==leader)this.host.members.delete(id);
      this.host.queue=[];
      try{await this.host.commit(this.lastCommand);return 'acknowledged';}catch{return 'unavailable';}
    });
  }
  async seed(identity,records) {
    const dir=new Directory(this.ctx.storage.sql,identity,Date.now,policyOf());
    for(const r of records){
      if(r.raw){dir.assertLegacy();this.ctx.storage.sql.exec('INSERT OR REPLACE INTO dir VALUES(?,?,?)',r.kind,r.id,r.raw);}
      else dir.write(r.kind,r.id,r.doc);
    }
    await this.ctx.storage.put({identity,admitted:true});await this.ctx.storage.sync();
  }
  async replayStale() {
    return this.host.operation(async()=>{
      const from=this.host.leader(),to=this.host.manifest.config.anchors.find(id=>id!==from);
      const member=this.host.member(to),snapshot=await member.oldSnapshotAndCheckpoint();
      const uint=v=>{let n=BigInt(v),b=[];do{const x=Number(n&127n);n>>=7n;b.push(x|(n?128:0));}while(n);return b;};
      const field=(n,v)=>[...uint(n*8),...uint(v)];
      const raw=Uint8Array.from([...field(1,7),...field(2,to),...field(3,from),...field(4,this.host.states.get(from).term),...uint(9*8+2),...uint(snapshot.length),...snapshot]);
      this.host.queue.push({from,to,bytes:raw});await this.host.pump();return this.host.members.size;
    });
  }
  async leadershipTicks(count,missing){
    if(!this.current.host)await this.current.alarm();
    const host=this.current.host;
    return host.operation(async()=>{
      if(missing){host.members.delete(missing);host.states.delete(missing);}
      const errors=[],leaders=[],base=Date.now();
      for(let n=0;n<count;n++){
        const elapsed=Math.max(host.elapsed,this.virtualElapsed??0)+host.policy.tick_every;
        this.virtualElapsed=elapsed;
        try{await host.maintenance(base+elapsed,host.started+elapsed);}catch(error){errors.push(error.message);}
        leaders.push(host.leader()??'0');
      }
      const id=host.replica(),state=host.states.get(id),view=await host.members.get(id).hostView(state.applied,'membership','registry');
      return {leaders,errors,leader:host.leaderID(),cluster:host.manifest.config.cluster,configuration:host.configuration(view.app.conf),membership:JSON.parse(new TextDecoder().decode(view.raw)),campaigns:measurements.campaigns};
    });
  }
  async idleSchedule(){
    // The object may hibernate between the HTTP request and this fixture RPC.
    // Restore through the production alarm before driving its virtual clock.
    if(!this.current.host)await this.current.alarm();
    const host=this.current.host;
    return host.operation(async()=>{
    const base=Date.now(),offset=host.elapsed,membershipBefore=measurements.membership;
    const settle=()=>host.drainCoordination();
    await settle();
    for(let n=1;n<=20;n++){await host.maintenance(base+n*host.policy.tick_every,host.started+offset+n*host.policy.tick_every);await settle();}
    const id=host.leader()??host.replica(),before=host.states.get(id).applied;
    const sleeping=host.asleep;
    const turns=measurements.turns;
    for(let n=21;n<=140;n++)await host.maintenance(base+n*host.policy.tick_every,host.started+offset+n*host.policy.tick_every);
    return {sleeping,membershipCalls:measurements.membership-membershipBefore,appliedBefore:String(before),appliedAfter:String(host.states.get(id).applied),recurringMemberCalls:measurements.turns-turns,idleMilliseconds:120*host.policy.tick_every};
  });}
  inspect() {
    return {gate:this.ctx.storage.sql.exec('SELECT doc FROM legacy_gate WHERE one=1').toArray()[0]?.doc,
      originals:this.ctx.storage.sql.exec('SELECT kind,id,doc FROM dir ORDER BY kind,id').toArray()};
  }
}
export class RaftMemberDO extends Member {
  alarmState(){return this.ctx.storage.getAlarm();}
  alarm(...args){return measured(()=>super.alarm(...args));}
  importReceipt(...args){return measured(()=>super.importReceipt(...args));}
  hostSnapshot(...args){return measured(()=>super.hostSnapshot(...args));}
  hostCheckpoint(...args){return measured(()=>super.hostCheckpoint(...args));}
  acknowledgeScheduled(...args){return measured(()=>super.acknowledgeScheduled(...args));}
  schedule(...args){return measured(()=>super.schedule(...args));}
  initialize(...args){return measured(()=>super.initialize(...args));}
  drainRecovery(...args){return measured(()=>super.drainRecovery(...args));}
  scheduled(...args){return measured(()=>super.scheduled(...args));}
  sleepAck(...args){return measured(()=>super.sleepAck(...args));}
  refreshEnrollment(...args){return measured(()=>super.refreshEnrollment(...args));}
  hostPersonal(...args){return measured(()=>super.hostPersonal(...args));}
  membership(...args){measurements.membership++;return measured(()=>super.membership(...args));}
  hostInput(...args){if(args[0]===16)measurements.campaigns++;return measured(()=>super.hostInput(...args));}
  peer(...args){return measured(()=>super.peer(...args));}
  hostView(...args){return measured(()=>super.hostView(...args));}
  hostResult(...args){return measured(()=>super.hostResult(...args));}
  async transportCheckpoint(){await this.load();return this.actor.serial(()=>({applied:this.actor.store.application().applied,retained:this.actor.store.one('SELECT count(*) AS n FROM journal').n,snapshot:this.actor.codec.inspect(3,unb64(this.actor.store.get('snapshot'))).index.toString()}));}
  async seedTransportTail(){await this.load();await this.ctx.storage.put('raft-scheduled-part/interrupted/0',new Uint8Array([1,2,3]));await this.ctx.storage.sync();}
  async retainedControl(){return [...(await this.ctx.storage.list({prefix:'raft-'})).keys()].filter(key=>key!=='raft-erased');}
  async oldSnapshotAndCheckpoint(){
    await this.load();const old=await this.actor.serial(()=>unb64(this.actor.store.get('snapshot')));await this.actor.checkpoint();return old;
  }
  async discardStore() {
    // The next fixture action kills the runtime. This models lost durable
    // bytes while retaining the source's published identity and provenance.
    for(const {name} of this.ctx.storage.sql.exec("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '_cf_%'").toArray())this.ctx.storage.sql.exec(`DROP TABLE "${name}"`);
    await this.ctx.storage.sync();
  }
  hasStore() {return this.ctx.storage.sql.exec("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'").toArray().length>0;}
  async inspect() {
    await this.load();return this.actor.serial(()=>({app:this.actor.store.application(),identity:this.actor.store.identity,provenance:new TextDecoder().decode(this.actor.store.read('membership','legacy-import'))}));
  }
}
export default {
  async fetch(request,env) {
    const url=new URL(request.url);
    if(url.pathname==='/fixture'){
      const {op,identity,records,node,cluster,ticks,missing}=await request.json();
      if(op==='leadership-ticks')return new Response(encodeCanonical(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).leadershipTicks(ticks,missing)));
      if(op==='stale-retirement')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).staleRetirement());
      if(op==='diagnostic'){console.warn('local runtime warning capture');console.error('local runtime error capture');return Response.json(true);}
      if(op==='evict-minority')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).evictMinority());
      if(op==='presence-schedule')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).presenceSchedule());
      if(op==='quiet-state')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).quietState());
      if(op==='evict-idle')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).evictIdle());
      if(op==='presence-turn')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).presenceTurn()??true);
      if(op==='member-alarm')return Response.json(await env.RAFT_MEMBERS.get(env.RAFT_MEMBERS.idFromName(cluster+'/'+node)).alarmState());
      if(op==='peer-idle')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).peerIdle());
      if(op==='sequence-proof')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).sequenceProof());
      if(op==='transport-state')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).transportState());
      if(op==='pause-public'){await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).pausePublic();return Response.json(true);}
      if(op==='resume-public'){await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).resumePublic();return Response.json(true);}
      if(op==='checkpoint-state')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).checkpointState());
      if(op==='metrics')return Response.json({coreBudget:CORE_MEMORY_BUDGET,...runtimeMetrics(),...measurements});
      if(op==='reset-metrics'){for(const key of Object.keys(measurements))measurements[key]=key==='wall'?[]:0;return Response.json(true);}
      if(op==='seed'){await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).seed(identity,records);return Response.json(true);}
      if(op==='hold-upload'){await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).holdUpload();return Response.json(true);}
      if(op==='release-upload'){await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).releaseHeldUpload();return Response.json(true);}
      if(op==='retry-without-quorum')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).retryWithoutQuorum());
      if(op==='stale-snapshot')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).replayStale());
      if(op==='idle-schedule')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).idleSchedule());
      if(op==='source')return Response.json(await env.IDENTITY.get(env.IDENTITY.idFromName(identity)).inspect());
      if(op==='seed-transport-tail'){await env.RAFT_MEMBERS.get(env.RAFT_MEMBERS.idFromName(cluster+'/'+node)).seedTransportTail();return Response.json(true);}
      if(op==='retained-control')return Response.json(await env.RAFT_MEMBERS.get(env.RAFT_MEMBERS.idFromName(cluster+'/'+node)).retainedControl());
      if(op==='discard-member'){await env.RAFT_MEMBERS.get(env.RAFT_MEMBERS.idFromName(cluster+'/'+node)).discardStore();return Response.json(true);}
      if(op==='has-member')return Response.json(await env.RAFT_MEMBERS.get(env.RAFT_MEMBERS.idFromName(cluster+'/'+node)).hasStore());
      if(op==='member')return Response.json(await env.RAFT_MEMBERS.get(env.RAFT_MEMBERS.idFromName(cluster+'/'+node)).inspect());
      return new Response(null,{status:404});
    }
    measurements.http++;return production.fetch(request,env);
  }
};
