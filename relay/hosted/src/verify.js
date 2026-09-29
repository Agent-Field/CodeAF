// Port of internal/reqsign Verify over WebCrypto Ed25519 (native in Workers and node >= 20).
export const SKEW_MS = 5 * 60 * 1000;
const enc = new TextEncoder();
const CERT_LABEL = 'codeaf/device-cert/v1\n';
const REQ_LABEL = 'codeaf-req-v1\n';

export class Refusal extends Error {
  constructor(kind, why) {
    super(why);
    this.kind = kind; // 'unauthorized' | 'skew'
  }
}
const unauthorized = (why) => new Refusal('unauthorized', why);

const fromB64Url = (s) => {
  if (!/^[A-Za-z0-9_-]*$/.test(s ?? '')) throw unauthorized('unsigned');
  return Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
};
const fromHex = (s) => {
  if (!/^([0-9a-f]{2})*$/.test(s ?? '')) throw unauthorized('bad hex');
  return Uint8Array.from(s.match(/../g) ?? [], (h) => parseInt(h, 16));
};
const hex = (bytes) => [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
const sha256 = async (bytes) => new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));

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
 * verify checks headers {identity, cert, time, sig} of a request {method, uri}
 * and its body. It answers {identity, device} or throws Refusal.
 */
export async function verify(req, body, nowMs) {
  const { identity, cert: certText, time, sig } = req.headers;
  if (!identity || !certText || !time || !sig) throw unauthorized('unsigned');
  const identityKey = fromB64Url(identity);
  if (identityKey.length !== 32) throw unauthorized('unsigned');
  const cert = readCert(certText);
  const devicePub = fromHex(cert.device);
  if (devicePub.length !== 32) throw unauthorized('bad cert');
  const certOk = await verifySig(await importKey(identityKey), fromHex(cert.sig), certBody(cert));
  if (!certOk) throw unauthorized('bad cert');
  const msg = await requestMessage({ method: req.method, uri: req.uri, time }, body);
  if (!(await verifySig(await importKey(devicePub), fromB64Url(sig), msg))) throw unauthorized('bad signature');
  if (!withinSkew(time, nowMs)) throw new Refusal('skew', 'clock');
  return { identity: await idOf(identityKey), device: await deviceId(cert) };
}
