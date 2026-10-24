// The pairing wire (contract 18.4) as the front Worker serves it: it checks the shape of a
// request, asks PairGate for the global decisions (nameplates, per-IP limits), and forwards to
// the mailbox object named by the nameplate. Nothing here is signed; the joining device has no
// identity yet, so every limit is by the caller's IP and every box is short-lived.
import { readBody } from '../body.js';
import { base64, fromBase64Url, sha256, hex } from '../codec.js';
import { limitsOf } from '../limits.js';
import { empty, ipOf, json, unwrap } from '../wire.js';
import { GENERATION_HEADER, generationOf } from './generation.js';
import { forbidden, gone, tooBig } from './refusals.js';

const MAX_WAIT_MS = 25_000;
const KEY_BYTES = 16;

/** keyHashOf is the hex SHA-256 of the side key in Codeaf-Pair-Key, the only form of it the relay keeps. A request without a key owns nothing, which is what 403 says. */
async function keyHashOf(request) {
  const key = fromBase64Url(request.headers.get('codeaf-pair-key') ?? '');
  if (key?.length !== KEY_BYTES) throw forbidden();
  return hex(await sha256(key));
}

/** intOf reads a non-negative integer query value; anything else is the default, as in the self-hosted relay. */
function intOf(url, name) {
  const text = url.searchParams.get(name);
  return /^\d{1,9}$/.test(text ?? '') ? Number(text) : 0;
}

const gateOf = (c) => c.env.PAIR_GATE.get(c.env.PAIR_GATE.idFromName('gate'));
const boxOf = (c, nameplate) => c.env.MAILBOX.get(c.env.MAILBOX.idFromName(nameplate));

/** stamped adds the box's generation to an answer, when the box has one, so a device can carry it on its next call. */
function stamped(res, gen) {
  if (gen) res.headers.set(GENERATION_HEADER, gen);
  return res;
}

const ROUTES = [
  ['GET', /^\/v1\/pair\/limits$/, (c) => json({
    ttl_ms: c.limits.pairTtlMs,
    max_msg: c.limits.pairMaxMsg,
    max_msgs_per_side: c.limits.pairMaxMsgsPerSide,
    create_per_hour: c.limits.pairCreatePerHour,
    write_per_minute: c.limits.pairWritePerMinute,
  })],

  ['POST', /^\/v1\/pair$/, async (c) => {
    const { gen, ...answer } = unwrap(await gateOf(c).create(c.ip, await keyHashOf(c.request)));
    return stamped(json(answer, 201), gen);
  }],

  ['POST', /^\/v1\/pair\/(\d{1,4})\/([ab])$/, async (c, [nameplate, side]) => {
    const keyHash = await keyHashOf(c.request);
    const bytes = await readBody(c.request, c.limits.pairMaxMsg, tooBig().code);
    unwrap(await gateOf(c).admitWrite(c.ip));
    const { gen, ...posted } = unwrap(await boxOf(c, nameplate).post(side, keyHash, bytes, generationOf(c.request)));
    return stamped(json(posted, 201), gen);
  }],

  ['GET', /^\/v1\/pair\/(\d{1,4})\/([ab])$/, async (c, [nameplate, side]) => {
    const after = intOf(c.url, 'after');
    const wait = Math.min(intOf(c.url, 'wait'), MAX_WAIT_MS);
    const token = unwrap(await gateOf(c).acquirePoll(c.ip));
    try {
      const got = unwrap(await boxOf(c, nameplate).read(side, after, wait, generationOf(c.request)));
      return stamped(got.msgs ? json({ msgs: got.msgs.map(base64), next: got.next }) : empty(), got.gen);
    } finally {
      await gateOf(c).releasePoll(token);
    }
  }],

  ['DELETE', /^\/v1\/pair\/(\d{1,4})$/, async (c, [nameplate]) => {
    unwrap(await boxOf(c, nameplate).close(await keyHashOf(c.request), generationOf(c.request)));
    await gateOf(c).release(nameplate);
    return empty();
  }],
];

/** servePair answers one request on /v1/pair; a path no route names, a listing included, is 404. */
export function servePair(request, env) {
  const url = new URL(request.url);
  for (const [method, pattern, handler] of ROUTES) {
    const hit = request.method === method && pattern.exec(url.pathname);
    if (hit) return handler({ request, env, url, ip: ipOf(request, env), limits: limitsOf(env) }, hit.slice(1));
  }
  throw gone();
}
