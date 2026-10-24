// The pure parts of link pairing: the short code, the check digits, the shape of a create body and of
// an approve body. Nothing here touches storage, so every rule is tested without a relay.
import { fromBase64Url, hex, sha256 } from '../codec.js';
import { badRequest } from '../wire.js';
import { tooBig } from './refusals.js';

export const CODE_ALPHABET = '0123456789abcdefghjkmnpqrstvwxyz'; // Crockford base32: no i, l, o, u
const CODE_PATTERN = /^[0-9a-hjkmnp-tv-z]{8}$/;
const PLATFORMS = new Set(['darwin', 'linux', 'windows', 'ios', 'android', 'other']);
const KEY_BYTES = 32;
const NONCE_BYTES = 24; // NaCl secretbox
const MAC_BYTES = 16;
const MAX_NAME_BYTES = 96;

/** newCode draws 40 random bits as 8 characters. */
export function newCode() {
  const bytes = crypto.getRandomValues(new Uint8Array(5));
  let bits = bytes.reduce((n, b) => (n << 8n) | BigInt(b), 0n);
  let out = '';
  for (let i = 0; i < 8; i++, bits >>= 5n) out = CODE_ALPHABET[Number(bits & 31n)] + out;
  return out;
}

/** codeOf is the canonical (lower case) form of a code text, or null when it is not a code. */
export function codeOf(text) {
  const code = String(text ?? '').toLowerCase();
  return CODE_PATTERN.test(code) ? code : null;
}

/** platformOf keeps a known platform and calls every other value "other". */
export const platformOf = (value) => (PLATFORMS.has(value) ? value : 'other');

/** checkDigits are four decimal digits of the public key's hash: uint16_be(sha256(pubkey)[0:2]) mod 10000. */
export async function checkDigits(pubkey) {
  const head = (await sha256(pubkey)).subarray(0, 2);
  return String(((head[0] << 8) | head[1]) % 10_000).padStart(4, '0');
}

/** deviceIdOf is the id the relay knows a device by, from its public key, as verify.js derives it. */
export async function deviceIdOf(pubkey) {
  return 'dev_' + hex((await sha256(pubkey)).subarray(0, 16));
}

function bytesOf(text, ok) {
  const bytes = typeof text === 'string' ? fromBase64Url(text) : null;
  if (!bytes || !ok(bytes.length)) throw badRequest();
  return bytes;
}

const isKey = (n) => n === KEY_BYTES;
// A sealed name is a nonce, the secretbox mac and at most MAX_NAME_BYTES of name.
const isSealedName = (n) => n >= NONCE_BYTES + MAC_BYTES && n <= NONCE_BYTES + MAC_BYTES + MAX_NAME_BYTES;

/**
 * newRequest checks a create body and answers the part of the record the caller chose, plus the two
 * values the relay derives from the key: the device id and the check digits. A device the body names
 * must be the one the key makes.
 */
export async function newRequest(body) {
  const pubkey = bytesOf(body.pubkey, isKey);
  bytesOf(body.x25519, isKey);
  bytesOf(body.name_sealed, isSealedName);
  const device = await deviceIdOf(pubkey);
  if (body.device !== undefined && body.device !== device) throw badRequest();
  return {
    device,
    pubkey: body.pubkey,
    x25519: body.x25519,
    name_sealed: body.name_sealed,
    platform: platformOf(body.platform),
    check: await checkDigits(pubkey),
  };
}

/** asGrant checks the sealed grant of an approve body: base64url text of at most `max` bytes. */
export function asGrant(text, max) {
  const bytes = typeof text === 'string' ? fromBase64Url(text) : null;
  if (!bytes || bytes.length === 0) throw badRequest();
  if (bytes.length > max) throw tooBig();
  return text;
}
