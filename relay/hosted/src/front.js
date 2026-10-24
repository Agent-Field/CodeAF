// The front door's routing, kept apart from the Worker entry so a test can drive it without the
// Durable Object classes (which only load inside the Workers runtime).
import { stripBase } from './base.js';
import { checkDeclared } from './body.js';
import { serveLink } from './link/front.js';
import { isPagePath, servePage } from './page.js';
import { servePair } from './pair/front.js';
import { matchRoute } from './routes.js';
import { identityOf } from './verify.js';
import { notFound, respond } from './wire.js';


/** forward hands an identity's request to its Durable Object, after refusing what needs no object to refuse. */
async function forward(request, env) {
  const route = matchRoute(request.method, new URL(request.url).pathname);
  checkDeclared(request, route.limit, route.over);
  const identity = await identityOf(request.headers.get('codeaf-identity'));
  return env.IDENTITY.get(env.IDENTITY.idFromName(identity)).fetch(request);
}

/** dispatch serves a request under the deployment's base path; anything outside that prefix is a 404. */
export function dispatch(outer, env) {
  if (isPagePath(outer)) return servePage(outer); // a link opened in a browser lives on the bare host, outside the base path
  const request = stripBase(outer, env);
  if (!request) throw notFound();
  const { pathname } = new URL(request.url);
  if (pathname.startsWith('/v1/link/')) return serveLink(request, env);
  if (pathname.startsWith('/v1/pair')) return servePair(request, env);
  if (pathname.startsWith('/v1/')) return forward(request, env);
  throw notFound();
}
