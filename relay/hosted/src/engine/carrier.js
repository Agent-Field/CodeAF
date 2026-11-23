// The carrier's bytes, shared by the machine object and its tests: frames (mux.go), the registration verdict and the
// possession proof (handshake.go). Nothing here reads a payload.

/** PROTOCOL is the upgrade token both ends spell; an on-the-wire identifier, not product prose. */
export const PROTOCOL = 'aforge-relay/1'; // legacy-name
/** The headers of a registration: the name claimed and the machine's public key, base64 raw-url. */
export const HEADER_NAME = 'aforge-name'; // legacy-name: persisted on the wire.
export const HEADER_KEY = 'aforge-key'; // legacy-name: persisted on the wire.

const LABEL = new TextEncoder().encode('aforge relay registration v1'); // legacy-name

// The tuning of internal/relay/relay.go, kept in lockstep under the same names.
export const MAX_PAYLOAD = 1 << 17; // relay.MaxPayload
export const MAX_STREAMS = 16; // relay.MaxStreams
export const LIVE_STREAMS = 14; // relay.LiveStreams: how many streams a live dial may leave attached, whoever holds the rest
export const HEADER_DOOR = 'codeaf-door'; // relay.HeaderDoor
export const DOOR_LIVE = 'live'; // relay.DoorLive
export const DOOR_MOVE = 'move'; // relay.DoorMove
export const BREAKER_BYTES_PER_SECOND = 4 << 20; // relay.BreakerRate
export const BREAKER_WINDOW_MS = 5000; // relay.BreakerWindow
export const HANDSHAKE_MS = 20000; // relay.HandshakeWithin
export const PROBE_MS = 5000; // relay.ProbeWithin
export const DIALS_PER_MINUTE = 30; // relay.DialsPerMinute (and MACHINE_DIAL_LIMIT in wrangler.toml)
export const REGISTRATIONS_PER_MINUTE = 10; // relay.RegistrationsPerMinute (and MACHINE_REGISTER_LIMIT in wrangler.toml)

export const TOO_MANY_DIALS = 'too many connections from here — wait a minute';
export const TOO_MANY_REGISTRATIONS = 'too many registrations from here — wait a minute';
/** TOO_MANY_LIVE is a live dial turned away while the last streams are kept for moving a chat. */
export const TOO_MANY_LIVE = `that machine already has ${LIVE_STREAMS} live connections open; the last ${MAX_STREAMS - LIVE_STREAMS} are kept for moving a chat`;
/** The sentence a closed stream's socket carries when the breaker cut it, with close code 1008. */
export const BREAKER_REASON = 'sending faster than a live view does';

/** The rate limits count in fixed windows of this many seconds, which is also the longest a refused caller can be told to wait. */
export const RATE_PERIOD_SECONDS = 60;

/**
 * doorOf reads a dial's class from its header. ONLY THE TWO KNOWN CLASSES ARE CLASSES: anything else, an absent header
 * included, is no class, so an older client and a class a later build adds are held to the full MAX_STREAMS exactly as every
 * dial was before the header existed.
 */
export const doorOf = (request) => {
  const said = request.headers.get(HEADER_DOOR);
  return said === DOOR_LIVE || said === DOOR_MOVE ? said : '';
};

/** secondsToNextMinute is how long until the fixed per-minute window `now` (milliseconds) is in rolls over, never less than one. */
export const secondsToNextMinute = (now) => Math.max(1, Math.ceil((60000 - (now % 60000)) / 1000));

/** The two doors, beneath the deployment's base path. */
export const ENGINE_PATH = '/v1/engine';
export const DIAL_PREFIX = '/v1/dial/';

/**
 * plain is a refusal in the relay's own sentence, which the Go client passes on as it is. A rate limit's refusal also says
 * when to come back, in `retry-after`, so a client waits for the window and not for a guess.
 */
export const plain = (status, sentence, headers = {}) => new Response(sentence, {
  status,
  headers: { 'content-type': 'text/plain; charset=utf-8', ...headers },
});
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
