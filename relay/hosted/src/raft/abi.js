import { wasiPreview1 } from './wasi.js';

export const ABI_VERSION = 1;
export const MAX_BUFFER = 2 * 1024 * 1024;
export const CORE_MEMORY_BUDGET = 64 * 1024 * 1024;
const utf8 = new TextEncoder();
const text = new TextDecoder('utf-8', { fatal: true });

export class Writer {
  constructor() { this.parts = []; this.length = 0; }
  raw(b) { if (!(b instanceof Uint8Array)) throw new TypeError('ABI bytes required'); this.parts.push(b); this.length += b.length; return this; }
  u32(n) { if (!Number.isInteger(n) || n < 0 || n > 0xffffffff) throw new RangeError('ABI uint32'); const b = new Uint8Array(4); new DataView(b.buffer).setUint32(0, n, true); return this.raw(b); }
  u64(n) { if ((typeof n === 'number' && !Number.isSafeInteger(n)) || BigInt(n) < 0n || BigInt(n) > ((1n << 64n) - 1n)) throw new RangeError('ABI uint64'); const b = new Uint8Array(8); new DataView(b.buffer).setBigUint64(0, BigInt(n), true); return this.raw(b); }
  blob(b = new Uint8Array()) { return this.u32(b.length).raw(b); }
  str(s) { return this.blob(utf8.encode(s)); }
  finish() { const b = new Uint8Array(this.length); let at = 0; for (const p of this.parts) { b.set(p, at); at += p.length; } return b; }
}
export class Reader {
  constructor(bytes) { this.bytes = bytes; this.at = 0; this.view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength); }
  take(n) { if (!Number.isInteger(n) || n < 0 || this.at + n > this.bytes.length) throw new Error('ABI truncated'); const b = this.bytes.slice(this.at, this.at + n); this.at += n; return b; }
  u32() { const at = this.at; this.take(4); return this.view.getUint32(at, true); }
  u64() { const at = this.at; this.take(8); return this.view.getBigUint64(at, true); }
  blob() { return this.take(this.u32()); }
  str() { return text.decode(this.blob()); }
  entries() { const n = this.u32(); if (n > MAX_BUFFER / 4) throw new Error('ABI count'); return Array.from({ length: n }, () => this.blob()); }
  end() { if (this.at !== this.bytes.length) throw new Error('ABI trailing bytes'); }
}
export const frame = (op) => new Writer().u32(ABI_VERSION).u32(op);
export function response(bytes) {
  const r = new Reader(bytes);
  if (r.u32() !== ABI_VERSION) throw new Error('ABI version');
  const error = r.str(); if (error) { r.end(); throw new Error(error); }
  return r;
}
export function createFrame({ cluster, id, seed, bootstrap = [], hard = new Uint8Array(), conf = new Uint8Array(), snapshot = new Uint8Array(), entries = [], applied = 0n }) {
  const w = frame(1).str(cluster).u64(id).u64(seed).u32(1).u32(bootstrap.length);
  for (const id of bootstrap) w.u64(id);
  w.blob(hard).blob(conf).blob(snapshot).u32(entries.length);
  for (const e of entries) w.blob(e);
  return w.u64(applied).finish();
}
export function stepFrame(handle, tag, value) {
  const w = frame(3).u32(handle).u32(tag);
  if ([2, 3, 8].includes(tag)) w.blob(value);
  else if ([4, 10].includes(tag)) w.str(value);
  else if ([5, 6, 7, 9].includes(tag)) w.u64(value);
  return w.finish();
}
export function effects(bytes) {
  const r = response(bytes), v = {};
  if (r.u32()) v.persist = { id: r.u64(), hard: r.blob(), entries: r.entries(), snapshot: r.blob() };
  v.send = Array.from({ length: r.u32() }, () => ({ to: r.u64(), bytes: r.blob() }));
  if (r.u32()) v.apply = { id: r.u64(), index: r.u64(), entries: r.entries(), snapshot: r.blob(), conf: r.blob() };
  v.reads = Array.from({ length: r.u32() }, () => ({ id: r.str(), index: r.u64() }));
  v.status = { id: r.u64(), leader: r.u64(), term: r.u64(), commit: r.u64(), applied: r.u64(), role: r.u32(), rngState: r.u64(), rngDraws: r.u64(), stopped: !!r.u32() };
  v.tick = !!r.u32(); r.end(); return v;
}

// A Worker imports a trusted precompiled module. No runtime compilation or
// await occurs within a call; the shared instance cannot interleave members.
export class Reactor {
  constructor(module) {
    this.wasi = wasiPreview1();
    this.instance = new WebAssembly.Instance(module, { wasi_snapshot_preview1: this.wasi.imports });
    this.exports = this.instance.exports;
    this.wasi.attach(this.exports.memory);
    this.poisoned = false;
    this.exports._initialize();
    if (this.exports.codeaf_abi_version() !== ABI_VERSION) throw new Error('ABI version');
    this.peakMemory = this.memory;
  }
  get memory() { return this.exports.memory.buffer.byteLength; }
  check() {
    this.peakMemory = Math.max(this.peakMemory, this.memory);
    if (this.memory > CORE_MEMORY_BUDGET) { this.poisoned = true; throw new Error('core memory capacity; stop and recover'); }
    if (this.poisoned) throw new Error('reactor stopped; recover');
  }
  call(bytes) {
    if (!(bytes instanceof Uint8Array) || bytes.length === 0 || bytes.length > MAX_BUFFER) throw new Error('ABI frame capacity');
    this.check();
    try {
      const p = this.exports.codeaf_alloc(bytes.length);
      if (!p) throw new Error('ABI allocation');
      this.check(); new Uint8Array(this.exports.memory.buffer, p, bytes.length).set(bytes);
      const n = this.exports.codeaf_call(bytes.length);
      this.check();
      if (n === 0 || n > MAX_BUFFER) throw new Error('ABI result capacity');
      return new Uint8Array(this.exports.memory.buffer, this.exports.codeaf_result(), n).slice();
    } catch (e) { this.poisoned = true; throw e; }
    finally { this.exports.codeaf_free(); }
  }
  metrics() { return { memory: this.memory, peakMemory: this.peakMemory, wasi: { ...this.wasi.calls } }; }
}
