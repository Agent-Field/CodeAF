// Local-only RPC controls drive the production actor/store. No production
// request router exposes ticks, bootstrap, proposals or checkpoint controls.
import core from './core.wasm';
import { installRaftModule, runtimeMetrics, MemberActor, sharedRuntime } from '../../src/raft/actor.js';
import { RaftMemberDO } from '../../src/raft/member.js';
import { frame, response } from '../../src/raft/abi.js';
import { MemberInbox } from '../../src/raft/inbox.js';
import { b64, unb64 } from '../../src/raft/storage.js';
export { IdentityDO, NewcomerGate, LinkGate, LinkRequest, PairGate, Mailbox } from '../../src/worker.js';
installRaftModule(core);
const json = v => Response.json(v, { headers: { 'content-type': 'application/json' } });
const encode = v => JSON.stringify(v, (_, x) => typeof x === 'bigint' ? String(x) : x instanceof Uint8Array ? b64(x) : x);
const reply = v => new Response(encode(v ?? null), { headers: { 'content-type': 'application/json' } });
const evaluate = (command, read, app) => {
  const reactor = sharedRuntime(), r = response(reactor.call(frame(4).blob(command).blob(read('state', 'cell') ?? new Uint8Array()).finish()));
  const data = r.blob(); r.end(); return { writes: [{ namespace: 'state', key: 'cell', data }], version: BigInt(app.version) + 1n };
};
export class ActorFixture extends RaftMemberDO {
  evaluator() { return evaluate; }
  async enrollment(view, applied) {
    await this.load();
    return this.actor.serial(async () => {
      if (BigInt(applied) > this.actor.status.applied) throw new Error('raft member: enrollment barrier not applied');
      const old = await this.ctx.storage.get('raft-registry') ?? {};
      for (const [id, before] of Object.entries(old)) {
        const after = view[id];
        if (!after || before.revoked && !after.revoked || BigInt(after.incarnation) < BigInt(before.incarnation) || after.incarnation === before.incarnation && after.key !== before.key) throw new Error('raft member: enrollment regression');
      }
      await this.ctx.storage.put('raft-registry', view); await this.ctx.storage.sync(); this.registry = view;
    });
  }
  async peer(raw) {
    // The mechanism fixture isolates host durability from policy. Its explicit
    // local registry is not a production admission or authority path.
    await this.load();
    this.inbox ??= new MemberInbox(this.config.scope, this.config.id, this.actor.codec, () => this.registry, fn => this.actor.serial(fn));
    return this.inbox.receive(raw, async (envelope, payload) => {
      if (envelope.chunk) {
        if (!this.actor.store.one('SELECT id FROM snapshots WHERE id=?', envelope.chunk.generation)) return { staged: false };
        await this.actor.store.putChunk(envelope.chunk.generation, Number(BigInt(envelope.chunk.offset)), payload, envelope.chunk.hash); return { staged: true };
      }
      const message = this.actor.codec.inspect(4, payload);
      if (message.snapshot.length) {
        const snapshot = this.actor.codec.inspect(3, message.snapshot);
        if (snapshot.index > this.actor.status.applied) {
          const id = await this.actor.store.beginSnapshot(snapshot.data), manifest = this.actor.store.generation(id).manifest;
          if (this.actor.store.one('SELECT count(*) AS n FROM chunks WHERE generation=?', id).n !== Number(manifest.chunks)) return { staged: true, installed: false };
          await this.actor.store.completeSnapshot(id);
        }
        const result = await this.actor.drive(2, payload); return { ...result, installed: this.actor.status.applied >= snapshot.index };
      }
      return this.actor.drive(2, payload);
    });
  }
  async fetch(request) {
    const url = new URL(request.url), b = request.method === 'POST' ? await request.json() : {};
    try {
      if (url.pathname === '/init') return reply(await this.initialize(b));
      await this.load();
      if (url.pathname === '/input') return reply(await this.actor.input(b.tag, [2,3,8,11].includes(b.tag) ? unb64(b.value) : [5,6,7,9].includes(b.tag) ? BigInt(b.value) : b.value));
      if (url.pathname === '/row') return reply(this.actor.store.read('state', 'cell'));
      if (url.pathname === '/application') return reply(this.actor.store.application());
      if (url.pathname === '/checkpoint') return reply(await this.actor.checkpoint());
      if (url.pathname === '/begin') return reply(await this.actor.begin(unb64(b.data)));
      if (url.pathname === '/chunk') return reply(await this.actor.chunk(b.id, b.offset, unb64(b.data), b.hash));
      if (url.pathname === '/complete') return reply(await this.actor.complete(b.id));
      if (url.pathname === '/read-chunk') return reply(this.actor.store.chunk(b.id, b.offset));
      if (url.pathname === '/peer') return reply(await this.peer(unb64(b.data)));
      if (url.pathname === '/enrollment') return reply(await this.enrollment(b.view, b.applied));
      if (url.pathname === '/transport') return reply(await this.actor.serial(() => this.actor.drive(12, b)));
      if (url.pathname === '/corrupt') { this.actor.store.rows('UPDATE chunks SET hash=? WHERE generation=? AND offset=?', 'damaged', b.id, b.offset); await this.ctx.storage.sync(); return reply(true); }
      if (url.pathname === '/storage-fail') { this.actor.store.rows("CREATE TRIGGER fail_write BEFORE INSERT ON journal BEGIN SELECT RAISE(ABORT, 'injected storage failure'); END"); return reply(true); }
      if (url.pathname === '/concurrent') {
        const seen = []; let release;
        const wait = new Promise(resolve => { release = resolve; });
        const first = this.actor.serial(async () => { seen.push('first-start'); await wait; seen.push('first-end'); });
        const second = this.actor.serial(() => { seen.push('second'); });
        await Promise.resolve(); release(); await Promise.all([first, second]); return reply(seen);
      }
      return new Response('fixture path', { status: 404 });
    } catch (e) { return json({ error: e.message }); }
  }
}
export default {
  async fetch(request, env) {
    const u = new URL(request.url);
    if (u.pathname === '/ready') return new Response(null, { status: 204 });
    if (u.pathname === '/metrics') return reply(runtimeMetrics());
    const [, , name, ...parts] = u.pathname.split('/');
    const stub = env.ACTORS.get(env.ACTORS.idFromName(name));
    return stub.fetch(new Request('http://actor/' + parts.join('/'), request));
  },
};
