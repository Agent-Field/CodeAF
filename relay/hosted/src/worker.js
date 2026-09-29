// Option (c): the hosted relay with no Durable Object and no D1, a stateless Worker over R2.
import { respond, counted } from './relay.js';
import { R2Directory } from './r2dir.js';
import { R2Store } from './r2store.js';
import { AMENDED, STAGE1 } from './rules.js';
import { snapshot, reset } from './tally.js';

const policyOf = (env) => (env.LEASE_POLICY === 'stage1' ? STAGE1 : AMENDED);

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (env.DEV_TALLY === '1' && url.pathname === '/_tally') return Response.json(request.method === 'DELETE' ? (reset(), {}) : snapshot());
    if (env.DEV_TALLY === '1' && url.pathname === '/_dump') return Response.json(await new R2Directory(env.FRAMES, url.searchParams.get('identity'), Date.now, policyOf(env)).list());
    const open = (identity, tally) => {
      const bucket = counted(env.FRAMES, tally);
      return { dir: new R2Directory(bucket, identity, Date.now, policyOf(env)), store: new R2Store(bucket, identity) };
    };
    return respond(request, { open, where: 'c', wait: (p) => ctx.waitUntil(p) });
  },
};
