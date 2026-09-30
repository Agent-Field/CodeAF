// The two wires an identity uses, as one table: /v1/store (contract 3) and /v1/dir (contract 4.4).
// A route names its method, its path, the largest body it accepts and the code an oversized body
// is refused with, and the handler. Handlers get {tenant, device, body} and the path's captures,
// and answer a Response or throw a refusal; the Durable Object does everything around them.
import { MAX_FRAME } from './frame.js';
import { Wire, badRequest, json, empty, notFound } from './wire.js';

const MAX_SMALL = 1 << 20; // directory.MaxBody, and every store body but a frame
const MAX_HAS = 1000;
const isRid = (r) => typeof r === 'string' && /^[0-9a-f]{64}$/.test(r);
const decoder = new TextDecoder();

/** object parses a JSON body that must be an object; an empty body is an empty object. */
function object(body) {
  const v = body.length ? JSON.parse(decoder.decode(body)) : {};
  if (v === null || typeof v !== 'object' || Array.isArray(v)) throw badRequest();
  return v;
}

const asRids = ({ rids }, max = MAX_HAS) => {
  if (!Array.isArray(rids)) throw badRequest();
  if (rids.length > max) throw new Wire('too_many', 400);
  if (!rids.every(isRid)) throw new Wire('bad_rid', 400);
  return rids;
};

const MAX_MANY = 256; // blobstore.MaxGetMany

// A batch get names at least one rid, no rid twice (the answer is a frame, which cannot repeat one).
const asMany = (body) => {
  const rids = asRids(object(body), MAX_MANY);
  if (rids.length === 0) throw badRequest();
  if (new Set(rids).size !== rids.length) throw new Wire('bad_rid', 400);
  return rids;
};

// The directory version the answer was read at; the whole turn is synchronous, so it cannot be older than the records (contract 21.5).
const withVersion = (res, version) => (res.headers.set('Codeaf-Dir-Version', String(version)), res);

// The watch is an upgrade and nothing else: a plain GET is told so (contract 21.2).
function watching(c) {
  if (c.request.headers.get('upgrade')?.toLowerCase() !== 'websocket') throw new Wire('upgrade_required', 426);
  return c.tenant.watch(c.device);
}

const octets = (bytes) => new Response(bytes, { headers: { 'content-type': 'application/octet-stream' } });

// Each entry: [method, path pattern, options, handler]. `frames` marks the one route whose body is
// a put in flight, which Has must not overtake.
const STORE = { over: 'bad_frame' };
const DIR = { over: 'too_large' };
// `write` marks a route a replaced identity refuses: every verb that changes a record or a frame, and
// the watch, which a replaced identity could only ever hear a thaw on (contract 21.2).
const STORE_WRITE = { ...STORE, write: true };
const DIR_WRITE = { ...DIR, write: true };

const ROUTES = [
  ['POST', /^\/v1\/store\/frames$/, { ...STORE_WRITE, limit: MAX_FRAME, frames: true }, async (c) => json(await c.tenant.putFrame(c.body))],
  ['GET', /^\/v1\/store\/objects\/([^/]+)$/, STORE, async (c, [rid]) => {
    if (!isRid(rid)) throw new Wire('bad_rid', 400);
    return octets(await c.tenant.getObject(rid));
  }],
  ['POST', /^\/v1\/store\/objects$/, STORE, async (c) => octets(await c.tenant.getMany(asMany(c.body)))],
  ['POST', /^\/v1\/store\/has$/, STORE, async (c) => json({ have: await c.tenant.has(asRids(object(c.body))) })],
  ['GET', /^\/v1\/store\/stats$/, STORE, (c) => json(c.tenant.stats.snapshot())],
  ['GET', /^\/v1\/dir\/list$/, DIR, (c) => withVersion(json(c.tenant.dir.list()), c.tenant.dir.version)],
  ['GET', /^\/v1\/dir\/watch$/, DIR_WRITE, (c) => watching(c)],
  ['GET', /^\/v1\/dir\/cells\/([^/]+)$/, DIR, (c, [id]) => json(c.tenant.dir.cell(id))],
  ['PUT', /^\/v1\/dir\/devices\/([^/]+)$/, DIR_WRITE, (c, [id]) => {
    if (id !== c.device) throw new Wire('unauthorized', 401);
    c.tenant.dir.putDevice(id, object(c.body));
    return empty();
  }],
  ['POST', /^\/v1\/dir\/devices\/([^/]+)\/revoke$/, DIR_WRITE, (c, [id]) => (c.tenant.dir.revoke(id, c.device), empty())],
  ['POST', /^\/v1\/dir\/vault$/, DIR_WRITE, (c) => {
    const { old, new: next } = object(c.body);
    c.tenant.dir.setVault(old, next);
    return empty();
  }],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)$/, DIR_WRITE, (c, [id]) => json(c.tenant.dir.create(id, object(c.body), c.device))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/acquire$/, DIR_WRITE, (c, [id]) => json(c.tenant.dir.acquire(id, c.device, object(c.body).force === true))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/heartbeat$/, DIR_WRITE, (c, [id]) => json(c.tenant.dir.heartbeat(id, c.device, object(c.body)))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/publish$/, DIR_WRITE, (c, [id]) => json(c.tenant.dir.publish(id, c.device, object(c.body)))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/release$/, DIR_WRITE, (c, [id]) => (c.tenant.dir.release(id, c.device, object(c.body).fence), empty())],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/archive$/, DIR_WRITE, (c, [id]) => (c.tenant.dir.archive(id), empty())],
  ['GET', /^\/v1\/identity\/rotation$/, DIR, (c) => json(c.tenant.rotationView())],
  ['POST', /^\/v1\/identity\/rotation$/, DIR, (c) => json(c.tenant.rotate(c.device, object(c.body)))],
];

/** matchRoute answers {limit, over, frames, write, handler, args} for a request, or throws not_found. */
export function matchRoute(method, path) {
  for (const [m, pattern, { limit = MAX_SMALL, over, frames = false, write = false }, handler] of ROUTES) {
    const hit = m === method && pattern.exec(path);
    if (hit) return { limit, over, frames, write, handler, args: hit.slice(1) };
  }
  throw notFound();
}
