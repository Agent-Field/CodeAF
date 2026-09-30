// The pairing wire (contract 18.4) as the front Worker serves it: it checks the shape of a
// request, asks PairGate for the global decisions (nameplates, per-IP limits), and forwards to
// the mailbox object named by the nameplate. Nothing here is signed; the joining device has no
// identity yet, so every limit is by the caller's IP and every box is short-lived.
import { readBody } from '../body.js';
import { base64, fromBase64Url, sha256, hex } from '../codec.js';
import { limitsOf } from '../limits.js';
import { badRequest, empty, ipOf, json, notFound, unwrap } from '../wire.js';

const MAX_WAIT_MS = 25_000;
const KEY_BYTES = 16;

/** keyHashOf is the hex SHA-256 of the side key in Codeaf-Pair-Key, the only form of it the relay keeps. */
async function keyHashOf(request) {
  const key = fromBase64Url(request.headers.get('codeaf-pair-key') ?? '');
  if (key?.length !== KEY_BYTES) throw badRequest();
  return hex(await sha256(key));
}

/** intOf reads a non-negative integer query value, or the default when absent. */
function intOf(url, name, fallback) {
  const text = url.searchParams.get(name);
  if (text === null) return fallback;
  if (!/^\d{1,9}$/.test(text)) throw badRequest();
  return Number(text);
}

const gateOf = (c) => c.env.PAIR_GATE.get(c.env.PAIR_GATE.idFromName('gate'));
const boxOf = (c, nameplate) => c.env.MAILBOX.get(c.env.MAILBOX.idFromName(nameplate));

const ROUTES = [
  ['GET', /^\/v1\/pair\/limits$/, (c) => json({
    ttl_ms: c.limits.pairTtlMs,
    max_msg: c.limits.pairMaxMsg,
    max_msgs_per_side: c.limits.pairMaxMsgsPerSide,
    create_per_hour: c.limits.pairCreatePerHour,
    write_per_minute: c.limits.pairWritePerMinute,
  })],

  ['POST', /^\/v1\/pair$/, async (c) => {
    const answer = unwrap(await gateOf(c).create(c.ip, await keyHashOf(c.request)));
    return json(answer, 201);
  }],

  ['POST', /^\/v1\/pair\/(\d{1,4})\/([ab])$/, async (c, [nameplate, side]) => {
    const keyHash = await keyHashOf(c.request);
    const bytes = await readBody(c.request, c.limits.pairMaxMsg, 'too_large');
    unwrap(await gateOf(c).admitWrite(c.ip));
    return json(unwrap(await boxOf(c, nameplate).post(side, keyHash, bytes)), 201);
  }],

  ['GET', /^\/v1\/pair\/(\d{1,4})\/([ab])$/, async (c, [nameplate, side]) => {
    const after = intOf(c.url, 'after', 0);
    const wait = Math.min(intOf(c.url, 'wait', 0), MAX_WAIT_MS);
    const token = unwrap(await gateOf(c).acquirePoll(c.ip));
    try {
      const got = unwrap(await boxOf(c, nameplate).read(side, after, wait));
      return got ? json({ msgs: got.msgs.map(base64), next: got.next }) : empty();
    } finally {
      await gateOf(c).releasePoll(token);
    }
  }],

  ['DELETE', /^\/v1\/pair\/(\d{1,4})$/, async (c, [nameplate]) => {
    unwrap(await boxOf(c, nameplate).close(await keyHashOf(c.request)));
    await gateOf(c).release(nameplate);
    return empty();
  }],
];

/** servePair answers one request on /v1/pair; a path no route names, a listing included, is 404. */
export function servePair(request, env) {
  const url = new URL(request.url);
  for (const [method, pattern, handler] of ROUTES) {
    const hit = request.method === method && pattern.exec(url.pathname);
    if (hit) return handler({ request, env, url, ip: ipOf(request), limits: limitsOf(env) }, hit.slice(1));
  }
  throw notFound();
}
