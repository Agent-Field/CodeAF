import { CORE_MEMORY_BUDGET, Reactor, HostCodec, createFrame, response, effects, frame, stepFrame } from './abi.js';
import { MemberStorage, DEFAULT_ROWS_BYTES, unb64 } from './storage.js';
import { decodeCanonical, encodeCanonical } from './codec.js';
import { committedRegistry } from './membership.js';

const RESERVATION = 6 * 1024 * 1024;
const CACHE_BYTES = 2 * 1024 * 1024;
const QUEUED_INPUTS = 64;
let module, runtime;
const active = new Set();

// A compiled module and runtime are shared by every object in this isolate.
// Reservations precede allocation; pressure refuses activation, never evicts a
// serving leader. Quiescent eviction requires an explicit scheduler decision.
export function installRaftModule(compiled) {
  if (module && module !== compiled) throw new Error('raft member: core module already installed');
  module = compiled;
}
export function sharedRuntime() { if (!module) throw new Error('raft member: core unavailable'); return runtime ??= new Reactor(module); }
export function runtimeMetrics() { return { ...sharedRuntime().metrics(), active: active.size, reservedBytes: active.size * RESERVATION }; }

// A member queue spans awaits and remains bounded even when storage stalls.
// Rejected inputs are retryable; a failed persistence stops this handle.
export class MemberActor {
  constructor(storage, reactor, evaluate) {
    this.reactor = reactor; this.codec = new HostCodec(reactor); this.store = new MemberStorage(storage, this.codec);
    this.evaluate = evaluate; this.tail = Promise.resolve(); this.pending = 0; this.handle = 0; this.stopped = false; this.generation = 0; this.checkpointIndex = 0n;
  }
  serial(fn) {
    if (this.pending >= QUEUED_INPUTS) return Promise.reject(new Error('raft member: capacity; retry with backoff'));
    this.pending++;
    const p = this.tail.then(fn); this.tail = p.catch(() => {});
    return p.finally(() => { this.pending--; });
  }
  init(config) { config = structuredClone(config); return this.serial(async () => {
    if (this.handle) throw new Error('raft member: already active');
    if (active.size * RESERVATION + RESERVATION + this.reactor.memory > CORE_MEMORY_BUDGET) throw new Error('raft member: isolate capacity; retry with backoff');
    active.add(this);
    try {
      const recovered = await this.store.open({ cluster: config.cluster, id: String(config.id), protocol: 1 }, config.ceiling ?? DEFAULT_ROWS_BYTES);
      const cache = recovered.entries.reduce((n, b) => n + b.length, 0);
      if (cache > CACHE_BYTES) throw new Error('raft member: log cache capacity; recover with checkpoint');
      const registry = this.store.read('membership', 'registry');
      const imported = this.store.read('membership', 'legacy-import');
      const provisioned = (config.anchors ?? config.bootstrap ?? []).map(a => String(typeof a === 'object' ? a.node : a));
      const anchors = registry ? decodeCanonical(new TextDecoder().decode(registry)).anchors : imported ? decodeCanonical(new TextDecoder().decode(imported)).config.anchors : provisioned;
      if (provisioned.length && anchors.join(',') !== provisioned.join(',')) throw new Error('raft member: conflicting anchor manifest');
      const r = response(this.reactor.call(createFrame({ ...config, anchors, bootstrap: recovered.entries.length || recovered.snapshot.length || recovered.hard.length ? [] : config.bootstrap, ...recovered })));
      this.handle = r.u32(); r.end(); this.config = config; this.generation++; this.cache = cache;
      return this.drive(0);
    } catch (e) {
      this.stopped = true;
      if (this.handle) { try { this.reactor.call(frame(2).u32(this.handle).finish()); } catch {} this.handle = 0; }
      active.delete(this); throw e;
    }
  }); }
  async drive(tag, value) {
    if (this.stopped || this.store.stopped || !this.handle) throw new Error('raft member: stopped; recover from durable storage');
    const sends = [], reads = []; let e;
    try { e = effects(this.reactor.call(stepFrame(this.handle, tag, value))); }
    catch (error) { if (this.reactor.poisoned) this.stopped = true; throw error; }
    for (let budget = 0; ; budget++) {
      if (budget > 256) { this.stopped = true; throw new Error('raft member: completion budget'); }
      this.status = e.status;
      sends.push(...e.send); reads.push(...e.reads);
      try {
        if (e.persist) {
          await this.store.persist(e.persist);
          this.cache = this.store.one('SELECT coalesce(sum(length(data)),0) AS n FROM journal').n;
          e = effects(this.reactor.call(stepFrame(this.handle, 5, e.persist.id))); continue;
        }
        if (e.apply) { await this.store.apply(e.apply, this.evaluate); e = effects(this.reactor.call(stepFrame(this.handle, 6, e.apply.id))); continue; }
      } catch (error) {
        this.stopped = true;
        try { this.reactor.call(stepFrame(this.handle, 9, e.persist?.id ?? e.apply?.id ?? 0n)); } catch {}
        throw error;
      }
      // Checkpoint sooner than the retained-log ceiling so the active core's
      // cache stays bounded. This compacts only durable applied history.
      const app = this.store.application();
      if ((this.cache >= CACHE_BYTES / 2 || this.status.applied - this.checkpointIndex >= 1024n) && BigInt(app.applied) > 0n && BigInt(app.applied) === this.status.commit) {
        let snapshot;
        try { snapshot = await this.store.checkpoint(); e = effects(this.reactor.call(stepFrame(this.handle, 11, snapshot))); }
        catch (error) { this.stopped = true; throw error; }
        this.checkpointIndex = BigInt(app.applied); this.cache = this.store.one('SELECT coalesce(sum(length(data)),0) AS n FROM journal').n; continue;
      }
      if (this.cache > CACHE_BYTES) { this.stopped = true; throw new Error('raft member: log cache capacity; recover'); }
      return { send: sends, reads, status: this.status };
    }
  }
  input(tag, value) { value = value instanceof Uint8Array ? value.slice() : value && typeof value === 'object' ? structuredClone(value) : value; return this.serial(() => this.drive(tag, value)); }
  checkpoint() { return this.serial(async () => {
    try { const snapshot = await this.store.checkpoint(); await this.drive(11, snapshot); this.checkpointIndex = this.status.applied; return snapshot; }
    catch (error) { this.stopped = true; throw error; }
  }); }
  // Reconcile only the committed intent. Core planning prevents a second
  // configuration while the first is uncommitted and refreshes snapshots that
  // would otherwise exclude the newly admitted learner.
  membership() { return this.serial(async () => {
    const header = this.store.read('membership', 'registry');
    if (!header) throw new Error('raft member: enrollment is absent');
    let r = response(this.reactor.call(frame(17).u32(this.handle).blob(header).finish()));
    const change = r.blob(), checkpoint = r.u32() !== 0, settle = r.u32() !== 0; r.end();
    if (change.length) return { ...(await this.drive(8, change)), settle: false };
    if (checkpoint) {
      const snapshot = await this.store.checkpoint(); await this.drive(11, snapshot); this.checkpointIndex = this.status.applied;
    }
    return { send: [], reads: [], status: this.status, settle };
  }); }
  // Evidence is an internal authenticated target reply, derived under the
  // durable member queue. No public request may set its own applied index.
  leadershipEvidence(revision, barrier) { return this.serial(() => {
    if(this.stopped||this.store.stopped||!this.handle)throw new Error('raft member: stopped; recover from durable storage');
    const header = this.store.read('membership', 'registry');
    if (!header) throw new Error('raft member: enrollment is absent');
    const h = JSON.parse(new TextDecoder().decode(header)), app = this.store.application();
    if (h.revision !== String(revision) || !h.leadership || h.leadership.barrier !== String(barrier) || BigInt(app.applied) < BigInt(barrier) || committedRegistry(this.reactor,this.store)[this.config.id]?.revoked !== false) throw new Error('raft member: leadership barrier not applied');
    return {node:this.config.id,revision:String(revision),applied:String(app.applied),protocol:this.store.identity.protocol,durable:true};
  }); }
  leadership(evidence) { evidence = structuredClone(evidence); return this.serial(async () => {
    const header = this.store.read('membership', 'registry');
    if (!header) throw new Error('raft member: enrollment is absent');
    if(BigInt(evidence.node)!==0n){const target=committedRegistry(this.reactor,this.store)[String(evidence.node)];if(!target||target.revoked)throw new Error('raft member: leadership target is unavailable');}
    const raw = new TextEncoder().encode(encodeCanonical(evidence));
    const r = response(this.reactor.call(frame(21).u32(this.handle).blob(header).blob(raw).finish()));
    const plan = JSON.parse(new TextDecoder().decode(r.blob())); r.end();
    if (plan.configure) return {...(await this.drive(8,unb64(plan.change ?? ''))),plan};
    if (BigInt(plan.transfer)) return {...(await this.drive(7,BigInt(plan.transfer))),plan};
    return {send:[],reads:[],status:this.status,plan};
  }); }
  leadershipReceipt() { return this.serial(() => {
    const header = this.store.read('membership', 'registry');
    if (!header) throw new Error('raft member: enrollment is absent');
    const r = response(this.reactor.call(frame(22).u32(this.handle).blob(header).finish()));
    const receipt = JSON.parse(new TextDecoder().decode(r.blob()));r.end();return receipt;
  }); }
  joinStatus(advertised) { return this.serial(() => {
    const raw = this.store.read('enrollment', 'node/' + this.config.id);
    if (!raw || committedRegistry(this.reactor, this.store)[this.config.id]?.revoked) return { ready: false };
    const app = this.store.application();
    const r = response(this.reactor.call(frame(18).blob(raw).blob(unb64(app.conf)).u64(app.applied).u64(advertised).finish()));
    const ready = r.u32() !== 0; r.end(); return { ready };
  }); }
  // Timer decisions use the shared Go scheduler and paged Go activity rules.
  // An internal caller supplies queued assignment/outbox work conservatively.
  schedule(now, ingressNow, busy, acks = []) { acks = structuredClone(acks); return this.serial(() => this.scheduleInQueue(now,ingressNow,busy,acks)); }
  async scheduleInQueue(now, ingressNow, busy, acks = []) {
    busy = busy || this.pending > 1;
    for (const row of this.store.rows("SELECT namespace,key FROM records WHERE namespace='state' OR (namespace='membership' AND key='registry') OR (namespace='outbox' AND key LIKE 'join/%') ORDER BY namespace,key")) {
      const raw = this.store.read(row.namespace,row.key);
      const r = response(this.reactor.call(frame(24).str(row.namespace).str(row.key).blob(raw).u64(ingressNow).finish()));
      busy = (r.u32() !== 0) || busy; r.end();
    }
    const raw = this.store.read('membership','quiescence');
    const marker = raw ? decodeCanonical(new TextDecoder().decode(raw)) : {index:'0',term:'0',barrier:'0',voters:null};
    const request = new TextEncoder().encode(encodeCanonical({now,busy,marker,acks}));
    const r = response(this.reactor.call(frame(23).u32(this.handle).blob(request).finish()));
    const plan = decodeCanonical(new TextDecoder().decode(r.blob())); r.end();
    const effects = plan.tick ? await this.drive(1) : {send:[],reads:[],status:this.status};
    return { ...effects, plan };
  }
  // A final acknowledgment is local durable application, not transport receipt.
  sleepAck(index, term) { return this.serial(() => {
    const raw = this.store.read('membership','quiescence');
    const m = raw && decodeCanonical(new TextDecoder().decode(raw));
    if (!m || m.index !== String(index) || m.term !== String(term) || this.status.applied !== BigInt(index) || this.status.term !== BigInt(term)) throw new Error('raft member: sleep barrier not applied');
    return {node:this.config.id,index:String(index),term:String(term)};
  }); }
  begin(data) { data = data.slice(); return this.serial(() => this.store.beginSnapshot(data)); }
  chunk(id, offset, data, hash) { data = data.slice(); return this.serial(() => this.store.putChunk(id, offset, data, hash)); }
  complete(id) { return this.serial(() => this.store.completeSnapshot(id)); }
  // Explicit owner erasure has already durably fenced every ingress. Dropping
  // this handle reclaims memory; it is never a pressure eviction of a leader.
  disposeErased() { return this.serial(() => {
    this.stopped=true;
    if(this.handle){const r=response(this.reactor.call(frame(2).u32(this.handle).finish()));r.end();}
    this.handle=0;active.delete(this);
  }); }
  close() { return this.serial(() => {
    if (this.status?.role === 2 || this.pending > 1) throw new Error('raft member: active member cannot be evicted');
    if (this.handle) { const r = response(this.reactor.call(frame(2).u32(this.handle).finish())); r.end(); }
    this.handle = 0; this.generation++; active.delete(this);
  }); }
}

