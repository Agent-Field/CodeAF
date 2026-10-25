import { b64, unb64, equal } from './storage.js';

const text = new TextEncoder();
const uint = (n, nonzero = true) => typeof n === 'string' && /^(0|[1-9][0-9]*)$/.test(n) && BigInt(n) <= (1n << 64n) - 1n && (!nonzero || BigInt(n) !== 0n);
const token = s => typeof s === 'string' && /^[a-zA-Z0-9_.-]{1,128}$/.test(s);
const hash = s => typeof s === 'string' && /^[a-f0-9]{64}$/.test(s);
const check = v => { if (!v) throw new Error('raft inbox: peer authorization refused'); };
export const INBOX_BYTES = 1024 * 1024;
const budgets = [INBOX_BYTES - 128 * 1024 - 384 * 1024, 128 * 1024, 384 * 1024];
const slots = [4, 2, 2];
const NORMAL_BYTES=64*1024;
const shared = new Map();

// Authentication is repeated at dispatch, after every async signature wait.
// A queued packet never conveys application authority or a disk acknowledgment.
export class MemberInbox {
  constructor(scope, id, codec, registry, enqueue) {
    this.scope = scope; this.id = String(id); this.codec = codec; this.registry = registry; this.enqueue = enqueue;
    this.identity = JSON.stringify(scope); this.counts = [0, 0, 0];
    this.pending = new Map();
  }
  members(e) {
    check(JSON.stringify(e.scope) === JSON.stringify(this.scope) && e.to === this.id && e.from !== e.to && uint(e.from) && uint(e.to) && uint(e.incarnation) && uint(e.destination) && uint(e.sequence));
    const view = this.registry(), from = view[e.from], to = view[e.to];
    check(from && to && !from.revoked && !to.revoked && from.incarnation === e.incarnation && to.incarnation === e.destination);
    return { from, to };
  }
  async receive(raw, dispatch) {
    check(raw instanceof Uint8Array && raw.length <= 512 * 1024);
    const e = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw));
    const { from } = this.members(e);
    check(token(e.scope.identity) && token(e.scope.cluster) && uint(e.scope.epoch) && e.scope.protocol === 1);
    const payload = unb64(e.payload); check(b64(payload) === e.payload && hash(e.digest) && this.codec.hash(payload).digest.every((x, i) => x === parseInt(e.digest.slice(i * 2, i * 2 + 2), 16)));
    const unsigned = { scope: { identity: e.scope.identity, cluster: e.scope.cluster, epoch: e.scope.epoch, protocol: e.scope.protocol }, from: e.from, to: e.to, incarnation: e.incarnation, destination: e.destination, sequence: e.sequence, chunk: e.chunk, digest: e.digest, payload: e.payload, signature: null };
    const canonical = { ...unsigned, signature: e.signature }; check(JSON.stringify(canonical) === new TextDecoder().decode(raw));
    if (e.chunk !== null) {
      check(Object.keys(e.chunk).join(',') === 'generation,offset,hash' && hash(e.chunk.generation) && uint(e.chunk.offset, false) && e.chunk.hash === e.digest && payload.length > 0 && payload.length <= 256 * 1024);
      if(e.chunk.generation===e.chunk.hash&&e.chunk.offset==='0'){const m=this.codec.inspect(4,payload);check(payload.length>NORMAL_BYTES&&m.type===3&&String(m.from)===e.from&&String(m.to)===e.to);}
    } else {
      check(payload.length > 0 && payload.length <= NORMAL_BYTES);
      const m = this.codec.inspect(4, payload); check(String(m.from) === e.from && String(m.to) === e.to);
    }
    const key = await crypto.subtle.importKey('raw', unb64(from.key), 'Ed25519', false, ['verify']);
    check(await crypto.subtle.verify('Ed25519', key, unb64(e.signature), text.encode('codeaf/raft/envelope/1\n' + JSON.stringify(unsigned))));
    this.members(e);
    const lane = e.chunk !== null ? 2 : from.voter ? 0 : 1;
    const id = e.from + '/' + e.sequence;
    const old = this.pending.get(id);
    if (old) { check(equal(old.raw, raw)); return old.promise; }
    if (!shared.has(this.identity)) shared.set(this.identity, { used: [0, 0, 0] });
    const budget = shared.get(this.identity);
    if (this.counts[lane] >= slots[lane] || budget.used[lane] + raw.length > budgets[lane]) throw new Error('raft inbox: capacity; retry with backoff');
    const owned = raw.slice(); this.counts[lane]++; budget.used[lane] += owned.length;
    const promise = this.enqueue(async () => {
      // The durable registry may have changed while another member persisted.
      this.members(e); return dispatch(e, payload);
    });
    this.pending.set(id, { raw: owned, promise });
    try { return await promise; }
    finally { this.pending.delete(id); this.counts[lane]--; budget.used[lane] -= owned.length; if (budget.used.every(n => n === 0)) shared.delete(this.identity); }
  }
}
