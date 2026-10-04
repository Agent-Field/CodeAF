// A signed HTTP client for the end-to-end scripts, which run against a live `wrangler dev`.
import http from 'node:http';
import { signed } from './party.js';

export const BASE = process.env.RELAY ?? 'http://127.0.0.1:18791';
/** TIGHT is a relay started with short limits, for the caps and pairing scripts. */
export const TIGHT = process.env.RELAY_TIGHT ?? 'http://127.0.0.1:18792';
const enc = new TextEncoder();
const dec = new TextDecoder();

/** target is the path a client signs: the base URL's own path (a deployment's prefix) and the route. */
export const target = (base, path) => new URL(base).pathname.replace(/\/+$/, '') + path;

const bytesOf = (body) => (body === undefined ? new Uint8Array(0) : body instanceof Uint8Array ? body : enc.encode(JSON.stringify(body)));

/** call sends one request signed by dev and answers {status, headers, buf, json}. */
export async function call(dev, method, path, body, { shiftMs = 0, base = BASE } = {}) {
  const bytes = bytesOf(body);
  const res = await fetch(base + path, { method, headers: await signed(dev, method, target(base, path), bytes, shiftMs), body: method === 'GET' ? undefined : bytes });
  const buf = new Uint8Array(await res.arrayBuffer());
  const text = dec.decode(buf);
  return { status: res.status, headers: res.headers, buf, json: text.startsWith('{') || text.startsWith('[') ? JSON.parse(text) : null };
}

/** answerOf is the one word a test compares: the refusal code, or the status when it succeeded. */
export const answerOf = (r) => r.json?.err ?? r.status;

/**
 * trickle sends a body in two halves, pauseMs apart, and answers the status: a put that is
 * still arriving. `headers` are already signed over the whole body.
 */
export function trickle(headers, path, bytes, pauseMs, base = BASE) {
  const url = new URL(base);
  const half = bytes.length >> 1;
  return new Promise((done, fail) => {
    const req = http.request({ host: url.hostname, port: url.port, method: 'POST', path, headers: { ...headers, 'content-length': bytes.length } }, (res) => (res.resume(), done(res.statusCode)));
    req.on('error', fail);
    req.write(bytes.subarray(0, half));
    setTimeout(() => req.end(bytes.subarray(half)), pauseMs);
  });
}
