// The carrier's bytes, shared by the machine object and its tests: frames (mux.go), the registration verdict and the
// possession proof (handshake.go). Nothing here reads a payload.

/** PROTOCOL is the upgrade token both ends spell; an on-the-wire identifier, not product prose. */
export const PROTOCOL = 'aforge-relay/1'; // legacy-name
/** The headers of a registration: the name claimed and the machine's public key, base64 raw-url. */
export const HEADER_NAME = 'aforge-name'; // legacy-name: persisted on the wire.
export const HEADER_KEY = 'aforge-key'; // legacy-name: persisted on the wire.

const LABEL = new TextEncoder().encode('aforge relay registration v1'); // legacy-name

// The tuning of relay.go, in the same names.
export const MAX_PAYLOAD = 1 << 17;
export const MAX_STREAMS = 16;
export const DIALS_PER_MINUTE = 30;
export const REGISTRATIONS_PER_MINUTE = 10;

/** The two doors, beneath the deployment's base path. */
export const ENGINE_PATH = '/v1/engine';
export const DIAL_PREFIX = '/v1/dial/';

/** plain is a refusal in the relay's own sentence, which the Go client passes on as it is. */
export const plain = (status, sentence) => new Response(sentence, { status, headers: { 'content-type': 'text/plain; charset=utf-8' } });
const FRAME_HEADER = 13;

export const OP = { open: 1, data: 2, close: 3, ping: 4, pong: 5 };
const VERDICT_READY = 1;
const VERDICT_REFUSED = 2;


/** frame is one carrier frame: an op byte, an 8-byte stream id, a 4-byte length, the payload. */
export function frame(op, stream, payload = new Uint8Array(0)) {
  const out = new Uint8Array(FRAME_HEADER + payload.length);
  const view = new DataView(out.buffer);
  out[0] = op;
  view.setBigUint64(1, BigInt(stream));
  view.setUint32(9, payload.length);
  out.set(payload, FRAME_HEADER);
  return out;
}

/** readFrame parses one message as one frame, or answers null for a message that is not exactly one. */
export function readFrame(bytes) {
  if (bytes.length < FRAME_HEADER) return null;
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const length = view.getUint32(9);
  if (length > MAX_PAYLOAD || bytes.length !== FRAME_HEADER + length) return null;
  return { op: bytes[0], stream: view.getBigUint64(1).toString(), payload: bytes.subarray(FRAME_HEADER) };
}

/** verdict is the relay's last word on a registration: ready, or refused with a sentence. */
export function verdict(reason = '') {
  if (!reason) return new Uint8Array([VERDICT_READY, 0]);
  const said = new TextEncoder().encode(reason).subarray(0, 255);
  const out = new Uint8Array(2 + said.length);
  out[0] = VERDICT_REFUSED;
  out[1] = said.length;
  out.set(said, 2);
  return out;
}

/** registrationProof is the MAC the machine must answer a challenge with (handshake.go's registrationProof). */
export async function registrationProof(shared, nonce, name, publicKey) {
  const ikm = await crypto.subtle.importKey('raw', shared, 'HKDF', false, ['deriveBits']);
  const key = await crypto.subtle.deriveBits({ name: 'HKDF', hash: 'SHA-256', salt: nonce, info: LABEL }, ikm, 256);
  const mac = await crypto.subtle.importKey('raw', key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  const body = new Uint8Array([...new TextEncoder().encode(name), ...publicKey]);
  return new Uint8Array(await crypto.subtle.sign('HMAC', mac, body));
}

/** challenge mints the relay's half of the proof: the 64 bytes it sends, and the proof it then expects. */
export async function challenge(name, publicKey) {
  const ephemeral = await crypto.subtle.generateKey({ name: 'X25519' }, true, ['deriveBits']);
  const offered = await crypto.subtle.importKey('raw', publicKey, { name: 'X25519' }, false, []);
  const shared = new Uint8Array(await crypto.subtle.deriveBits({ name: 'X25519', public: offered }, ephemeral.privateKey, 256));
  const nonce = crypto.getRandomValues(new Uint8Array(32));
  const sent = new Uint8Array(64);
  sent.set(new Uint8Array(await crypto.subtle.exportKey('raw', ephemeral.publicKey)));
  sent.set(nonce, 32);
  return { sent, want: await registrationProof(shared, nonce, name, publicKey) };
}
