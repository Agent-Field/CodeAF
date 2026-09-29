// The hosted relay with no Durable Object and no D1: a stateless Worker over R2.
// Same wires as internal/relayserve: /v1/store/* and /v1/dir/*, every request signed.
import { verify, Refusal } from './verify.js';
import { BadFrame, MAX_FRAME } from './frame.js';
import { RuleError } from './rules.js';
import { R2Directory } from './r2dir.js';
import { R2Store } from './r2store.js';
import { tallyOf, counted, countRequest, snapshot, reset } from './tally.js';

const HEADERS = { identity: 'codeaf-identity', cert: 'codeaf-cert', time: 'codeaf-time', sig: 'codeaf-sig' };
const MAX_SMALL = 1 << 20;
const MAX_HAS = 1000;
const RID = /^[0-9a-f]{64}$/;
const dec = new TextDecoder();
const parse = (body) => (body.length ? JSON.parse(dec.decode(body)) : {});

class Wire extends Error {
  constructor(code, status) {
    super(code);
    this.code = code;
    this.status = status;
  }
}

// One table for every refusal: the class it comes from, the code it carries, its status.
const STATUS = { not_found: 404, exists: 409, lease_held: 409, fence_stale: 409, head_moved: 409, cas: 409, too_large: 413 };
function wireOf(e) {
  if (e instanceof Wire) return e;
  if (e instanceof Refusal) return new Wire(e.kind, 401);
  if (e instanceof BadFrame) return new Wire('bad_frame', 400);
  if (e instanceof RuleError) return new Wire(e.code, STATUS[e.code] ?? 500);
  if (e instanceof SyntaxError) return new Wire('bad_request', 400);
  return new Wire('internal', 500);
}

const json = (v, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'content-type': 'application/json' } });
const empty = () => new Response(null, { status: 204 });

// Routes: [method, pattern, name, handler(ctx)]. ctx = {dir, store, device, id, body, tally, request, wait}.
const ROUTES = [
  ['POST', /^\/v1\/store\/frames$/, 'put', async (c) => {
    c.tally.puts = (c.tally.puts ?? 0) + 1;
    const put = c.store.put(c.body);
    c.wait(put);
    return json(await put);
  }],
  ['GET', /^\/v1\/store\/objects\/([^/]+)$/, 'get', async (c, [rid]) => {
    if (!RID.test(rid)) throw new Wire('bad_rid', 400);
    const bytes = await c.store.get(rid);
    if (!bytes) throw new Wire('not_found', 404);
    c.tally.bytes_out += bytes.length;
    return new Response(bytes, { headers: { 'content-type': 'application/octet-stream' } });
  }],
  ['POST', /^\/v1\/store\/has$/, 'has', async (c) => {
    const { rids } = parse(c.body);
    if (rids.length > MAX_HAS) throw new Wire('too_many', 400);
    if (!rids.every((r) => RID.test(r))) throw new Wire('bad_rid', 400);
    return json({ have: await c.store.has(rids) });
  }],
  ['GET', /^\/v1\/store\/stats$/, 'stats', (c) => json({ puts: c.tally.puts ?? 0, gets: c.tally.requests.get ?? 0, has: c.tally.requests.has ?? 0, bytes_in: c.tally.bytes_in, bytes_out: c.tally.bytes_out })],
  ['GET', /^\/v1\/dir\/list$/, 'dir.list', async (c) => json(await c.dir.list())],
  ['GET', /^\/v1\/dir\/cells\/([^/]+)$/, 'dir.cell', async (c, [id]) => json(await c.dir.cell(id))],
  ['PUT', /^\/v1\/dir\/devices\/([^/]+)$/, 'dir.device', async (c, [id]) => {
    if (id !== c.device) throw new Wire('unauthorized', 401);
    await c.dir.putDevice(id, parse(c.body));
    return empty();
  }],
  ['POST', /^\/v1\/dir\/vault$/, 'dir.vault', async (c) => {
    const { old, new: next } = parse(c.body);
    await c.dir.setVault(old, next);
    return empty();
  }],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)$/, 'dir.create', async (c, [id]) => json(await c.dir.create(id, parse(c.body), c.device))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/acquire$/, 'dir.acquire', async (c, [id]) => json(await c.dir.acquire(id, c.device, parse(c.body).force === true))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/heartbeat$/, 'dir.heartbeat', async (c, [id]) => json(await c.dir.heartbeat(id, c.device, parse(c.body)))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/publish$/, 'dir.publish', async (c, [id]) => json(await c.dir.publish(id, c.device, parse(c.body)))],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/release$/, 'dir.release', async (c, [id]) => (await c.dir.release(id, c.device, parse(c.body).fence), empty())],
  ['POST', /^\/v1\/dir\/cells\/([^/]+)\/archive$/, 'dir.archive', async (c, [id]) => (await c.dir.archive(id), empty())],
];

function match(method, path) {
  for (const [m, re, name, handler] of ROUTES) {
    const hit = m === method && re.exec(path);
    if (hit) return { name, handler, args: hit.slice(1) };
  }
  throw new Wire('not_found', 404);
}

async function readBody(request, limit) {
  if (Number(request.headers.get('content-length') ?? 0) > limit) throw new Wire(limit === MAX_FRAME ? 'bad_frame' : 'too_large', 413);
  const body = new Uint8Array(await request.arrayBuffer());
  if (body.length > limit) throw new Wire(limit === MAX_FRAME ? 'bad_frame' : 'too_large', 413);
  return body;
}

async function serve(request, env, ctx) {
  const url = new URL(request.url);
  const { name, handler, args } = match(request.method, url.pathname);
  const body = await readBody(request, name === 'put' ? MAX_FRAME : MAX_SMALL);
  const headers = Object.fromEntries(Object.entries(HEADERS).map(([k, h]) => [k, request.headers.get(h)]));
  const who = await verify({ method: request.method, uri: url.pathname + url.search, headers }, body, Date.now());
  const tally = tallyOf(who.identity);
  countRequest(tally, name);
  tally.bytes_in += name === 'put' ? body.length : 0;
  const bucket = counted(env.FRAMES, tally);
  const c = { dir: new R2Directory(bucket, who.identity), store: new R2Store(bucket, who.identity), device: who.device, body, tally, wait: (p) => ctx.waitUntil(p.catch(() => {})) };
  return handler(c, args);
}

async function respond(request, env, ctx) {
  try {
    return await serve(request, env, ctx);
  } catch (e) {
    const w = wireOf(e);
    if (w.status === 500) console.error(e);
    return json({ err: w.code }, w.status);
  }
}

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (env.DEV_TALLY === '1' && url.pathname === '/_tally') return json(request.method === 'DELETE' ? (reset(), {}) : snapshot());
    if (env.DEV_TALLY === '1' && url.pathname === '/_dump') return json(await new R2Directory(env.FRAMES, url.searchParams.get('identity')).list());
    const res = await respond(request, env, ctx);
    res.headers.set('Codeaf-Now', String(Date.now()));
    return res;
  },
};
