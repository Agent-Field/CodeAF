import { encodeCanonical, decodeCanonical } from './codec.js';
import { hex, sha256, fromBase64Url } from '../codec.js';
import { unwrap, Wire } from '../wire.js';

const enc = new TextEncoder(), dec = new TextDecoder();
const segment = text => hex(enc.encode(text));
const digest = async value => hex(await sha256(enc.encode(encodeCanonical(value))));

// The host signs with a committed, restricted effect key and waits for actual
// commit/application. Neither this adapter nor a LinkRequest decides policy.
export class RaftEffectsFacade {
  constructor(host, cluster) { this.host = host; this.cluster = cluster; }
  async commit(change, now) {
    const result = await this.host.commitEffect(change, now);
    if (result.code !== 'ok') throw new Wire(result.code, result.code === 'rotated' ? 410 : 409);
  }
  async upload(store, receipt, durable, now) {
    receipt = structuredClone(receipt);
    if ((receipt.frames?.length ?? 0) > 512) throw new Wire('bad_request', 400);
    if (!receipt.frames?.length) delete receipt.frames;
    await store.checkReferences(receipt.head_rid, receipt.frames ?? [], durable);
    await this.commit({ record_upload: receipt }, now);
    return digest(receipt);
  }
  async admit(box, code, now) {
    code = String(code).trim().toLowerCase();
    const r = unwrap(await box.bind(this.cluster));
    if (r.code !== code) throw new Wire('bad_request',400);
    const key = fromBase64Url(r.pubkey);
    if (!key || key.length !== 32) throw new Wire('bad_request', 400);
    // Canonical byte slices use standard padded base64, not base64url.
    const public_key = btoa(String.fromCharCode(...key));
    await this.commit({ admit_join: { code, public_key, expires: r.expires_at, platform:r.platform } }, now);
  }
  async replay(boxOf, after, now) {
    const rows = await this.host.barrierOutbox(after, 64);
    if (rows.length > 64) throw new Wire('full', 503);
    for (const row of rows) {
      if (row.key <= after) throw new Wire('bad_request', 400);
      if (row.key.startsWith('join/')) {
        const request = decodeCanonical(dec.decode(row.data));
        if (request.decision && !request.outcome) await this.deliver(boxOf(request.decision.code), request.decision.code, now);
      }
      after = row.key;
    }
    return after;
  }
  async deliver(box, code, now) {
    code = String(code).trim().toLowerCase();
    const raw = await this.host.barrierRecord({ namespace: 'outbox', key: 'join/' + segment(code) });
    if (!raw) throw new Wire('request_gone', 404);
    const row = decodeCanonical(dec.decode(raw));
    if (!row.decision) throw new Wire('request_gone', 404);
    if (row.outcome) return;
    const id = await digest(row.decision), d = row.decision;
    let outcome = 'delivered';
    const reply = await box.deliver(this.cluster, id, d.deny ? 'denied' : 'approved', d.deny ? null : d.grant, row.device);
    try { unwrap(reply); } catch (e) { if (e.code !== 'gone') throw e; outcome = 'gone'; }
    await this.commit({ complete_join: { code, decision: id, outcome } }, now);
  }
}

// External writes recheck committed authority after I/O. Losing authorization
// never deletes an uploaded frame, and never supplies a successful receipt.
export class RaftBlobTenant {
  constructor(store, authorize, durable) { this.store = store; this.authorize = authorize; this.durable = durable; }
  async putFrame(frame, admit) {
    await this.authorize();
    const stored = await this.store.put(frame, admit);
    await this.durable();
    await this.authorize();
    return stored;
  }
}
