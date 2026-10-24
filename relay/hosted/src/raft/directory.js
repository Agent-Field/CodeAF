import { frame, response } from './abi.js';
import { unb64 } from './storage.js';

// JavaScript supplies immutable dependency bytes and persists the Go write-set.
// It never interprets leases, clocks, revocations, CAS or request sequences.
export function directoryEvaluator(runtime, cluster, ceiling) {
  return (command, read, app) => {
    let r = response(runtime.call(frame(11).blob(command).finish()));
    const keys = Array.from({ length: r.u32() }, () => ({ namespace: r.str(), key: r.str() })); r.end();
    const w = (app.index === undefined ? frame(10) : frame(15)).blob(command).str(cluster).u64(app.now).u64(app.version).u64(app.size).u64(ceiling).u32(keys.length);
    for (const k of keys) { const data = read(k.namespace, k.key); w.str(k.namespace).str(k.key).u32(data == null ? 0 : 1).blob(data ?? new Uint8Array()); }
    if (app.index !== undefined) w.u64(app.index).blob(unb64(app.conf));
    r = response(runtime.call(w.finish()));
    const now = r.u64(), version = r.u64(), result = r.blob();
    const writes = Array.from({ length: r.u32() }, () => ({ namespace: r.str(), key: r.str(), data: r.blob() })); r.end();
    return { now, version, result, writes };
  };
}

// The consensus host implements commit and barrier, not a local-success cache.
// Calls return only committed/durably applied bytes; a leader hint is routing.
export class RaftDirectoryFacade {
  constructor(host) { this.host = host; }
  async command(originalSignedBytes) { return this.host.commit(originalSignedBytes); }
  async read(authenticatedRequest) { return this.host.barrierRead(authenticatedRequest); }
}
