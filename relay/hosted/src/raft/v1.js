import { frame, response } from './abi.js';
import { encodeCanonical } from './codec.js';
import {unb64} from './storage.js';

// Preserve the original public proof; Go alone translates legacy bodies and
// applies the compatibility rule. The surrounding ingress authenticates first,
// binds the committed identity, and supplies its own current clock sample.
export function v1Command(runtime, cluster, now, request) {
  const proof = new TextEncoder().encode(encodeCanonical({...request,body:null}));
  const r = response(runtime.call(frame(40).str(cluster).u64(now).blob(proof).blob(unb64(request.body)).finish()));
  const commands = Array.from({length:r.u32()},()=>r.blob()); r.end(); return commands.length===1?commands[0]:commands;
}
