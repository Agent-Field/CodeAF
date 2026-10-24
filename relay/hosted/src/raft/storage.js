import { Writer } from './abi.js';

export const CHUNK_BYTES = 256 * 1024;
export const ROW_BYTES = 1024 * 1024;
export const DEFAULT_ROWS_BYTES = 8 * ROW_BYTES;
const EMPTY = new Uint8Array();
const enc = new TextEncoder(), dec = new TextDecoder('utf-8', { fatal: true });
export const hex = b => Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
export const equal = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
export const bytes = b => b instanceof Uint8Array ? b : new Uint8Array(b);
export const b64 = b => { let s = ''; for (const x of b) s += String.fromCharCode(x); return btoa(s); };
export const unb64 = s => Uint8Array.from(atob(s), c => c.charCodeAt(0));
const namespaces = ['state', 'sessions', 'membership', 'enrollment', 'tombstones', 'outbox'];
const key = n => BigInt(n).toString(16).padStart(16, '0');
const check = (v, message = 'raft storage: invalid or damaged durable state') => { if (!v) throw new Error(message); };
const xor = (a, b) => { for (let i = 0; i < 32; i++) a[i] ^= b[i]; };
const compare = (a, b) => { const x = enc.encode(a), y = enc.encode(b); for (let i = 0; i < Math.min(x.length, y.length); i++) if (x[i] !== y[i]) return x[i] - y[i]; return x.length - y.length; };
const rowSize = (ns, name, data) => 12 + enc.encode(ns).length + enc.encode(name).length + data.length;
const scalar = v => enc.encode(JSON.stringify(v));

