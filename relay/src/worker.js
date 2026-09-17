// worker.js is the Model Pool relay: it accepts measurement rows from
// installs, folds each install's rows into per-install running totals, and
// once an hour publishes a signed, versioned index document and serves it.
//
// The wire is fixed by the Go client (internal/pool/outbox for submit,
// internal/pool/pull and internal/pool/index for the document), so this file
// matches it exactly and adds nothing of its own to it.
//
// It owns only its /pool/ prefix; every other path answers 404 with an empty
// body. It never logs a request and never reads or stores the client address.

import { validateInstall, validateRow } from './schema.js';
import { aggregate, cellKey, fold, judgesOf } from './sheet.js';
import { buildIndex, signIndex } from './index.js';

const PREFIX = '/pool';

// The submit limits the client already works within.
const MAX_BODY = 256 << 10; // 256 KiB
const MAX_LINES = 200;

// The KV keys the published document lives under.
const DOC_KEY = 'index/doc';
const SIG_KEY = 'index/sig';
const VERSION_KEY = 'index/version';

// The lock one reader holds while it republishes; it expires by itself so a
// reader that dies mid-publish does not wedge the rest.
const LOCK_KEY = 'index/publishing';

export default {
  // fetch serves the four routes under /pool/. The ctx carries waitUntil
  // when the platform passes one; reads never require it.
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const route = routeOf(url.pathname);
    switch (route) {
      case '/v1/rows':
        if (request.method === 'POST') {
          return handleRows(request, env);
        }
        return notFound();
      case '/index.json':
        await refreshInBackground(env, ctx);
        return serveStored(request, env, DOC_KEY, 'application/json');
      case '/index.json.sig':
        await refreshInBackground(env, ctx);
        return serveStored(request, env, SIG_KEY, 'text/plain');
      case '/healthz':
        if (request.method === 'GET' || request.method === 'HEAD') {
          return new Response(request.method === 'HEAD' ? null : 'ok', {
            status: 200,
            headers: { 'Content-Type': 'text/plain' },
          });
        }
        return notFound();
      default:
        return notFound();
    }
  },

  // scheduled folds every install's sheets into one document, signs it, and
  // replaces the published copy. A publication with no cells still publishes:
  // an empty index is a valid index.
  async scheduled(event, env) {
    await publish(env);
  },
};

// publish folds every install's sheets into one document, signs it, and
// replaces the published copy. The document's version is its publish time
// in unix seconds, which is what reads compare against PUBLISH_EVERY.
async function publish(env) {
    const minInstalls = intVar(env, 'MIN_INSTALLS', 3);
    const entries = await loadEntries(env);
    const cells = aggregate(entries, { minInstalls });
    const judges = judgesOf(entries);
    const version = Math.floor(Date.now() / 1000); // monotone by construction
    const generated = new Date().toISOString().slice(0, 10); // today, UTC
    const doc = buildIndex(cells, { version, generated, minInstalls, judges });
    const sig = await signIndex(doc, env.POOL_SIGNING_KEY, env.POOL_PUBLIC_KEY);
    await env.POOL.put(DOC_KEY, doc);
    await env.POOL.put(SIG_KEY, sig);
    await env.POOL.put(VERSION_KEY, String(version));
}

// routeOf answers the path under /pool/, or null when the request is not ours.
function routeOf(pathname) {
  if (pathname === PREFIX) {
    return '/';
  }
  if (pathname.startsWith(PREFIX + '/')) {
    return pathname.slice(PREFIX.length);
  }
  return null;
}

// notFound is the empty 404 every path outside a served route gets.
function notFound() {
  return new Response(null, { status: 404 });
}

