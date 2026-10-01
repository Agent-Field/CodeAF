// The link wire (docs/ux-pairing-contract.md sections 3.1, 3.2 and 3.6) as the front Worker serves it:
// the routes a new device uses, which take no identity. The shape of a request is checked here, the
// per-IP decisions are LinkGate's, and the record is the LinkRequest object named by the code.
import { readBody } from '../body.js';
import { limitsOf } from '../limits.js';
import { badRequest, empty, ipOf, json, unwrap, Wire } from '../wire.js';
import { codeOf, newRequest } from './code.js';
import { gone, tooBig } from './refusals.js';

const MAX_WAIT_MS = 25_000;
const MAX_CREATE_BODY = 1024;

const gateOf = (c) => c.env.LINK_GATE.get(c.env.LINK_GATE.idFromName('gate'));
const requestOf = (c, code) => c.env.LINK_REQUEST.get(c.env.LINK_REQUEST.idFromName(code));

/** intOf reads a non-negative integer query value; anything else is 0. */
function intOf(url, name) {
  const text = url.searchParams.get(name);
  return /^\d{1,9}$/.test(text ?? '') ? Number(text) : 0;
}

async function parseObject(request) {
  const bytes = await readBody(request, MAX_CREATE_BODY, tooBig().code);
  const body = JSON.parse(new TextDecoder().decode(bytes));
  if (body === null || typeof body !== 'object' || Array.isArray(body)) throw badRequest();
  return body;
}

/** poll holds one of the caller's poll slots for as long as `read` takes; a read without a wait holds none. */
async function withPollSlot(c, wait, read) {
  if (wait === 0) return read();
  const token = unwrap(await gateOf(c).acquirePoll(c.ip));
  try {
    return await read();
  } finally {
    await gateOf(c).releasePoll(token);
  }
}

async function readRequest(c, [text]) {
  unwrap(await gateOf(c).admitRead(c.ip));
  const code = codeOf(text);
  const wait = Math.min(intOf(c.url, 'wait'), MAX_WAIT_MS);
  const answer = code && (await withPollSlot(c, wait, () => requestOf(c, code).read(wait)));
  if (!answer || answer.err === 'gone') {
    await gateOf(c).miss(c.ip);
    throw gone();
  }
  const record = unwrap(answer);
  return record ? json(record) : empty();
}

const ROUTES = [
  ['GET', /^\/v1\/link\/limits$/, (c) => json(limitsView(c.limits))],
  ['POST', /^\/v1\/link\/requests$/, async (c) => json(unwrap(await gateOf(c).create(c.ip, await newRequest(await parseObject(c.request)))), 201)],
  ['GET', /^\/v1\/link\/requests\/([^/]+)$/, readRequest],
];

const limitsView = (l) => ({
  ttl_ms: l.pairTtlMs,
  create_per_hour: l.linkCreatePerHour,
  pending_per_ip: l.linkPendingPerIp,
  read_per_minute: l.linkReadPerMinute,
  miss_per_minute: l.linkMissPerMinute,
  concurrent_polls: l.linkConcurrentPolls,
  max_grant: l.linkMaxGrant,
  max_live: l.linkMaxLive,
});

/** serveLink answers one request on /v1/link; a path no route names is 404 not_found. */
export function serveLink(request, env) {
  const url = new URL(request.url);
  for (const [method, pattern, handler] of ROUTES) {
    const hit = request.method === method && pattern.exec(url.pathname);
    if (hit) return handler({ request, env, url, ip: ipOf(request, env), limits: limitsOf(env) }, hit.slice(1));
  }
  throw new Wire('not_found', 404);
}