// SQL transactions are synchronous so no other event can enter the immutable
// read-set. sync() is awaited before a persistence completion or reply escapes.
export class MemberStorage {
  constructor(storage, codec) { this.storage = storage; this.sql = storage.sql; this.codec = codec; this.stopped = false; }
  rows(query, ...bindings) { return this.sql.exec(query, ...bindings); }
  one(query, ...bindings) { return this.rows(query, ...bindings).toArray()[0]; }
  hash(b) { return hex(this.codec.hash(b).digest); }
  checksum(...parts) {
    let state = EMPTY, digest;
    for (const part of parts) {
      const b = typeof part === 'string' ? enc.encode(part) : bytes(part);
      const length = new Uint8Array(8); new DataView(length.buffer).setBigUint64(0, BigInt(b.length));
      state = this.codec.hash(length, state).state;
      ({ state, digest } = this.codec.hash(b, state));
    }
    return digest;
  }
  put(name, value) { const data = scalar(value); this.rows('INSERT OR REPLACE INTO meta VALUES (?,?,?)', name, data, this.hash(data)); }
  get(name) { const v = this.one('SELECT data,hash FROM meta WHERE name=?', name); check(v); const b = bytes(v.data); check(this.hash(b) === v.hash); return JSON.parse(dec.decode(b)); }
  async transaction(fn) {
    check(!this.stopped, 'raft storage: member stopped; recover');
    try { const result = this.storage.transactionSync(fn); await this.storage.sync(); return result; }
    catch (e) { this.stopped = true; throw e; }
  }
  async open(identity, ceiling = DEFAULT_ROWS_BYTES) {
    const exists = this.one("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'");
    if (!exists) {
      check(this.one("SELECT count(*) AS n FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND name NOT LIKE '_cf_%'").n === 0);
      await this.transaction(() => {
        this.rows('CREATE TABLE meta (name TEXT PRIMARY KEY, data BLOB NOT NULL, hash TEXT NOT NULL)');
        this.rows('CREATE TABLE journal (idx TEXT PRIMARY KEY, data BLOB NOT NULL, hash TEXT NOT NULL)');
        this.rows('CREATE TABLE records (namespace TEXT NOT NULL, key TEXT NOT NULL, data BLOB NOT NULL, hash TEXT NOT NULL, PRIMARY KEY(namespace,key))');
        this.rows('CREATE TABLE snapshots (id TEXT PRIMARY KEY, data BLOB NOT NULL, hash TEXT NOT NULL, complete INTEGER NOT NULL)');
        this.rows('CREATE TABLE chunks (generation TEXT NOT NULL, offset INTEGER NOT NULL, data BLOB NOT NULL, hash TEXT NOT NULL, PRIMARY KEY(generation,offset))');
        this.put('identity', { format: 1, ...identity, ceiling });
        this.put('hard', ''); this.put('snapshot', ''); this.put('tip', '0');
        this.put('application', { applied: '0', conf: '', version: '0', now: '0', count: 0, size: 0, hash: '00'.repeat(32) });
      });
    }
    this.identity = this.get('identity');
    check(this.identity.format === 1 && this.identity.cluster === identity.cluster && this.identity.id === identity.id && this.identity.protocol === 1 && this.identity.ceiling >= DEFAULT_ROWS_BYTES);
    check(this.one('SELECT count(*) AS n FROM meta').n === 5);
    return this.recover();
  }
  application() { return this.get('application'); }
  read(namespace, name) {
    const r = this.one('SELECT data,hash FROM records WHERE namespace=? AND key=?', namespace, name);
    if (!r) return null;
    const b = bytes(r.data); check(hex(this.checksum('records', namespace, name, b)) === r.hash); return b;
  }
  *recordRows() {
    for (const r of this.rows('SELECT namespace,key,data,hash FROM records ORDER BY namespace,key')) {
      const data = bytes(r.data); check(hex(this.checksum('records', r.namespace, r.key, data)) === r.hash);
      yield { namespace: r.namespace, key: r.key, data };
    }
  }
  recover() {
    const app = this.application(), entries = [], aggregate = new Uint8Array(32); let count = 0, size = 0;
    for (const r of this.recordRows()) { xor(aggregate, this.checksum('records', r.namespace, r.key, r.data)); count++; size += rowSize(r.namespace, r.key, r.data); }
    check(count === app.count && size === app.size && hex(aggregate) === app.hash);
    const snapshot = unb64(this.get('snapshot'));
    let previous = snapshot.length ? this.codec.inspect(3, snapshot).index : 0n;
    if (snapshot.length) { const s = this.codec.inspect(3, snapshot); this.walk(this.hash(s.data)); check(BigInt(app.applied) >= s.index); }
    for (const r of this.rows('SELECT idx,data,hash FROM journal ORDER BY idx')) {
      const b = bytes(r.data), e = this.codec.inspect(1, b); check(this.hash(b) === r.hash && key(e.index) === r.idx && e.index === previous + 1n); previous = e.index; entries.push(b);
    }
    const hard = unb64(this.get('hard')), h = hard.length ? this.codec.inspect(2, hard) : { commit: 0n };
    check(previous === BigInt(this.get('tip')) && h.commit <= previous && BigInt(app.applied) <= h.commit);
    // Every retained complete generation is evidence. Corruption never falls
    // back silently to an older apparently usable pointer.
    for (const r of this.rows('SELECT id FROM snapshots WHERE complete=1')) this.walk(r.id);
    return { hard, conf: unb64(app.conf), snapshot, entries, applied: BigInt(app.applied) };
  }
  async persist(p) {
    await this.transaction(() => {
      let app = this.application(); const oldBytes = unb64(this.get('hard'));
      const old = oldBytes.length ? this.codec.inspect(2, oldBytes) : { term: 0n, vote: 0n, commit: 0n };
      const h = p.hard.length ? this.codec.inspect(2, p.hard) : old;
      check(h.term >= old.term && h.commit >= old.commit && !(h.term === old.term && old.vote !== 0n && h.vote !== old.vote));
      if (p.snapshot.length) {
        const s = this.codec.inspect(3, p.snapshot), m = JSON.parse(dec.decode(s.data));
        check(m.cluster === this.identity.cluster && m.protocol === '1' && s.index > BigInt(app.applied));
        const g = this.generation(this.hash(s.data)); check(g.complete === 1 && equal(g.data, s.data));
        this.rows('DELETE FROM records');
        this.walk(g.id, r => this.rows('INSERT INTO records VALUES (?,?,?,?)', r.namespace, r.key, r.data, hex(this.checksum('records', r.namespace, r.key, r.data))));
        check(BigInt(m.version) >= BigInt(app.version) && BigInt(m.now) >= BigInt(app.now));
        app = { applied: m.index, conf: b64(s.conf), version: m.version, now: m.now, count: Number(m.records), size: Number(m.bytes), hash: m.records_hash };
        this.put('application', app); this.put('snapshot', b64(p.snapshot)); this.rows('DELETE FROM journal'); this.put('tip', m.index);
      }
      let tip = BigInt(this.get('tip'));
      if (p.entries.length) {
        const first = this.codec.inspect(1, p.entries[0]).index; check(first <= tip + 1n);
        for (const b of p.entries) {
          const e = this.codec.inspect(1, b);
          if (e.index <= old.commit) { const r = this.one('SELECT data FROM journal WHERE idx=?', key(e.index)); check(r && equal(bytes(r.data), b)); }
        }
        this.rows('DELETE FROM journal WHERE idx>=?', key(first)); tip = first - 1n;
        for (const b of p.entries) { const e = this.codec.inspect(1, b); check(e.index === tip + 1n && e.term <= h.term); this.rows('INSERT INTO journal VALUES (?,?,?)', key(e.index), b, this.hash(b)); tip = e.index; }
        this.put('tip', String(tip));
      }
      check(h.commit <= tip && BigInt(app.applied) <= h.commit);
      if (p.hard.length) this.put('hard', b64(p.hard));
    });
  }
  async apply(a, evaluate) {
    await this.transaction(() => {
      const app = this.application(), original = BigInt(app.applied); let index = original;
      const aggregate = Uint8Array.from(app.hash.match(/../g), h => parseInt(h, 16));
      for (const b of a.entries) {
        const e = this.codec.inspect(1, b); check(e.index === index + 1n);
        const persisted = this.one('SELECT data FROM journal WHERE idx=?', key(e.index)); check(persisted && equal(bytes(persisted.data), b));
        if (e.type === 0 && e.data.length) {
          check(evaluate, 'raft storage: application evaluator is absent');
          let readBytes = 0, writeBytes = 0;
          const change = evaluate(e.data, (ns, k) => { const data = this.read(ns, k); readBytes += data?.length ?? 0; check(readBytes <= ROW_BYTES, 'raft storage: evaluation read-set capacity'); return data; }, { ...app });
          check(change && !change.then && Array.isArray(change.writes));
          for (const r of change.writes) {
            check(namespaces.includes(r.namespace) && typeof r.key === 'string' && enc.encode(r.key).length > 0 && enc.encode(r.key).length <= ROW_BYTES && r.data instanceof Uint8Array && r.data.length <= ROW_BYTES);
            writeBytes += rowSize(r.namespace, r.key, r.data); check(writeBytes <= ROW_BYTES, 'raft storage: evaluation write-set capacity');
            const before = this.read(r.namespace, r.key);
            if (before) { xor(aggregate, this.checksum('records', r.namespace, r.key, before)); app.size -= rowSize(r.namespace, r.key, before); } else app.count++;
            xor(aggregate, this.checksum('records', r.namespace, r.key, r.data)); app.size += rowSize(r.namespace, r.key, r.data);
            this.rows('INSERT OR REPLACE INTO records VALUES (?,?,?,?)', r.namespace, r.key, r.data, hex(this.checksum('records', r.namespace, r.key, r.data)));
          }
          check(app.size <= this.identity.ceiling || app.size <= this.application().size, 'raft storage: application capacity; retry after capacity is raised');
          if (change.version !== undefined) { check(BigInt(change.version) >= BigInt(app.version)); app.version = String(change.version); }
          if (change.now !== undefined) { check(BigInt(change.now) >= BigInt(app.now)); app.now = String(change.now); }
        }
        index = e.index;
      }
      const hard = this.codec.inspect(2, unb64(this.get('hard')));
      check(a.index === index && index <= hard.commit && (index >= original));
      app.applied = String(index); app.conf = b64(a.conf); app.hash = hex(aggregate); this.put('application', app);
    });
  }
  generation(id) {
    const r = this.one('SELECT data,hash,complete FROM snapshots WHERE id=?', id); check(r);
    const data = bytes(r.data); check(this.hash(data) === id && this.hash(data) === r.hash);
    this.codec.snapshot(data); check(r.complete === 0 || r.complete === 1); return { ...r, id, data, manifest: JSON.parse(dec.decode(data)) };
  }
  async beginSnapshot(data) {
    const p = this.codec.snapshot(data), s = this.codec.inspect(3, p), m = JSON.parse(dec.decode(data)), id = this.hash(data);
    check(m.cluster === this.identity.cluster && m.protocol === '1' && BigInt(m.bytes) <= BigInt(this.identity.ceiling), 'raft storage: snapshot capacity or identity mismatch');
    await this.transaction(() => {
      const active = unb64(this.get('snapshot')), at = active.length ? this.codec.inspect(3, active).index : 0n;
      check(s.index >= at);
      for (const r of this.rows('SELECT id FROM snapshots WHERE complete=1')) { const g = this.generation(r.id); check(BigInt(g.manifest.index) <= at || r.id === id); }
      if (this.one('SELECT id FROM snapshots WHERE id=?', id)) { this.generation(id); return; }
      this.rows('DELETE FROM chunks WHERE generation IN (SELECT id FROM snapshots WHERE complete=0)'); this.rows('DELETE FROM snapshots WHERE complete=0');
      this.rows('INSERT INTO snapshots VALUES (?,?,?,0)', id, data, id);
    }); return id;
  }
  async putChunk(id, offset, data, digest) {
    check(Number.isSafeInteger(offset) && offset >= 0 && data.length > 0 && data.length <= CHUNK_BYTES && this.hash(data) === digest);
    await this.transaction(() => {
      const g = this.generation(id), m = g.manifest;
      check(BigInt(offset) < BigInt(m.chunks) && data.length === Math.min(CHUNK_BYTES, Number(BigInt(m.bytes) - BigInt(offset) * BigInt(CHUNK_BYTES))));
      const old = this.one('SELECT data FROM chunks WHERE generation=? AND offset=?', id, offset);
      if (old) { check(equal(bytes(old.data), data)); return; }
      check(!g.complete); this.rows('INSERT INTO chunks VALUES (?,?,?,?)', id, offset, data, digest);
    });
  }
  chunk(id, offset) { const r = this.one('SELECT data,hash FROM chunks WHERE generation=? AND offset=?', id, offset); check(r); const b = bytes(r.data); check(this.hash(b) === r.hash); return b; }
  walk(id, visit) {
    const { manifest: m } = this.generation(id);
    let state = EMPTY, digest = this.codec.hash(EMPTY).digest, pending = EMPTY, count = 0, total = 0, size = 0, lastNS = '', lastKey = '';
    const aggregate = new Uint8Array(32);
    const chunks = Number(m.chunks); check(Number.isSafeInteger(chunks) && chunks >= 0);
    check(this.one('SELECT count(*) AS n FROM chunks WHERE generation=?', id).n === chunks);
    for (let i = 0; i < chunks; i++) {
      const block = this.chunk(id, i); check(block.length === Math.min(CHUNK_BYTES, Number(m.bytes) - total)); total += block.length;
      ({ state, digest } = this.codec.hash(block, state));
      const combined = new Uint8Array(pending.length + block.length); combined.set(pending); combined.set(block, pending.length); pending = combined;
      while (pending.length >= 12) {
        const v = new DataView(pending.buffer, pending.byteOffset, pending.byteLength), n = v.getUint32(0), k = v.getUint32(4), d = v.getUint32(8), length = 12 + n + k + d;
        check(n <= 16 && k > 0 && k <= ROW_BYTES && d <= ROW_BYTES);
        if (pending.length < length) break;
        const namespace = dec.decode(pending.subarray(12, 12 + n)), name = dec.decode(pending.subarray(12 + n, 12 + n + k));
        check(namespaces.includes(namespace) && (count === 0 || compare(namespace, lastNS) > 0 || (namespace === lastNS && compare(name, lastKey) > 0)));
        const data = pending.slice(12 + n + k, length); xor(aggregate, this.checksum('records', namespace, name, data)); count++; size += rowSize(namespace, name, data);
        visit?.({ namespace, key: name, data }); lastNS = namespace; lastKey = name; pending = pending.slice(length);
      }
      check(pending.length <= 2 * ROW_BYTES + 28);
    }
    check(pending.length === 0 && String(count) === m.records && String(total) === m.bytes && hex(digest) === m.digest && hex(aggregate) === m.records_hash);
    check(size <= this.identity.ceiling, 'raft storage: snapshot exceeds retained identity capacity');
  }
  retain(id) {
    const others = this.rows('SELECT id FROM snapshots WHERE complete=1 AND id<>?', id).toArray().map(r => this.generation(r.id)).sort((a, b) => BigInt(a.manifest.index) > BigInt(b.manifest.index) ? -1 : 1);
    for (const g of others.slice(1)) { this.rows('DELETE FROM chunks WHERE generation=?', g.id); this.rows('DELETE FROM snapshots WHERE id=?', g.id); }
  }
  async completeSnapshot(id) {
    return this.transaction(() => { this.walk(id); this.rows('UPDATE snapshots SET complete=1 WHERE id=?', id); this.retain(id); return this.codec.snapshot(this.generation(id).data); });
  }
  async checkpoint() {
    return this.transaction(() => {
      const app = this.application(), active = unb64(this.get('snapshot'));
      if (active.length && this.codec.inspect(3, active).index === BigInt(app.applied)) { this.walk(this.hash(this.codec.inspect(3, active).data)); return active; }
      const entry = this.one('SELECT data FROM journal WHERE idx=?', key(app.applied)); check(entry);
      const term = this.codec.inspect(1, bytes(entry.data)).term;
      this.rows('DELETE FROM chunks WHERE generation=?', 'creating');
      let buffer = new Uint8Array(CHUNK_BYTES), used = 0, offset = 0, total = 0, state = EMPTY, digest = this.codec.hash(EMPTY).digest;
      const flush = () => { const b = buffer.slice(0, used); this.rows('INSERT INTO chunks VALUES (?,?,?,?)', 'creating', offset++, b, this.hash(b)); used = 0; };
      for (const r of this.recordRows()) {
        const ns = enc.encode(r.namespace), name = enc.encode(r.key), header = new Uint8Array(12), v = new DataView(header.buffer);
        v.setUint32(0, ns.length); v.setUint32(4, name.length); v.setUint32(8, r.data.length);
        for (const part of [header, ns, name, r.data]) {
          ({ state, digest } = this.codec.hash(part, state)); total += part.length;
          for (let at = 0; at < part.length;) { const n = Math.min(CHUNK_BYTES - used, part.length - at); buffer.set(part.subarray(at, at + n), used); used += n; at += n; if (used === CHUNK_BYTES) flush(); }
        }
      }
      if (used) flush();
      const data = scalar({ format: '1', cluster: this.identity.cluster, protocol: '1', index: app.applied, term: String(term), conf: app.conf || null, version: app.version, now: app.now, records: String(app.count), records_hash: app.hash, bytes: String(total), chunks: String(offset), digest: hex(digest) });
      const snapshot = this.codec.snapshot(data), id = this.hash(data);
      const existing = this.one('SELECT id FROM snapshots WHERE id=?', id);
      if (existing) { this.walk(id); this.rows('DELETE FROM chunks WHERE generation=?', 'creating'); }
      else { this.rows('INSERT INTO snapshots VALUES (?,?,?,1)', id, data, id); this.rows('UPDATE chunks SET generation=? WHERE generation=?', id, 'creating'); }
      this.walk(id); this.retain(id); this.put('snapshot', b64(snapshot)); this.rows('DELETE FROM journal WHERE idx<=?', key(app.applied));
      return snapshot;
    });
  }
}
