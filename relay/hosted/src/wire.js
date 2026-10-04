// The wire's vocabulary shared by every part of the relay: one error type that carries the
// code and status a client sees, one table that turns each internal failure into one, and the
// answers themselves. Every answer carries Codeaf-Now, the relay's clock, refusals included,
// because a client that was refused for skew needs the time most of all.
import { Refusal } from './verify.js';
import { BadFrame } from './frame.js';
import { RuleError } from './rules.js';
import { Conflict, Damaged } from './store.js';
import { networkOf } from './network.js';

export class Wire extends Error {
  constructor(code, status, retryAfter, limitBytes) {
    super(code);
    this.code = code;
    this.status = status;
    this.retryAfter = retryAfter; // seconds, for a refusal that time cures
    this.limitBytes = limitBytes; // the byte ceiling, for a refusal that names one
  }
}

/** Refusals the relay raises itself; each is one code with one status. */
export const notFound = () => new Wire('not_found', 404);
export const badRequest = () => new Wire('bad_request', 400);
export const rateLimited = (seconds) => new Wire('rate_limited', 429, seconds);
/** full is 507; limitBytes, when the stored-bytes ceiling is what was hit, is told to the client so it can say how big the ceiling is. */
export const full = (limitBytes) => new Wire('full', 507, undefined, limitBytes);
/** gone is an identity the relay replaced and deleted; its id is answered forever and never made again. */
export const gone = () => new Wire('gone', 410);
export const tooManyIdentities = (seconds) => new Wire('too_many_identities', 429, seconds);

// The IP is the socket peer as Cloudflare reports it (CF-Connecting-IP), named by its network (see network.js), so every
// per-caller limit counts an IPv6 caller by its /64. A request without one shares a single "unknown" allowance, which is
// the strict way to fail.
export function ipOf(request, env) {
  const address = (env.TRUST_PROXY === '1' ? forwarded(request) : null) ?? request.headers.get('cf-connecting-ip') ?? 'unknown';
  return networkOf(address);
}

// With TRUST_PROXY set (a test rig that stands where a trusted proxy would), a caller may name its own
// network in X-Forwarded-For, first address first, as the self-hosted relay's --trust-proxy allows.
const forwarded = (request) => request.headers.get('x-forwarded-for')?.split(',')[0].trim() || null;

// The status of each rule refusal the directory rules raise.
const RULE_STATUS = { not_found: 404, exists: 409, lease_held: 409, fence_stale: 409, head_moved: 409, cas: 409, self_revoke: 400, bad_request: 400, rotated: 410, rotation_step: 409, bad_grace: 400 };

/** wireOf names any failure as the Wire refusal a client should see. */
export function wireOf(e) {
  if (e instanceof Wire) return e;
  if (e instanceof Refusal) return new Wire(e.kind, 401);
  if (e instanceof BadFrame) return new Wire('bad_frame', 400);
  if (e instanceof Conflict) return new Wire('conflict', 409);
  if (e instanceof Damaged) return new Wire('damaged', 500);
  if (e instanceof RuleError) return new Wire(e.code, RULE_STATUS[e.code] ?? 500);
  if (e instanceof SyntaxError) return badRequest();
  return new Wire('internal', 500);
}

export const json = (value, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { 'content-type': 'application/json' } });
export const empty = () => new Response(null, { status: 204 });

function refusal(w) {
  // retry_after is the Retry-After header again, in the body, for a client that reads the body and not the headers.
  const wait = w.retryAfter && Math.ceil(w.retryAfter);
  const res = json({ err: w.code, ...(w.limitBytes && { limit_bytes: w.limitBytes }), ...(wait && { retry_after: wait }) }, w.status);
  if (wait) res.headers.set('retry-after', String(wait));
  return res;
}

/** respond runs one request and answers it, stamped with the clock that judged it (an answer already stamped keeps its stamp). */
export async function respond(run, clock = Date.now) {
  let res;
  try {
    res = await run();
  } catch (e) {
    const w = wireOf(e);
    if (w.status === 500) console.error(e);
    res = refusal(w);
  }
  if (!res.headers.has('Codeaf-Now')) res.headers.set('Codeaf-Now', String(clock()));
  return res;
}

/**
 * guarded runs one call between Durable Objects and answers {ok} or {err}: a thrown Error
 * loses its class across the boundary, so a refusal travels as data and unwrap re-raises it.
 */
export async function guarded(fn) {
  try {
    return { ok: await fn() };
  } catch (e) {
    const { code, status, retryAfter, limitBytes } = wireOf(e);
    return { err: code, status, retryAfter, limitBytes };
  }
}

export function unwrap(answer) {
  if ('err' in answer) throw new Wire(answer.err, answer.status, answer.retryAfter, answer.limitBytes);
  return answer.ok;
}
