import { frame, response } from './abi.js';

const segment = s => [...new TextEncoder().encode(s)].map(b => b.toString(16).padStart(2, '0')).join('');

// Run under the member queue AFTER committing current ingress time and passing
// a fresh current-term quorum barrier. Go alone decides scopes and expiry;
// this helper does not provide an independent local-success read door.
export function workerCell(runtime, store, cluster, worker, cell, incarnation) {
  const app = store.application(), w = frame(27).str(cluster).str(worker).str(cell).u64(incarnation).u64(app.now);
  const keys = [['state','identity'], ['enrollment','actor/' + segment(worker)], ['state','device/' + segment(worker)], ['state','worker/' + segment(worker)], ['state','cell/' + segment(cell)]];
  for (const [namespace,key] of keys) w.blob(store.read(namespace,key) ?? new Uint8Array());
  const r = response(runtime.call(w.finish())), raw = r.blob(); r.end();
  return raw;
}
