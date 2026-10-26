// The front door's routing, kept apart from the Worker entry so a test can drive it without the
// Durable Object classes (which only load inside the Workers runtime).
import { peerPath,peerLimits } from './raft/peers.js';
import { stripBase } from './base.js';
import { clusterRequestLimit } from './raft/ingress.js';
import { sharedRuntime } from './raft/actor.js';
import { checkDeclared,readBody } from './body.js';
import { serveLink } from './link/front.js';
import { isPagePath, servePage } from './page.js';
import { servePair } from './pair/front.js';
import { matchRoute } from './routes.js';
import { identityOf } from './verify.js';
import { notFound, respond } from './wire.js';


/** forward hands an identity's request to its Durable Object, after refusing what needs no object to refuse. */
async function forward(request, env) {
  const path=new URL(request.url).pathname;
  if(peerPath(path)){
    if(request.method!==(path.endsWith('/exchange')?'POST':'GET'))return new Response(null,{status:405});
    let scope;try{const limit=peerLimits(sharedRuntime()).Exchange,auth=request.headers.get('Codeaf-Cluster-Auth')??'';if(auth.length>limit)return new Response('invalid peer request',{status:400});const raw=path.endsWith('/exchange')?new TextDecoder().decode(await readBody(request.clone(),limit,'too_large')):atob(auth.replace(/-/g,'+').replace(/_/g,'/'));scope=JSON.parse(raw).scope;}catch{return new Response('invalid peer request',{status:400});}
    if(!scope||typeof scope.identity!=='string'||!/^id_[a-f0-9]{32}$/.test(scope.identity))return new Response('member authorization refused',{status:403});
    return env.IDENTITY.get(env.IDENTITY.idFromName(scope.identity)).fetch(request);
  }
  const route = ['/v2/cluster/control','/v2/cluster/enroll'].includes(path)?{limit:clusterRequestLimit(sharedRuntime()),over:'too_large'}:new URL(request.url).pathname==='/v2/cluster/api' ? {limit:clusterRequestLimit(sharedRuntime()),over:"too_large"} : matchRoute(request.method, new URL(request.url).pathname);
  checkDeclared(request, route.limit, route.over);
  let identity;try{identity=await identityOf(request.headers.get('codeaf-identity'));}catch(error){if(path.startsWith('/v2/cluster/')&&path!=='/v2/cluster/api')return new Response(path.endsWith('/control')?'this computer is not allowed to coordinate this cluster':'this computer is not allowed to join this cluster',{status:403});throw error;}
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
  if (pathname.startsWith('/v1/') || pathname.startsWith('/v2/cluster/')) return forward(request, env);
  throw notFound();
}
