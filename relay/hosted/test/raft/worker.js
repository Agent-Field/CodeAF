// This local-only door measures the actual Worker runtime. No production route
// imports it. Authenticated actors and persistent host adapters live in src/raft.
import core from './core.wasm';
import { Reactor } from '../../src/raft/abi.js';
export { IdentityDO, NewcomerGate, LinkGate, LinkRequest, PairGate, Mailbox } from '../../src/worker.js';
import production from '../../src/worker.js';
let reactor;
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === '/ready') return new Response(null, {status:204});
    if (url.pathname.startsWith('/store/')) {
      const [, , member, ...parts] = url.pathname.split('/');
      const stub = env.PROOF_STORE.get(env.PROOF_STORE.idFromName(member));
      return stub.fetch(new Request('http://proof/' + parts.join('/'), request));
    }
    if (url.pathname === '/production') return production.fetch(request, env);
    reactor ??= new Reactor(core);
    if (url.pathname === '/metrics') return Response.json(reactor.metrics());
    if (url.pathname !== '/call') return new Response('local proof only', { status: 404 });
    return new Response(reactor.call(new Uint8Array(await request.arrayBuffer())));
  },
};

