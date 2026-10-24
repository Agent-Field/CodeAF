import { DurableObject } from 'cloudflare:workers';
import { MemberActor, sharedRuntime } from './actor.js';
import { MemberInbox, INBOX_BYTES } from './inbox.js';
import { directoryEvaluator } from './directory.js';
import { DEFAULT_ROWS_BYTES, MemberStorage, b64 } from './storage.js';
import { HostCodec } from './abi.js';
import { committedRegistry, provisionedRegistry } from './membership.js';
import { MemberAlarms, coordinationPolicy } from './liveness.js';
import { stageLegacyImport, finishLegacyImport } from './legacy.js';
export { installRaftModule, runtimeMetrics } from './actor.js';

// Only internal namespace calls can initialize a provisioned member. The
// public Worker exports this class but does not route directory traffic here.
// Enrollment is derived from the durable Go registry; P09 supplies evaluation.
export class RaftMemberDO extends DurableObject {
  constructor(ctx, env) { super(ctx, env); this.actor = null; this.loading = null; this.ctx = ctx; this.env = env; }
  evaluator(config) { return directoryEvaluator(sharedRuntime(), config.cluster, config.ceiling ?? DEFAULT_ROWS_BYTES); }
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
    this.loading = this.activate(config);
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
    const actor = new MemberActor(this.ctx.storage, sharedRuntime(), this.evaluator(config));
    const result = await actor.init(canonical);
    this.config = canonical; this.registry = actor.store.read('membership', 'registry') ? committedRegistry(sharedRuntime(), actor.store) : await this.ctx.storage.get('raft-registry'); this.actor = actor;
    return result;
  }

  async load() {
    if (this.actor) return;
    if(await this.ctx.storage.get('legacy-pending')||this.importPending)throw new Error('raft member: import incomplete; retry');
    if (this.loading) return this.loading;
    this.loading = this.restore();
    try { return await this.loading; } finally { this.loading = null; }
  }
  async restore() {
    const config = await this.ctx.storage.get('raft-config'); if (!config) throw new Error('raft member: not provisioned');
    const actor = new MemberActor(this.ctx.storage, sharedRuntime(), this.evaluator(config));
    this.recoveredEffects = await actor.init(config); this.config = config; this.registry = actor.store.read('membership', 'registry') ? committedRegistry(sharedRuntime(), actor.store) : await this.ctx.storage.get('raft-registry'); this.actor = actor;
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
  async peer(raw) {
    await this.load();
    const config = this.config;
    if (!this.actor.store.read('membership', 'registry') && !config.anchors) throw new Error('raft member: enrollment is absent');
    this.inbox ??= new MemberInbox(config.scope, config.id, this.actor.codec, () => this.actor.store.read('membership', 'registry') ? committedRegistry(sharedRuntime(), this.actor.store) : provisionedRegistry(sharedRuntime(), config), fn => this.actor.serial(fn));
    return this.inbox.receive(raw, async (envelope, payload) => {
      if (envelope.chunk) {
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
      const view = committedRegistry(sharedRuntime(), this.actor.store);
      await this.ctx.storage.put('raft-registry', view); await this.ctx.storage.sync(); this.registry = view;
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
