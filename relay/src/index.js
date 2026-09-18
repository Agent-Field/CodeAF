// index.js builds the signed index document and signs it.
//
// The document is what internal/pool/index.Parse reads (see
// internal/pool/index/seed.json for a complete example). Keys sit in a fixed
// order and the text is JSON with no spaces, so the same cells always render
// the same bytes.
//
// signIndex signs under Ed25519 through WebCrypto, which is the same API
// under Workers and under node ≥ 20 through globalThis.crypto.subtle.

const SCHEMA = 1;

// buildIndex renders the document for cells. The keys are written in the fixed
// order the reader expects; JSON.stringify keeps the order of object keys.
export function buildIndex(cells, { version, generated, minInstalls, judges }) {
  const doc = {
    schema: SCHEMA,
    version,
    generated,
    min_installs: minInstalls,
    judges,
    rubrics: { role_quality: 1, acceptable: 1 },
    aliases: {},
    metrics: {
      role_quality: { kind: 'gaussian', unit: 'score', dims: ['role', 'model'] },
      acceptable: { kind: 'bernoulli', unit: 'share', dims: ['role', 'model', 'source'] },
    },
    cells: cells.map((c) => {
      const cell = {
        metric: c.metric,
        role: c.role,
        model: c.model,
      };
      if (c.source !== undefined) {
        // The reader folds a source label without its vendor: the wire says
        // codeaf/grader, the document says grader.
        cell.source = c.source.slice(c.source.lastIndexOf('/') + 1);
      }
      cell.mean = c.mean;
      cell.sd = c.sd;
      cell.n = c.n;
      cell.installs = c.installs;
      return cell;
    }),
  };
  return JSON.stringify(doc);
}

// signIndex signs docBytes and answers the detached signature as standard
// base64. seedB64 is the 32-byte private seed and publicB64 the 32-byte public
// key, both standard base64; together they import as one Ed25519 JWK.
export async function signIndex(docBytes, seedB64, publicB64) {
  const seed = fromBase64(seedB64);
  const pub = fromBase64(publicB64);
  const key = await crypto.subtle.importKey(
    'jwk',
    {
      kty: 'OKP',
      crv: 'Ed25519',
      d: toBase64Url(seed),
      x: toBase64Url(pub),
    },
    { name: 'Ed25519' },
    false,
    ['sign'],
  );
  const bytes = typeof docBytes === 'string' ? new TextEncoder().encode(docBytes) : docBytes;
  const signature = await crypto.subtle.sign({ name: 'Ed25519' }, key, bytes);
  return toBase64(new Uint8Array(signature));
}

// fromBase64 decodes standard base64 text (whitespace around it ignored).
function fromBase64(text) {
  const raw = atob(String(text).trim());
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) {
    out[i] = raw.charCodeAt(i);
  }
  return out;
}

// toBase64 renders bytes as standard base64.
function toBase64(bytes) {
  let s = '';
  for (const b of bytes) {
    s += String.fromCharCode(b);
  }
  return btoa(s);
}

// toBase64Url renders bytes as base64url, which a JWK field wants.
function toBase64Url(bytes) {
  return toBase64(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
