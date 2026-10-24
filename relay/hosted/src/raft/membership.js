import { frame, response } from './abi.js';
import { unb64 } from './storage.js';
import { encodeCanonical } from './codec.js';

const decode = b => JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(b));
const segment = s => [...new TextEncoder().encode(s)].map(b => b.toString(16).padStart(2, '0')).join('');

// The host pages public addressing dependencies; Go decides roles and revoked
// authority. Run under the member queue after durable apply and before replies.
export function committedRegistry(runtime, store) {
  const header = store.read('membership', 'registry');
  if (!header) throw new Error('raft member: enrollment is absent');
  const app = store.application(), h = decode(header), view = {};
  for (const node of h.nodes) {
    const raw = store.read('enrollment', 'node/' + node);
    if (!raw) throw new Error('raft member: enrollment is incomplete');
    const m = decode(raw), device = m.device ? store.read('state', 'device/' + segment(m.device)) : null;
    const r = response(runtime.call(frame(16).u64(node).u64(app.applied).blob(unb64(app.conf)).blob(header).blob(raw).blob(device ?? new Uint8Array()).finish()));
    const peer = decode(r.blob()); r.end();
    view[peer.node] = { incarnation: peer.incarnation, key: peer.key, voter: peer.voter, revoked: peer.revoked };
  }
  return view;
}

// Only authenticated internal provisioning supplies the three public anchor
// records. Before registry commitment, personal keys cannot submit peer data.
export function provisionedRegistry(runtime, config) {
  const raw = new TextEncoder().encode(encodeCanonical({ anchors: config.anchors, voters: config.bootstrap }));
  const r = response(runtime.call(frame(19).blob(raw).finish())), peers = decode(r.blob()); r.end();
  return Object.fromEntries(peers.map(p => [p.node, { incarnation: p.incarnation, key: p.key, voter: p.voter, revoked: p.revoked }]));
}
