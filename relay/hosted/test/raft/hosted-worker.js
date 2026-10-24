// Only this loopback fixture exposes source seeding and storage inspection.
// Public requests use the production Worker and the actual identity/member DOs.
export * from '../../src/raft-worker.js';
import production from '../../src/raft-worker.js';
import { IdentityDO as Source } from '../../src/identity.js';
import { RaftMemberDO as Member } from '../../src/raft/member.js';
import { Directory } from '../../src/directory.js';
import { unb64 } from '../../src/raft/storage.js';
import { runtimeMetrics } from '../../src/raft/actor.js';
const measurements={turns:0,wall:[]};
async function measured(fn){const start=performance.now();measurements.turns++;try{return await fn();}finally{measurements.wall.push(performance.now()-start);}}
import { policyOf } from '../../src/rules.js';

export class IdentityDO extends Source {
  async fetch(request){
    const result=await measured(()=>super.fetch(request));
    if(this.host&&!this.capturing){
      const commit=this.host.commit.bind(this.host);this.host.commit=async raw=>{this.lastCommand=raw.slice();return commit(raw);};this.capturing=true;
    }
    return result;
  }
  holdUpload(){
    const put=this.host.tenant.store.put.bind(this.host.tenant.store);
    this.pauseUpload=new Promise(resolve=>{this.releaseUpload=resolve;});
    this.host.tenant.store.put=async(...args)=>{this.uploadStarted=true;await this.pauseUpload;return put(...args);};
  }
  releaseHeldUpload(){this.releaseUpload?.();}
  async retryWithoutQuorum(){
    return this.host.serial(async()=>{
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
    return this.host.serial(async()=>{
      const from=this.host.leader(),to=this.host.manifest.config.anchors.find(id=>id!==from);
      const member=this.host.member(to),snapshot=await member.oldSnapshotAndCheckpoint();
      const uint=v=>{let n=BigInt(v),b=[];do{const x=Number(n&127n);n>>=7n;b.push(x|(n?128:0));}while(n);return b;};
      const field=(n,v)=>[...uint(n*8),...uint(v)];
      const raw=Uint8Array.from([...field(1,7),...field(2,to),...field(3,from),...field(4,this.host.states.get(from).term),...uint(9*8+2),...uint(snapshot.length),...snapshot]);
      this.host.queue.push({from,to,bytes:raw});await this.host.pump();return this.host.members.size;
    });
  }
  async idleSchedule(){
    // R26 allows eviction between the HTTP request and this fixture RPC.
    // Restore through the production alarm before driving its virtual clock.
    if(!this.host)await super.alarm();
    return this.host.serial(async()=>{
    const base=Date.now();
    for(let n=1;n<=20;n++)await this.host.maintenance(base+n*this.host.policy.tick_every,this.host.started+n*this.host.policy.tick_every);
    const id=this.host.leader(),before=this.host.states.get(id).applied;
    const sleeping=this.host.asleep;
    const turns=measurements.turns;
    for(let n=21;n<=140;n++)await this.host.maintenance(base+n*this.host.policy.tick_every,this.host.started+n*this.host.policy.tick_every);
    return {sleeping,appliedBefore:String(before),appliedAfter:String(this.host.states.get(id).applied),recurringMemberCalls:measurements.turns-turns,idleMilliseconds:120*this.host.policy.tick_every};
  });}
  inspect() {
    return {gate:this.ctx.storage.sql.exec('SELECT doc FROM legacy_gate WHERE one=1').toArray()[0]?.doc,
      originals:this.ctx.storage.sql.exec('SELECT kind,id,doc FROM dir ORDER BY kind,id').toArray()};
  }
}
export class RaftMemberDO extends Member {
  hostInput(...args){return measured(()=>super.hostInput(...args));}
  peer(...args){return measured(()=>super.peer(...args));}
  hostView(...args){return measured(()=>super.hostView(...args));}
  hostResult(...args){return measured(()=>super.hostResult(...args));}
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
      const {op,identity,records,node,cluster}=await request.json();
      if(op==='metrics')return Response.json({...runtimeMetrics(),...measurements});
      if(op==='reset-metrics'){measurements.turns=0;measurements.wall=[];return Response.json(true);}
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
    return production.fetch(request,env);
  }
};
