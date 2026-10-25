import { clusterIngress } from './ingress.js';
import { workerCell,workerAssignment } from './worker.js';
import { DurableObject } from 'cloudflare:workers';
import { MemberActor, sharedRuntime,replaceMemberActivation } from './actor.js';
import { MemberInbox, INBOX_BYTES } from './inbox.js';
import { directoryEvaluator,projectRecord } from './directory.js';
import { DEFAULT_ROWS_BYTES, MemberStorage, b64 } from './storage.js';
import { HostCodec,frame,response } from './abi.js';
import { encodeCanonical, decodeCanonical } from './codec.js';
import { dropTables } from '../erasure.js';
import { committedRegistry, provisionedRegistry } from './membership.js';
import { MemberAlarms, coordinationPolicy } from './liveness.js';
import { stageLegacyImport, finishLegacyImport } from './legacy.js';
export { installRaftModule, runtimeMetrics } from './actor.js';

// Only internal namespace calls can initialize a provisioned member. The
// public Worker routes identity coordination through its authenticated source.
// Enrollment and evaluation use the durable Go registry and shared Go policy.
export class RaftMemberDO extends DurableObject {
  constructor(ctx, env) { super(ctx, env); replaceMemberActivation(ctx.id.toString()); this.actor = null; this.loading = null; this.ctx = ctx; this.env = env; }
  evaluator(config) {
    const evaluate=directoryEvaluator(sharedRuntime(),config.cluster,config.ceiling??DEFAULT_ROWS_BYTES);
    this.results=new Map();this.resultBytes=0;
    return (raw,read,app)=>{
      const result=evaluate(raw,read,app),hash=new HostCodec(sharedRuntime()).hash(raw).digest;
      const key=b64(hash),previous=this.results.get(key);
      if(previous){this.resultBytes-=previous.raw.length;this.results.delete(key);}
      this.results.set(key,{index:BigInt(app.index),raw:result.result});this.resultBytes+=result.result.length;
      while(this.results.size>1 && (this.results.size>32||this.resultBytes>1024*1024)){
        const first=this.results.keys().next().value;this.resultBytes-=this.results.get(first).raw.length;this.results.delete(first);
      }
      return result;
    };
  }
  // These RPC methods have no public fetch route. The authenticated source
  // coordinator alone provisions stores and drives the same durable actor.
  async importReceipt(manifest) {
    if(await this.ctx.storage.get('raft-erased'))throw new Error('raft member: identity retired');
    const config=await this.ctx.storage.get('raft-config');
    if(!config)return null;
    const store=new MemberStorage(this.ctx.storage,new HostCodec(sharedRuntime()));
    if(!store.one("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'"))return null;
    await store.open({cluster:config.cluster,id:config.id,protocol:1},config.ceiling);
    const raw=store.read('membership','legacy-import');
    if(!raw)return null;
    if(new TextDecoder().decode(raw)!==encodeCanonical(manifest))throw new Error('raft member: import provenance differs');
    return {node:config.id,applied:store.application().applied,manifest:decodeCanonical(new TextDecoder().decode(raw))};
  }
  async eraseRetired(manifest) {
    const gone=await this.ctx.storage.get('raft-erased');
    if(gone && gone!==manifest.config.cluster)throw new Error('raft member: wrong erased cluster');
    if(!gone){
      if(!await this.importReceipt(manifest))throw new Error('raft member: import provenance absent');
      await this.load();
      await this.actor.serial(async()=>{
        const raw=this.actor.store.read('state','identity'),record=raw&&JSON.parse(projectRecord(sharedRuntime(),'identity',raw));
        if(record?.rotation?.state!=='retired'||record.rotation.retire_at>Date.now())throw new Error('raft member: erasure not authorized');
        await this.ctx.storage.put('raft-erased',manifest.config.cluster);await this.ctx.storage.sync();this.erased=true;
      });
    }
    if(this.actor?.handle)await this.actor.disposeErased();
    dropTables(this.ctx.storage.sql);
    // A retry after the tombstone must finish KV transport cleanup too. Parts
    // may outlive a lost manifest reply; none are an owner-retained backup.
    const keys=[...(await this.ctx.storage.list({prefix:'raft-'})).keys()].filter(key=>key!=='raft-erased');
    if(keys.length)await this.ctx.storage.delete(keys);
    await this.ctx.storage.delete('legacy-pending');await this.ctx.storage.sync();
  }
  async hostInput(tag,value) {
    if(![0,1,3,4,10,12,15,16].includes(tag))throw new Error('raft member: unsupported coordinator input');
    await this.load();
    if(tag===3){
      const raw=value.slice();return this.actor.serial(async()=>{
        const proposalAfter=this.actor.status.applied;
        return {...await this.actor.drive(tag,raw),proposalAfter};
      });
    }
    return this.actor.input(tag,value);
  }
  async hostView(index,namespace,key,after=null) {
    await this.load();return this.actor.serial(()=>{
      if(this.actor.stopped||this.actor.store.stopped||this.actor.status.applied<BigInt(index))throw new Error('raft member: read barrier incomplete');
      const app=this.actor.store.application();
      if(after===null)return {app,raw:this.actor.store.read(namespace,key)};
      const rows=[];let size=0;
      for(const row of this.actor.store.rows('SELECT key FROM records WHERE namespace=? AND key>? ORDER BY key LIMIT 64',namespace,after)){
        const raw=this.actor.store.read(namespace,row.key);if(rows.length&&size+raw.length>1024*1024)break;
        rows.push({key:row.key,data:raw});size+=raw.length;
      }
      return {app,rows};
    });
  }
  // The coordinator's fresh ReadIndex must have applied before a scoped
  // read runs. Authority/time and selected rows share this member queue.
  async hostSelected(raw,index) {
    await this.load();return this.actor.serial(()=>{
      if(this.actor.stopped||this.actor.store.stopped||this.actor.status.applied<BigInt(index))throw new Error('raft member: read barrier incomplete');
      const app=this.actor.store.application();
      const {actor,request:q}=clusterIngress(sharedRuntime(),raw,app.now);
      if(q.cluster!==this.config.cluster)throw new Error('raft member: wrong cluster');
      return q.action==='cell'
        ?workerCell(sharedRuntime(),this.actor.store,q.cluster,actor,q.cell,q.incarnation)
        :workerAssignment(sharedRuntime(),this.actor.store,q.cluster,actor,q.incarnation,q.assignment);
    });
  }
  async hostResult(raw,after=0n) {
    await this.load();return this.actor.serial(()=>{
      if(this.actor.stopped||this.actor.store.stopped)throw new Error('raft member: stopped');
      const value=this.results.get(b64(this.actor.codec.hash(raw).digest));
      return value&&value.index>BigInt(after)&&value.index<=this.actor.status.applied?value:null;
    });
  }
  async hostPersonal(index,input){
    await this.load();return this.actor.serial(()=>{
      if(this.actor.stopped||this.actor.status.applied<BigInt(index))throw new Error('raft member: read barrier incomplete');
      const app=this.actor.store.application(),rows=[];
      const request={...input,cluster:this.config.cluster,applied:app.applied,now:Math.max(Date.now(),Number(app.now)),committed_now:Number(app.now),conf:app.conf,rows};
      for(let n=0;n<coordinationPolicy(sharedRuntime()).max_member_records;n++){
        const r=response(sharedRuntime().call(frame(36).blob(new TextEncoder().encode(encodeCanonical(request))).finish())),out=decodeCanonical(new TextDecoder().decode(r.blob()));r.end();
        if(!out.need?.length)return out;
        for(const k of out.need){const raw=this.actor.store.read(k.namespace,k.key);rows.push({namespace:k.namespace,key:k.key,data:raw?b64(raw):null});}
      }
      throw new Error('raft member: personal read-set capacity');
    });
  }
  async hostCheckpoint() {await this.load();return this.actor.checkpoint();}
  async hostSnapshot(op,...args) {
    await this.load();
    if(op==='begin')return this.actor.begin(...args);
    if(op==='chunk')return this.actor.chunk(...args);
    if(op==='complete')return this.actor.complete(...args);
    if(op==='read')return this.actor.serial(()=>this.actor.store.chunk(...args));
    throw new Error('raft member: unsupported snapshot operation');
  }
  // Source migration feeds bounded immutable rows before a core can activate.
  // The durable pending marker fences initialize/load across process death.
  legacyImport(config, manifest, row = null, finish = false) {
    if(this.actor||this.loading||this.importWork)return Promise.reject(new Error('raft member: active or import busy; retry'));
    config=structuredClone(config);manifest=structuredClone(manifest);row=row&&structuredClone(row);
    this.importPending=true;
    const run=Promise.resolve().then(async()=>{
      const store=new MemberStorage(this.ctx.storage,new HostCodec(sharedRuntime()));
      const canonical={...config,id:String(config.id),seed:String(config.seed),bootstrap:manifest.config.anchors.map(String),ceiling:Number(manifest.ceiling)};
      const previous=await this.ctx.storage.get('raft-config');
      if(previous&&JSON.stringify(previous)!==JSON.stringify(canonical))throw new Error('raft member: provisioned identity cannot change');
      await this.ctx.storage.put('legacy-pending',true);await this.ctx.storage.sync();
      if(!previous){await this.ctx.storage.put('raft-config',canonical);await this.ctx.storage.sync();}
      await store.open({cluster:canonical.cluster,id:canonical.id,protocol:1},canonical.ceiling);
      await stageLegacyImport(store,manifest,row);
      if(!finish)return;
      const snapshot=await finishLegacyImport(store,manifest);
      await this.ctx.storage.delete('legacy-pending');await this.ctx.storage.sync();this.importPending=false;
      return snapshot;
    });
    this.importWork=run;return run.finally(()=>{this.importWork=null;});
  }
  async initialize(config) {
    if(await this.ctx.storage.get('legacy-pending')||this.importPending)throw new Error('raft member: import incomplete; retry');
    config = { ...config, id: String(config.id), seed: String(config.seed), bootstrap: config.bootstrap.map(String) };
    if (this.loading) {
      if (this.loadingConfig !== JSON.stringify(config)) throw new Error('raft member: different activation is in progress');
      return this.loading;
    }
    this.loadingConfig = JSON.stringify(config);
    this.loading = this.activate(config).then(effects=>({...effects,elapsed:this.actor.elapsed??0}));
    try { return await this.loading; } finally { this.loading = null; this.loadingConfig = null; }
  }
  async activate(config) {
    if (this.actor) {
      if (JSON.stringify(this.config) !== JSON.stringify(config)) throw new Error('raft member: provisioned identity cannot change');
      return this.actor.input(0);
    }
    const canonical = { ...config, id: String(config.id), seed: String(config.seed), bootstrap: config.bootstrap.map(String) };
    const previous = await this.ctx.storage.get('raft-config');
    if (previous && JSON.stringify(previous) !== JSON.stringify(canonical)) throw new Error('raft member: provisioned identity cannot change');
    if (!previous) { await this.ctx.storage.put('raft-config', canonical); await this.ctx.storage.sync(); }
    const actor = new MemberActor(this.ctx.storage, sharedRuntime(), this.evaluator(config),this.ctx.id.toString());
    const result = await actor.init(canonical);
    if(actor.stopped)throw new Error('raft member: activation replaced; recover from durable storage');
    this.config = canonical; this.registry = actor.store.read('membership', 'registry') ? committedRegistry(sharedRuntime(), actor.store) : await this.ctx.storage.get('raft-registry'); this.actor = actor;
    return result;
  }

