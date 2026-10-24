import { DurableObject } from 'cloudflare:workers';
import { MemberActor, sharedRuntime } from './actor.js';
import { MemberInbox } from './inbox.js';
import { directoryEvaluator } from './directory.js';
import { DEFAULT_ROWS_BYTES } from './storage.js';
export { installRaftModule, runtimeMetrics } from './actor.js';

// Only internal namespace calls can initialize a provisioned member. The
// public Worker exports this class but does not route directory traffic here.
// P12 supplies the durable committed enrollment view and P09 the evaluator.
export class RaftMemberDO extends DurableObject {
  constructor(ctx, env) { super(ctx, env); this.actor = null; this.loading = null; this.ctx = ctx; this.env = env; }
  evaluator(config) { return directoryEvaluator(sharedRuntime(), config.cluster, config.ceiling ?? DEFAULT_ROWS_BYTES); }
  async initialize(config) {
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
    this.config = canonical; this.registry = await this.ctx.storage.get('raft-registry'); this.actor = actor;
    return result;
  }

  async load() {
    if (this.actor) return;
    if (this.loading) return this.loading;
    this.loading = this.restore();
    try { return await this.loading; } finally { this.loading = null; }
  }
  async restore() {
    const config = await this.ctx.storage.get('raft-config'); if (!config) throw new Error('raft member: not provisioned');
    const actor = new MemberActor(this.ctx.storage, sharedRuntime(), this.evaluator(config));
    this.recoveredEffects = await actor.init(config); this.config = config; this.registry = await this.ctx.storage.get('raft-registry'); this.actor = actor;
  }
  async drainRecovery() {
    await this.load();
    return this.actor.serial(() => { const effects = this.recoveredEffects; this.recoveredEffects = null; return effects; });
  }
  async peer(raw) {
    await this.load();
    const config = this.config;
    if (!this.registry) throw new Error('raft member: enrollment is absent');
    this.inbox ??= new MemberInbox(config.scope, config.id, this.actor.codec, () => this.registry, fn => this.actor.serial(fn));
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
  // A registry update is trusted assembly input derived from committed state.
  // Serialize it with dispatch and persist before acknowledging revocation.
  async enrollment(view, applied) {
    view = structuredClone(view);
    await this.load();
    return this.actor.serial(async () => {
      if (BigInt(applied) > this.actor.status.applied) throw new Error('raft member: enrollment barrier not applied');
      const old = await this.ctx.storage.get('raft-registry') ?? {};
      for (const [id, member] of Object.entries(old)) {
        const next = view[id];
        if (!next || (member.revoked && !next.revoked) || BigInt(next.incarnation) < BigInt(member.incarnation) || (next.incarnation === member.incarnation && next.key !== member.key)) throw new Error('raft member: enrollment regression');
      }
      await this.ctx.storage.put('raft-registry', view); await this.ctx.storage.sync(); this.registry = view;
    });
  }
}