// json answers one JSON object with a status.
function json(body, status) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// handleRows accepts one NDJSON batch from one install.
async function handleRows(request, env) {
  const install = request.headers.get('X-Codeaf-Install');
  if (!validateInstall(install)) {
    return json({ error: 'missing or bad X-Codeaf-Install' }, 400);
  }
  const body = await request.text();
  if (new TextEncoder().encode(body).length > MAX_BODY) {
    return json({ error: 'body is larger than 256 KiB' }, 413);
  }
  const lines = [];
  for (const [index, raw] of body.split('\n').entries()) {
    const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw;
    if (line.trim() !== '') {
      lines.push({ line, number: index + 1 });
    }
  }
  if (lines.length > MAX_LINES) {
    return json({ error: 'more than 200 lines' }, 413);
  }
  if (lines.length === 0) {
    return json({ error: 'no rows' }, 400);
  }
  const now = new Date();
  const rows = [];
  for (const { line, number } of lines) {
    const { row, error } = validateRow(line, now);
    if (error !== null) {
      return json({ error: `line ${number}: ${error}` }, 400);
    }
    rows.push(row);
  }

  // The per-install per-day quota is checked for the whole batch before
  // anything is stored: a batch that would exceed it stores nothing.
  const limit = intVar(env, 'ROWS_PER_INSTALL_PER_DAY', 500);
  const byDay = new Map();
  for (const row of rows) {
    const list = byDay.get(row.day) || [];
    list.push(row);
    byDay.set(row.day, list);
  }
  const used = new Map();
  for (const [day, list] of byDay) {
    const current = parseInt(await env.POOL.get(quotaKey(install, day)), 10) || 0;
    if (current + list.length > limit) {
      return json({ error: 'daily quota exceeded' }, 429);
    }
    used.set(day, current);
  }

  // Fold each score into its install's running total for the cell and day.
  for (const row of rows) {
    const key = `sheet/${install}/${row.day}/${cellKey(row.payload)}`;
    const previous = await env.POOL.get(key);
    let triple = null;
    if (previous !== null) {
      try {
        triple = JSON.parse(previous);
      } catch {
        triple = null;
      }
    }
    await env.POOL.put(key, JSON.stringify(fold(triple, row.payload.score)));
  }
  for (const [day, list] of byDay) {
    await env.POOL.put(quotaKey(install, day), String(used.get(day) + list.length));
  }
  return json({ accepted: rows.length }, 202);
}

// quotaKey names one install's per-day quota counter.
function quotaKey(install, day) {
  return `quota/${install}/${day}`;
}

// refreshInBackground republishes the document behind a read when the
// stored version is missing or older than PUBLISH_EVERY seconds. The read
// itself still answers what is stored, so callers never wait on a publish.
async function refreshInBackground(env, ctx) {
  if (!ctx || typeof ctx.waitUntil !== 'function') {
    return;
  }
  const every = intVar(env, 'PUBLISH_EVERY', 3600);
  const version = await env.POOL.get(VERSION_KEY);
  if (!isStale(version, every)) {
    return;
  }
  ctx.waitUntil(guardedPublish(env));
}

// isStale answers whether a stored version needs republishing. The version
// is the publish time in unix seconds, so staleness is its age in seconds;
// a missing or unreadable version is always stale.
function isStale(version, every) {
  const published = parseInt(version, 10);
  if (!Number.isFinite(published)) {
    return true;
  }
  return Math.floor(Date.now() / 1000) - published > every;
}

// guardedPublish publishes once per stale spell: a reader that finds the
// lock takes it and publishes, and the rest serve what is stored. The
// in-flight promise serialises readers on this isolate, where the check and
// the write would otherwise interleave; the KV lock covers the rest.
let inflight = null;

function guardedPublish(env) {
  if (inflight !== null) {
    return inflight;
  }
  inflight = (async () => {
    try {
      const held = await env.POOL.get(LOCK_KEY);
      if (held !== null) {
        return;
      }
      await env.POOL.put(LOCK_KEY, '1', { expirationTtl: 60 });
      await publish(env);
    } finally {
      inflight = null;
    }
  })();
  return inflight;
}

// serveStored answers one of the published KV values with its caching headers.
async function serveStored(request, env, key, contentType) {
  if (request.method !== 'GET' && request.method !== 'HEAD') {
    return notFound();
  }
  const value = await env.POOL.get(key);
  if (value === null) {
    return notFound();
  }
  const version = await env.POOL.get(VERSION_KEY);
  const etag = `"${version === null ? '' : version}"`;
  const headers = {
    'Content-Type': contentType,
    'Cache-Control': 'public, max-age=300',
    ETag: etag,
  };
  const ifNoneMatch = request.headers.get('If-None-Match');
  if (ifNoneMatch !== null && ifNoneMatch.trim() === etag) {
    return new Response(null, { status: 304, headers });
  }
  return new Response(request.method === 'HEAD' ? null : value, { status: 200, headers });
}

// loadEntries reads every sheet triple the store holds, one entry per install,
// cell and day.
async function loadEntries(env) {
  const entries = [];
  let cursor;
  do {
    const page = await env.POOL.list({ prefix: 'sheet/', cursor });
    for (const key of page.keys) {
      const parts = key.name.slice('sheet/'.length).split('/');
      const install = parts[0];
      const day = parts[1];
      const cell = parts.slice(2).join('/');
      const value = await env.POOL.get(key.name);
      if (value === null) {
        continue;
      }
      let triple;
      try {
        triple = JSON.parse(value);
      } catch {
        continue;
      }
      if (!triple || typeof triple.n !== 'number') {
        continue;
      }
      entries.push({ install, day, key: cell, triple });
    }
    cursor = page.list_complete ? null : page.cursor;
  } while (cursor);
  return entries;
}

// intVar reads an integer variable with a default when it is unset or blank.
function intVar(env, name, fallback) {
  const value = parseInt(env[name], 10);
  return Number.isFinite(value) ? value : fallback;
}
