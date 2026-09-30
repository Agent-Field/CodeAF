// A JS device for the local end-to-end run: an identity key, a device key, its cert, and request signing.
const enc = new TextEncoder();
const hex = (b) => [...new Uint8Array(b)].map((x) => x.toString(16).padStart(2, '0')).join('');
const b64u = (b) => Buffer.from(b).toString('base64url');
const raw = async (key) => new Uint8Array(await crypto.subtle.exportKey('raw', key));
const genKey = () => crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']);
const sign = async (key, msg) => new Uint8Array(await crypto.subtle.sign({ name: 'Ed25519' }, key, msg));
const sha = async (bytes) => hex(await crypto.subtle.digest('SHA-256', bytes));

export async function newIdentity() {
  const pair = await genKey();
  return { pair, pub: await raw(pair.publicKey) };
}

export async function newDevice(identity) {
  const pair = await genKey();
  const cert = { V: 1, device: hex(await raw(pair.publicKey)), created: Date.now(), sig: '' };
  const body = enc.encode('codeaf/device-cert/v1\n' + JSON.stringify(cert));
  cert.sig = hex(await sign(identity.pair.privateKey, body));
  return { identity, pair, cert };
}

/** headers for a request signed by device; body is a Uint8Array; shiftMs moves the signing clock. */
export async function signed(device, method, uri, body = new Uint8Array(0), shiftMs = 0) {
  const time = String(Date.now() + shiftMs);
  const msg = enc.encode(`codeaf-req-v1\n${method}\n${uri}\n${time}\n${await sha(body)}`);
  return {
    'codeaf-identity': b64u(device.identity.pub),
    'codeaf-cert': b64u(enc.encode(JSON.stringify(device.cert))),
    'codeaf-time': time,
    'codeaf-sig': b64u(await sign(device.pair.privateKey, msg)),
  };
}

/** deviceId is the id the relay knows a device by: dev_ and the first 16 bytes of the hash of its key, in hex. */
export async function deviceId(device) {
  return 'dev_' + (await sha(Buffer.from(device.cert.device, 'hex'))).slice(0, 32);
}
