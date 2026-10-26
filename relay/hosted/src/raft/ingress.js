import { frame, response } from './abi.js';
import { decodeCanonical } from './codec.js';
import { coordinationPolicy } from './liveness.js';

let requestLimit;
// The front and identity doors use Go's actual protocol bound, so a schema
// limit cannot drift between pre-body admission and the committed verifier.
export function clusterRequestLimit(runtime){return requestLimit??=coordinationPolicy(runtime).max_message_bytes;}

// The same closed verifier authenticates native and hosted ingress. Repeating
// it at committed time prevents a delayed request reviving expired authority.
export function clusterIngress(runtime, raw, now) {
  const r=response(runtime.call(frame(33).blob(raw).u64(now).finish()));
  const actor=r.str();r.end();
  return {actor,request:decodeCanonical(new TextDecoder().decode(raw))};
}
