// Storage permission is separate from directory authority, so disabling uploads
// leaves pairing, live connections and previously stored frames available.
import { Wire, json, unwrap } from './wire.js';
import { readBody } from './body.js';
import { hex, sha256, fromBase64Url } from './codec.js';

export const ADMIN_DOMAIN = 'codeaf/relay-admin/v1\n';
export const ADMIN_WINDOW_MS = 10 * 60_000;
export const isIdentity = value => typeof value === 'string' && /^id_[0-9a-f]{32}$/.test(value);
const enc = new TextEncoder();
const approvalKey = 'storage-approval';

export class StorageAccess {
  constructor(meta, limits) { this.meta = meta; this.limits = limits; }
  approval() { return this.meta.get(approvalKey); }
  state() { return this.limits.storageOpen || this.approval()?.storage === 'on' ? 'on' : 'off'; }
  assert() { if (this.state() !== 'on') throw new Wire('storage_off', 403); }
  bindSuccessor(successor) {
    const old = this.meta.get('storage-successor');
    if (old && old !== successor) throw new Wire('conflict', 409);
    if (!old) this.meta.set('storage-successor', successor);
  }
  set(approval, inherited = false) {
    const old = this.approval();
    if (inherited && (this.limits.storageOpen || approval.storage !== 'on' || old)) return;
    if (old && approval.at <= old.at) {
      if (inherited) return;
      throw new Wire('stale', 409);
    }
    this.meta.set(approvalKey, { storage: approval.storage, at: approval.at });
  }
}

// The successor proves its own key and binds consent to this source identity.
export async function checkStorageHandoff(source, raw) {
  let body;
  try { body = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw)); }
  catch { throw new Wire('bad_request', 400); }
  if (!body || !isIdentity(body.successor) || body.successor === source ||
      typeof body.successor_key !== 'string' || !/^[0-9a-f]{64}$/.test(body.successor_key) ||
      typeof body.proof !== 'string' || !/^[0-9a-f]{128}$/.test(body.proof)) throw new Wire('bad_request', 400);
  const fromHex = text => Uint8Array.from(text.match(/../g), h => parseInt(h, 16));
  const bytes = fromHex(body.successor_key);
  if ('id_' + hex((await sha256(bytes)).subarray(0, 16)) !== body.successor) throw new Wire('bad_request', 400);
  let accepted = false;
  try {
    const key = await crypto.subtle.importKey('raw', bytes, 'Ed25519', false, ['verify']);
    accepted = await crypto.subtle.verify('Ed25519', key, fromHex(body.proof),
      enc.encode(`codeaf/storage-handoff/v1\n${source}\n${body.successor}`));
  } catch { /* An invalid successor key cannot authorize a handoff. */ }
  if (!accepted) throw new Wire('bad_request', 400);
  return body.successor;
}

// Verify the exact bytes before crossing the identity boundary. Neither a
// device certificate nor newcomer admission participates in operator approval.
export async function serveStorageAdmin(request, env, clock = Date.now) {
  if (request.method !== 'POST') throw new Wire('not_found', 404);
  let raw;
  try { raw = await readBody(request, 4096, 'bad_request'); }
  catch { throw new Wire('bad_request', 400); }
  let body;
  try { body = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw)); }
  catch { throw new Wire('bad_request', 400); }
  if (!body || body.v !== 1 || !isIdentity(body.identity) ||
      !['on', 'off'].includes(body.storage) || typeof body.host !== 'string' ||
      !Number.isSafeInteger(body.at) || typeof body.nonce !== 'string' || !body.nonce.trim() || body.nonce.length > 256) {
    throw new Wire('bad_request', 400);
  }
  const signatureText = request.headers.get('codeaf-admin-signature') ?? '';
  const signature = /^[A-Za-z0-9_-]+$/.test(signatureText) && fromBase64Url(signatureText);
  const message = new Uint8Array(enc.encode(ADMIN_DOMAIN).length + raw.length);
  message.set(enc.encode(ADMIN_DOMAIN)); message.set(raw, enc.encode(ADMIN_DOMAIN).length);
  let accepted = false;
  if (signature?.length === 64) for (const text of (env.CAF_ADMIN_KEYS ?? '').split(',')) {
    const keyText = text.trim();
    if (!/^[A-Za-z0-9_-]{43}$/.test(keyText)) continue;
    const bytes = fromBase64Url(keyText);
    if (bytes?.length !== 32) continue;
    try {
      const key = await crypto.subtle.importKey('raw', bytes, 'Ed25519', false, ['verify']);
      if (await crypto.subtle.verify('Ed25519', key, signature, message)) { accepted = true; break; }
    } catch { /* An invalid configured key cannot authorize an approval. */ }
  }
  if (!accepted || body.host !== new URL(request.url).host || Math.abs(clock() - body.at) > ADMIN_WINDOW_MS) {
    throw new Wire('unauthorized', 401);
  }
  const target = env.IDENTITY.get(env.IDENTITY.idFromName(body.identity));
  unwrap(await target.setStorage(body));
  return json({ identity: body.identity, storage: body.storage });
}