  async load() {
    if(this.erased || await this.ctx.storage.get('raft-erased'))throw new Error('raft member: identity retired');
    if (this.actor) return;
    if(await this.ctx.storage.get('legacy-pending')||this.importPending)throw new Error('raft member: import incomplete; retry');
    if (this.loading) return this.loading;
    this.loading = this.restore();
    try { return await this.loading; } finally { this.loading = null; }
  }
  async restore() {
    const config = await this.ctx.storage.get('raft-config'); if (!config) throw new Error('raft member: not provisioned');
    const actor = new MemberActor(this.ctx.storage, sharedRuntime(), this.evaluator(config),this.ctx.id.toString());
    this.recoveredEffects = await actor.init(config);if(actor.stopped)throw new Error('raft member: activation replaced; recover from durable storage'); this.config = config; this.registry = actor.store.read('membership', 'registry') ? committedRegistry(sharedRuntime(), actor.store) : await this.ctx.storage.get('raft-registry'); this.actor = actor;
  }
  // Alarm effects wait in one bounded durable transport batch. The router
  // drains original bytes and acknowledges the batch ID; arrival is never an
  // append acknowledgment. A stalled router backpressures further alarm turns.
  async scheduled() { await this.load(); return this.actor.serial(async()=>{
    const manifest=await this.ctx.storage.get('raft-scheduled');if(!manifest)return null;
    const policy=coordinationPolicy(sharedRuntime());
    if(manifest.bytes>INBOX_BYTES||manifest.parts!==Math.ceil(manifest.bytes/policy.max_message_bytes))throw new Error('raft member: damaged scheduled batch');
    const raw=new Uint8Array(manifest.bytes);
    for(let n=0;n<manifest.parts;n++){
      const part=await this.ctx.storage.get('raft-scheduled-part/'+manifest.id+'/'+n);
      if(!part||part.length!==Math.min(policy.max_message_bytes,raw.length-n*policy.max_message_bytes))throw new Error('raft member: incomplete scheduled batch');
      raw.set(part,n*policy.max_message_bytes);
    }
    if(this.actor.store.hash(raw)!==manifest.hash)throw new Error('raft member: damaged scheduled batch');
    return JSON.parse(new TextDecoder().decode(raw));
  }); }
  async acknowledgeScheduled(id) { await this.load(); return this.actor.serial(async()=>{
    await this.ctx.storage.transaction(async tx=>{
      const batch=await tx.get('raft-scheduled');if(batch?.id!==String(id))return;
      for(let n=0;n<batch.parts;n++)await tx.delete('raft-scheduled-part/'+batch.id+'/'+n);
      await tx.delete('raft-scheduled');
    });await this.ctx.storage.sync();
  }); }
  // Chunking preserves valid maximum-sized Raft messages beneath the KV value
  // limit. Voters take the reservation first; lagging learners cannot block it.
  async retainScheduled(effects,policy) {
    const recovered=this.recoveredEffects;
    const sends=[...(recovered?.send??[]),...effects.send];
    if(!sends.length&&!effects.plan.wake?.length)return [];
    const view=this.actor.store.read('membership','registry')?committedRegistry(sharedRuntime(),this.actor.store):provisionedRegistry(sharedRuntime(),this.config);
    sends.sort((a,b)=>Number(!view[String(a.to)]?.voter)-Number(!view[String(b.to)]?.voter));
    const id=String(BigInt(await this.ctx.storage.get('raft-scheduled-sequence')??'0')+1n);
    const batch={id,send:[],wake:effects.plan.wake},dropped=[];
    let size=new TextEncoder().encode(JSON.stringify(batch)).length;
    for(const m of sends){
      const entry={to:String(m.to),bytes:b64(m.bytes)},cost=new TextEncoder().encode(JSON.stringify(entry)).length+1;
      if(size+cost>INBOX_BYTES){dropped.push(m);continue;}
      batch.send.push(entry);size+=cost;
    }
    const raw=new TextEncoder().encode(JSON.stringify(batch));
    const manifest={id,bytes:raw.length,parts:Math.ceil(raw.length/policy.max_message_bytes),hash:this.actor.store.hash(raw)};
    await this.ctx.storage.transaction(async tx=>{
      for(let n=0;n<manifest.parts;n++)await tx.put('raft-scheduled-part/'+id+'/'+n,raw.slice(n*policy.max_message_bytes,(n+1)*policy.max_message_bytes));
      await tx.put({'raft-scheduled':manifest,'raft-scheduled-sequence':id});
    });await this.ctx.storage.sync();this.recoveredEffects=null;return dropped;
  }
  async alarm() {
    await this.load();
    const policy=coordinationPolicy(sharedRuntime()),now=Date.now();
    const dispatch=async effects=>{
      const dropped=await this.retainScheduled(effects,policy);
      for(const m of dropped){const inspected=this.actor.codec.inspect(4,m.bytes);await this.actor.drive(12,{peer:BigInt(m.to),snapshot:inspected.snapshot.length>0,success:false});}
    };
    const ready=async()=>{
      if(await this.ctx.storage.get('raft-scheduled')){await this.ctx.storage.setAlarm(now+policy.idle_after);return false;}
      return true;
    };
    const timer=new MemberAlarms(this.ctx.storage,this.actor,dispatch,async()=>false,ready);
    await timer.run(Math.floor(performance.now()),now);
  }
  async schedule(elapsed, ingressNow, busy, acks=[]) { await this.load(); return this.actor.schedule(elapsed, ingressNow, busy, acks); }
  async sleepAck(index,term) { await this.load(); return this.actor.sleepAck(index,term); }
  async leadershipEvidence(revision,barrier) { await this.load(); return this.actor.leadershipEvidence(revision,barrier); }
  async leadership(evidence) { await this.load(); return this.actor.leadership(evidence); }
  async leadershipReceipt() { await this.load(); return this.actor.leadershipReceipt(); }
  async membership() { await this.load(); return this.actor.membership(); }
  async joinStatus(advertised) { await this.load(); return this.actor.joinStatus(advertised); }
  async drainRecovery() {
    await this.load();
    return this.actor.serial(() => { const effects = this.recoveredEffects; this.recoveredEffects = null; return effects; });
  }
  // Only durable application can change protocol authority. Reuse its exact
  // view within that index, and invalidate it after every committed change.
  committedPeers(){
    const index=this.actor.store.application().applied;
    if(this.registryIndex!==index){this.registryCache=this.actor.store.read('membership','registry')?committedRegistry(sharedRuntime(),this.actor.store):provisionedRegistry(sharedRuntime(),this.config);this.registryIndex=index;}
    return this.registryCache;
  }
  async peer(raw) {
    await this.load();
    const config = this.config;
    if (!this.actor.store.read('membership', 'registry') && !config.anchors) throw new Error('raft member: enrollment is absent');
    this.inbox ??= new MemberInbox(config.scope, config.id, this.actor.codec, () => this.committedPeers(), fn => this.actor.serial(fn));
    return this.inbox.receive(raw, async (envelope, payload) => {
      if (envelope.chunk) {
        if(envelope.chunk.offset==='0'&&envelope.chunk.generation===envelope.chunk.hash)return this.actor.drive(2,payload);
        if (!this.actor.store.one('SELECT id FROM snapshots WHERE id=?', envelope.chunk.generation)) return { staged: false };
        await this.actor.store.putChunk(envelope.chunk.generation, Number(BigInt(envelope.chunk.offset)), payload, envelope.chunk.hash);
        return { staged: true };
      }
      const m = this.actor.codec.inspect(4, payload);
      if (m.snapshot.length) {
        const snapshot = this.actor.codec.inspect(3, m.snapshot);
        if (snapshot.index > this.actor.status.applied) {
          const id = await this.actor.store.beginSnapshot(snapshot.data);
          const manifest = this.actor.store.generation(id).manifest;
          const chunks = this.actor.store.one('SELECT count(*) AS n FROM chunks WHERE generation=?', id).n;
          if (chunks !== Number(manifest.chunks)) return { staged: true, installed: false };
          await this.actor.store.completeSnapshot(id);
        }
        const result = await this.actor.drive(2, payload);
        return { ...result, installed: this.actor.status.applied >= snapshot.index };
      }
      return this.actor.drive(2, payload);
    });
  }
  // Refresh under the dispatch queue; never accept a caller-supplied membership
  // list as evidence that a device joined or a revoked transport key returned.
  async refreshEnrollment() {
    await this.load();
    return this.actor.serial(async () => {
      const view = this.committedPeers();
      if(JSON.stringify(view)!==JSON.stringify(this.registry)){await this.ctx.storage.put('raft-registry',view);await this.ctx.storage.sync();this.registry=view;}
      return view;
    });
  }
  // Existing internal callers may request publication, but cannot supply a
  // different view or invent enrollment before the committed manifest exists.
  async enrollment(view, applied) {
    view = structuredClone(view);
    await this.load();
    return this.actor.serial(async () => {
      if (BigInt(applied) > this.actor.status.applied) throw new Error('raft member: enrollment barrier not applied');
      const committed = committedRegistry(sharedRuntime(), this.actor.store);
      if (JSON.stringify(committed) !== JSON.stringify(view)) throw new Error('raft member: enrollment differs from committed registry');
      await this.ctx.storage.put('raft-registry', committed); await this.ctx.storage.sync(); this.registry = committed;
    });
  }
}
