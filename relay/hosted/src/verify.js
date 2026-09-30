// Port of internal/reqsign Verify over WebCrypto Ed25519 (native in Workers and node >= 20).
// Verification is two steps so the relay can route on the identity a cert proves, then check the body.
import { fromBase64Url, hex, sha256 } from './codec.js';

export const SKEW_MS = 5 * 60 * 1000;
const enc = new TextEncoder();
const CERT_LABEL = 'codeaf/device-cert/v1\n';
const REQ_LABEL = 'codeaf-req-v1\n';

export class Refusal extends Error {
  constructor(kind, why) {
    super(why);
    this.kind = kind; // 'unauthorized' | 'skew' | 'revoked'
  }
}
const unauthorized = (why) => new Refusal('unauthorized', why);

const fromB64Url = (s) => {
  const bytes = fromBase64Url(s ?? '');
  if (!bytes) throw unauthorized('unsigned');
  return bytes;
};
const fromHex = (s) => {
  if (!/^([0-9a-f]{2})*$/.test(s ?? '')) throw unauthorized('bad hex');
  return Uint8Array.from(s.match(/../g) ?? [], (h) => parseInt(h, 16));
};

const importKey = (raw) => crypto.subtle.importKey('raw', raw, { name: 'Ed25519' }, false, ['verify']);
const verifySig = async (key, sig, msg) => crypto.subtle.verify({ name: 'Ed25519' }, key, sig, msg);

// certBody is Go's json.Marshal of Cert with an empty sig: struct order V, device, created, sig.
const certBody = (c) => enc.encode(CERT_LABEL + JSON.stringify({ V: c.V, device: c.device, created: c.created, sig: '' }));

const idOf = async (key) => 'id_' + hex((await sha256(key)).subarray(0, 16));
const deviceId = async (cert) => 'dev_' + hex((await sha256(fromHex(cert.device))).subarray(0, 16));

function readCert(text) {
  try {
    return JSON.parse(new TextDecoder().decode(fromB64Url(text)));
  } catch (e) {
    throw e instanceof Refusal ? e : unauthorized('bad cert');
  }
}

async function requestMessage({ method, uri, time }, body) {
  return enc.encode(`${REQ_LABEL}${method}\n${uri}\n${time}\n${hex(await sha256(body))}`);
}

const withinSkew = (time, now) => /^-?\d+$/.test(time) && Math.abs(now - Number(time)) <= SKEW_MS;

/**
 * checkCert is the half of verification that needs no body: the headers are present and the
 * device cert verifies under the identity key. A caller that passes holds a cert the identity
 * signed, which is what lets the relay treat its request as work for that identity before
 * the body has arrived. It answers a Caller for checkRequest, or throws Refusal.
 */
export async function checkCert(headers) {
  const { identity, cert: certText, time, sig } = headers;
  if (!identity || !certText || !time || !sig) throw unauthorized('unsigned');
  const identityKey = fromB64Url(identity);
  if (identityKey.length !== 32) throw unauthorized('unsigned');
  const cert = readCert(certText);
  const devicePub = fromHex(cert.device);
  if (devicePub.length !== 32) throw unauthorized('bad cert');
  const certOk = await verifySig(await importKey(identityKey), fromHex(cert.sig), certBody(cert));
  if (!certOk) throw unauthorized('bad cert');
  return { devicePub, time, sig, identity: await idOf(identityKey), device: await deviceId(cert) };
}

/**
 * checkRequest is the other half: the device signature covers the method, the URI, the time
 * and the body, and the time must be near the relay's clock. It answers {identity, device}.
 */
export async function checkRequest(caller, req, body, nowMs) {
  const msg = await requestMessage({ method: req.method, uri: req.uri, time: caller.time }, body);
  if (!(await verifySig(await importKey(caller.devicePub), fromB64Url(caller.sig), msg))) throw unauthorized('bad signature');
  if (!withinSkew(caller.time, nowMs)) throw new Refusal('skew', 'clock');
  return { identity: caller.identity, device: caller.device };
}

/** verify checks a whole request {method, uri, headers} and its body, in Go's order. */
export async function verify(req, body, nowMs) {
  return checkRequest(await checkCert(req.headers), req, body, nowMs);
}

/** identityOf is the identity id a request header names, before anything is verified: the routing key. */
export async function identityOf(header) {
  const key = fromB64Url(header ?? '');
  if (key.length !== 32) throw unauthorized('unsigned');
  return idOf(key);
}
