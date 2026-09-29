// Option (a): one Durable Object per identity holds the directory (SQLite) and sees every put,
// so the in-flight-put rule is kept exactly. Frames and their index live in R2.
import { DurableObject } from 'cloudflare:workers';
import { respond, counted, Wire } from './relay.js';
import { SqlDirectory } from './sqldir.js';
import { R2Store } from './r2store.js';
import { Flight } from './flight.js';
import { AMENDED, STAGE1 } from './rules.js';

const HAS_WAIT_MS = 30_000;
const b64u = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
const hex = (b) => [...b].map((x) => x.toString(16).padStart(2, '0')).join('');

/** identityName is the same rule as reqsign.IDOf, from the unsigned header, so routing needs no body. */
async function identityName(request) {
  const key = b64u(request.headers.get('codeaf-identity') ?? '');
  return 'id_' + hex(new Uint8Array(await crypto.subtle.digest('SHA-256', key)).subarray(0, 16));
}

/** WaitingStore makes Has wait for the puts in flight. */
class WaitingStore extends R2Store {
  constructor(bucket, identity, flight) {
    super(bucket, identity);
    this.flight = flight;
  }
  async has(rids) {
    if (!(await this.flight.idle(HAS_WAIT_MS))) throw new Wire('unreachable', 503);
    return super.has(rids);
  }
}

export class IdentityDO extends DurableObject {
  flight = new Flight();

  constructor(ctx, env) {
    super(ctx, env);
    this.policy = env.LEASE_POLICY === 'stage1' ? STAGE1 : AMENDED;
  }

  async fetch(request) {
    const url = new URL(request.url);
    const isPut = request.method === 'POST' && url.pathname === '/v1/store/frames';
    const leave = isPut ? this.flight.enter() : () => {}; // counted from arrival, before the body is read
    try {
      return await respond(request, { open: (id, tally) => this.open(id, tally), where: 'a', wait: (p) => this.ctx.waitUntil(p) });
    } finally {
      leave();
    }
  }

  open(identity, tally) {
    const bucket = counted(this.env.FRAMES, tally);
    return {
      dir: new SqlDirectory(this.ctx.storage.sql, identity, Date.now, this.policy),
      store: new WaitingStore(bucket, identity, this.flight),
    };
  }
}

export default {
  async fetch(request, env) {
    if (!new URL(request.url).pathname.startsWith('/v1/')) return new Response('not found', { status: 404 });
    let name;
    try {
      name = await identityName(request);
    } catch {
      return Response.json({ err: 'unauthorized' }, { status: 401 });
    }
    return env.IDENTITY.get(env.IDENTITY.idFromName(name)).fetch(request);
  },
};
