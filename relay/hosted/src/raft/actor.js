import { CORE_MEMORY_BUDGET, Reactor, HostCodec, createFrame, response, effects, frame, stepFrame } from './abi.js';
import { MemberStorage, DEFAULT_ROWS_BYTES } from './storage.js';

const RESERVATION = 6 * 1024 * 1024;
const CACHE_BYTES = 2 * 1024 * 1024;
const QUEUED_INPUTS = 64;
let module, runtime;
const active = new Set();

// A compiled module and runtime are shared by every object in this isolate.
// Reservations precede allocation; pressure refuses activation, never evicts a
// serving leader. Quiescent eviction is an explicit later scheduler decision.
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
      const r = response(this.reactor.call(createFrame({ ...config, bootstrap: recovered.entries.length || recovered.snapshot.length || recovered.hard.length ? [] : config.bootstrap, ...recovered })));
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
  begin(data) { data = data.slice(); return this.serial(() => this.store.beginSnapshot(data)); }
  chunk(id, offset, data, hash) { data = data.slice(); return this.serial(() => this.store.putChunk(id, offset, data, hash)); }
  complete(id) { return this.serial(() => this.store.completeSnapshot(id)); }
  close() { return this.serial(() => {
    if (this.status?.role === 2 || this.pending > 1) throw new Error('raft member: active member cannot be evicted');
    if (this.handle) { const r = response(this.reactor.call(frame(2).u32(this.handle).finish())); r.end(); }
    this.handle = 0; this.generation++; active.delete(this);
  }); }
}

