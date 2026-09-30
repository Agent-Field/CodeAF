// Reading a request body under a cap. An oversized upload costs the relay at most one chunk
// past the cap, and is refused as 413, never as a malformed request.
import { Wire } from './wire.js';

/** tooLarge is the refusal for a body over its cap; over names its code (the wire keeps two). */
const tooLarge = (over) => new Wire(over, 413);

/** checkDeclared refuses on the Content-Length alone, so a giant upload is turned away unread. */
export function checkDeclared(request, limit, over) {
  if (Number(request.headers.get('content-length') ?? 0) > limit) throw tooLarge(over);
}

const join = (chunks, total) => {
  const out = new Uint8Array(total);
  let at = 0;
  for (const chunk of chunks) (out.set(chunk, at), (at += chunk.length));
  return out;
};

/** readBody reads the whole body, stopping as soon as it passes limit bytes. */
export async function readBody(request, limit, over) {
  checkDeclared(request, limit, over);
  const chunks = [];
  let total = 0;
  for await (const chunk of request.body ?? []) {
    total += chunk.length;
    if (total > limit) throw tooLarge(over);
    chunks.push(chunk);
  }
  return join(chunks, total);
}
