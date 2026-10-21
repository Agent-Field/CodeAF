// The base path a deployment is served under. A hostname that already carries other things
// (the hosted relay shares codeaf.agentfield.ai with other paths) mounts the relay under a prefix
// such as /fabric, and every route then lives beneath it. The prefix is one deployment variable,
// BASE_PATH, empty for a relay that owns its whole hostname.
//
// The client signs the path it sent, prefix included, so the prefix is removed from where a request
// is routed (the Worker does it once, before dispatch) and put back where a signature is checked.

/** prefixOf is BASE_PATH normalised to "" or "/word" with no trailing slash. */
export function prefixOf(env) {
  const raw = (env.BASE_PATH ?? '').trim().replace(/\/+$/, '');
  return raw === '' || raw.startsWith('/') ? raw : '/' + raw;
}

/** stripBase answers the request with the prefix removed from its path, or null when it is outside the prefix. */
export function stripBase(request, env) {
  const prefix = prefixOf(env);
  if (prefix === '') return request;
  const url = new URL(request.url);
  if (url.pathname !== prefix && !url.pathname.startsWith(prefix + '/')) return null;
  url.pathname = url.pathname.slice(prefix.length) || '/';
  return new Request(url, request);
}

/** signedUri is the request target the client signed: the prefix, the routed path and the query. */
export const signedUri = (env, url) => prefixOf(env) + url.pathname + url.search;
