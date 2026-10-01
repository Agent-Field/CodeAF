// The page a pairing link opens in a browser: /p/<code>#<key>. It is one static page, the same for
// any code, so it reveals nothing about the code and the relay looks nothing up. The key lives in
// the fragment, which a browser never sends; the inline script reads it on the person's own machine
// and makes no request, so the page cannot leak it. The CSP allows that one script and that one
// style by hash, and nothing else.
import { notFound } from './wire.js';

const STYLE = 'body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem}' +
  'code{display:block;margin:1rem 0;padding:.75rem;background:#eee;color:#111;overflow-wrap:anywhere}' +
  'button{font:inherit;padding:.5rem 1rem}@media(prefers-color-scheme:dark){body{background:#111;color:#eee}}';

// The link is code.key as `codeaf pair approve` takes it; the code is the last path part, the key the fragment.
// The app reads the key from the URL fragment only (internal/pair fromURL); a query key would arrive as no key.
export const DEEP_LINK = `const deepLink=(code,key)=>'codeaf://pair?code='+encodeURIComponent(code)+(key?'#'+key:'');`;

const SCRIPT = `${DEEP_LINK}
const code=location.pathname.split('/').pop();
const key=location.hash.slice(1);
const token=key?code+'.'+key:code;
document.getElementById('cmd').textContent='codeaf pair approve '+token;
document.getElementById('open').onclick=()=>{location.href=deepLink(code,key);};`;

const BODY = `<h1>Add this device</h1>
<p>Open this on your computer: run</p>
<code id="cmd">codeaf pair approve &lt;paste the link&gt;</code>
<p>Or, if CodeAF is installed here:</p>
<button id="open" type="button">Open in CodeAF</button>`;

const digest = async (text) => {
  const sum = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text)));
  return "'sha256-" + btoa(String.fromCharCode(...sum)) + "'";
};

let built;
/** pageParts answers the page's HTML and its CSP, built once. */
async function pageParts() {
  built ??= (async () => {
    const csp = `default-src 'none'; style-src ${await digest(STYLE)}; script-src ${await digest(SCRIPT)}; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`;
    const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Add this device</title><style>${STYLE}</style></head><body>${BODY}<script>${SCRIPT}</script></body></html>`;
    return { csp, html };
  })();
  return built;
}

const isPage = (request) => request.method === 'GET' && /^\/p\/[^/]+$/.test(new URL(request.url).pathname);

/** isPagePath is true for a request this module answers. */
export const isPagePath = (request) => /^\/p\//.test(new URL(request.url).pathname);

/** servePage answers a pairing link's page; any other /p/ request is a 404. */
export async function servePage(request) {
  if (!isPage(request)) throw notFound();
  const { csp, html } = await pageParts();
  return new Response(html, {
    headers: {
      'content-type': 'text/html; charset=utf-8',
      'content-security-policy': csp,
      'referrer-policy': 'no-referrer',
      'cache-control': 'no-store',
      'x-content-type-options': 'nosniff',
    },
  });
}
