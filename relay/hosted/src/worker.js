// The hosted relay's front door: a stateless Worker that decides where a request goes and does
// nothing else. Pairing goes to its own objects; the store and directory wires go to the
// Durable Object of the identity named in the request's unsigned header, and the body is not
// read here. That routing before reading is what keeps one identity's slow put from ever
// delaying another identity's Has, and what lets an identity's object count its own puts.
import { stripBase } from './base.js';
import { checkDeclared } from './body.js';
import { serveLink } from './link/front.js';
import { servePair } from './pair/front.js';
import { matchRoute } from './routes.js';
import { identityOf } from './verify.js';
import { notFound, respond } from './wire.js';

export { IdentityDO } from './identity.js';
export { NewcomerGate } from './newcomers.js';
export { LinkGate } from './link/gate.js';
export { LinkRequest } from './link/request.js';
export { PairGate } from './pair/gate.js';
export { Mailbox } from './pair/mailbox.js';

/** forward hands an identity's request to its Durable Object, after refusing what needs no object to refuse. */
async function forward(request, env) {
  const route = matchRoute(request.method, new URL(request.url).pathname);
  checkDeclared(request, route.limit, route.over);
  const identity = await identityOf(request.headers.get('codeaf-identity'));
  return env.IDENTITY.get(env.IDENTITY.idFromName(identity)).fetch(request);
}

/** dispatch serves a request under the deployment's base path; anything outside that prefix is a 404. */
function dispatch(outer, env) {
  const request = stripBase(outer, env);
  if (!request) throw notFound();
  const { pathname } = new URL(request.url);
  if (pathname.startsWith('/v1/link/')) return serveLink(request, env);
  if (pathname.startsWith('/v1/pair')) return servePair(request, env);
  if (pathname.startsWith('/v1/')) return forward(request, env);
  throw notFound();
}

export default {
  fetch: (request, env) => respond(() => dispatch(request, env)),
};
