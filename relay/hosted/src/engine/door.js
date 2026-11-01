// The front door's half of the machine doors: refuse what needs no object to refuse, then hand the upgrade to the
// one object that holds that machine name (machine.js).
import { ipOf } from '../wire.js';
import { validName } from './names.js';
import {
  DIALS_PER_MINUTE, DIAL_PREFIX, ENGINE_PATH, HEADER_NAME, PROTOCOL, REGISTRATIONS_PER_MINUTE,
  TOO_MANY_DIALS, TOO_MANY_REGISTRATIONS, plain,
} from './carrier.js';

/** isMachineDoor is a path this module serves. */
export const isMachineDoor = (pathname) => pathname === ENGINE_PATH || pathname.startsWith(DIAL_PREFIX);

/**
 * fallbackBuckets bounds per-caller cost in Worker memory if a custom deployment forgot the [[ratelimits]] bindings in
 * wrangler.toml. When the bindings are present, Cloudflare enforces the rate across isolates at the edge.
 */
const fallbackBuckets = new Map();

async function allowCaller(request, env, register) {
  const ip = ipOf(request, env);
  const limiter = register ? env.MACHINE_REGISTER_LIMIT : env.MACHINE_DIAL_LIMIT;
  if (limiter && typeof limiter.limit === 'function') {
    const { success } = await limiter.limit({ key: ip });
    return Boolean(success);
  }
  const perMinute = register ? REGISTRATIONS_PER_MINUTE : DIALS_PER_MINUTE;
  const minute = Math.floor(Date.now() / 60000);
  for (const [key, bucket] of fallbackBuckets) if (bucket.minute !== minute) fallbackBuckets.delete(key);
  const key = (register ? 'register ' : 'dial ') + ip;
  const bucket = fallbackBuckets.get(key) ?? { minute, count: 0 };
  bucket.count += 1;
  fallbackBuckets.set(key, bucket);
  return bucket.count <= perMinute;
}

/**
 * serveMachineDoor routes a registration or a dial. Only a WebSocket offering the carrier's protocol is taken: a
 * Worker cannot answer codeaf's raw upgrade, and the 426 is what tells a client to use the WebSocket. Rate limits and
 * name validity are enforced here before env.MACHINE is touched so one caller cannot enumerate names or instantiate
 * arbitrary Durable Objects.
 */
export async function serveMachineDoor(request, env) {
  const offered = (request.headers.get('sec-websocket-protocol') ?? '').split(',').map((p) => p.trim());
  if ((request.headers.get('upgrade') ?? '').toLowerCase() !== 'websocket' || !offered.includes(PROTOCOL)) {
    return plain(426, `this door speaks ${PROTOCOL} over a WebSocket`);
  }
  const { pathname } = new URL(request.url);
  const register = pathname === ENGINE_PATH;
  if (!(await allowCaller(request, env, register))) {
    return plain(429, register ? TOO_MANY_REGISTRATIONS : TOO_MANY_DIALS);
  }
  const name = register ? request.headers.get(HEADER_NAME) ?? '' : pathname.slice(DIAL_PREFIX.length);
  if (!validName(name)) return plain(400, 'that is not a machine name');
  const answer = await env.MACHINE.get(env.MACHINE.idFromName(name)).fetch(request);
  // An object's answer has immutable headers, and the Worker stamps Codeaf-Now on every answer.
  return new Response(answer.body, { status: answer.status, headers: answer.headers, webSocket: answer.webSocket });
}
