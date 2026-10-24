import { frame, response } from './abi.js';
import { encodeCanonical } from './codec.js';

// Preserve the original public proof; Go alone translates legacy bodies and
// applies the compatibility rule. The surrounding ingress authenticates first,
// binds the committed identity, and supplies its own current clock sample.
export function v1Command(runtime, cluster, now, request) {
  const proof = new TextEncoder().encode(encodeCanonical(request));
  const r = response(runtime.call(frame(29).str(cluster).u64(now).blob(proof).finish()));
  const command = r.blob(); r.end(); return command;
}
