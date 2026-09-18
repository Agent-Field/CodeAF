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

  // Keep only the rows the relay has not stored: a row's nonce is its
  // identity, and the client re-sends every row that did not get a 202, so an
  // install's retry is the ordinary case rather than an error. A nonce
  // repeated inside one batch is collapsed here too, by the set, before the
  // store is consulted, and the rows are grouped by day because a day's nonces
  // live together under one key.
  //
  // Per batch this costs a KV read and a KV write for each distinct (day,
  // cell) key it folds into, one read for every day the batch carries (that
  // day's seen set) and one read and one write for every day it has fresh rows
  // on (that day's seen set and its quota counter). A batch of 200 rows over
  // ten cells on one day is 12 writes and 12 reads, not 200 of each.
  const seenInBatch = new Set();
  const byDay = new Map();
  for (const row of rows) {
    if (seenInBatch.has(row.nonce)) {
      continue;
    }
    seenInBatch.add(row.nonce);
    const list = byDay.get(row.day) || [];
    list.push(row);
    byDay.set(row.day, list);
  }

  // A day's seen set is one KV value, so a quota raised past what one value
  // holds would silently drop nonces: refuse rather than lose them.
  const limit = intVar(env, 'ROWS_PER_INSTALL_PER_DAY', 500);
  if (limit * SEEN_BYTES_PER_NONCE > MAX_VALUE_BYTES) {
    return json({ error: 'ROWS_PER_INSTALL_PER_DAY is larger than one seen key holds' }, 500);
  }

  // One get per day names the nonces already folded, so freshness costs one
  // read per day rather than one per row.
  const storedSeen = new Map();
  const freshByDay = new Map();
  for (const [day, list] of byDay) {
    const seen = parseSeen(await env.POOL.get(seenKey(install, day)));
    storedSeen.set(day, seen);
    const fresh = list.filter((row) => !seen.has(row.nonce));
    if (fresh.length > 0) {
      freshByDay.set(day, fresh);
    }
  }

  // The per-install per-day quota is charged for the rows this batch actually
  // folds, so a retried row is not charged a second time.
  const charged = new Map();
  for (const [day, list] of freshByDay) {
    const current = parseInt(await env.POOL.get(quotaKey(install, day)), 10) || 0;
    if (current + list.length > limit) {
      return json({ error: 'daily quota exceeded' }, 429);
    }
    charged.set(day, current + list.length);
  }

  // Fold every fresh row into its install's running total for the cell and day,
  // grouping the rows that share a sheet key so each key is read once and
  // written once and the rows on it are folded in memory rather than by
  // read-after-write, one row at a time.
  let accepted = 0;
  const byKey = new Map();
  for (const list of freshByDay.values()) {
    for (const row of list) {
      const key = `sheet/${install}/${row.day}/${cellKey(row.payload)}`;
      const group = byKey.get(key) || [];
      group.push(row);
      byKey.set(key, group);
    }
  }
  for (const [key, group] of byKey) {
    const previous = await env.POOL.get(key);
    let triple = null;
    if (previous !== null) {
      try {
        triple = JSON.parse(previous);
      } catch {
        triple = null;
      }
    }
    for (const row of group) {
      triple = fold(triple, row.payload.score);
    }
    await env.POOL.put(key, JSON.stringify(triple));
    accepted += group.length;
  }

  // Record the nonces just folded and charge the days they were folded on: one
  // put of the day's seen set and one of its quota counter, per day.
  for (const [day, list] of freshByDay) {
    const seen = storedSeen.get(day);
    for (const row of list) {
      seen.add(row.nonce);
    }
    await env.POOL.put(seenKey(install, day), joinSeen(seen), { expirationTtl: SEEN_TTL });
    await env.POOL.put(quotaKey(install, day), String(charged.get(day)));
  }
  // A batch that was entirely already stored still answers 202: the client
  // needs only the 202 to mark its rows sent, and giving it anything else
  // would leave the outbox retrying rows the relay already holds. `accepted`
  // counts the rows folded now, so it is 0 for such a batch.
  return json({ accepted }, 202);
}

// quotaKey names one install's per-day quota counter.
function quotaKey(install, day) {
  return `quota/${install}/${day}`;
}

// A day's seen set lives under seen/<install>/<day>: the nonces already folded
// for that install and day, joined by newlines, with a one-week TTL. It is per
// day and not per batch because the client re-groups its rows on retry — a
// retry carries the rows that did not get a 202, which may be fewer than the
// batch that first sent them — so only the nonce each row carries survives,
// and the day's set of them is what a retry is checked against: one get and
// one put per day rather than one per row. At 32 hex characters per nonce plus
// a separator that is 33 bytes, the relay's own ROWS_PER_INSTALL_PER_DAY (500
// by default) bounds the value at 16,500 bytes, under KV's 25 MiB value limit.
// Idempotence is only as strong as KV — reads and writes are eventually
// consistent, so two concurrent copies of a batch can still both see a nonce
// absent and fold it twice — and two batches from one install on one day now
// race on this single key, where the last put wins and a retry may re-fold;
// within what KV answers, a nonce already stored is neither folded again nor
// charged again. The TTL is a week: the outbox retries within days, so a week
// outlasts any retry still in flight and the store does not grow without
// bound.
const SEEN_TTL = 7 * 24 * 60 * 60;
const SEEN_BYTES_PER_NONCE = 33; // 32 hex characters plus a separator
const MAX_VALUE_BYTES = 25 << 20; // KV's per-value limit
function seenKey(install, day) {
  return `seen/${install}/${day}`;
}

// parseSeen reads one day's seen set; a key the store has not written yet is
// an empty set.
function parseSeen(value) {
  return value === null ? new Set() : new Set(value.split('\n').filter((nonce) => nonce !== ''));
}

// joinSeen writes one day's seen set back as the value its key holds.
function joinSeen(seen) {
  return [...seen].join('\n');
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
