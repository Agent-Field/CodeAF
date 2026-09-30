// Port of internal/blobstore frame.go: one function per rule, same order.
export const MAX_FRAME = 16 << 20;
export const MAX_HEADER = 1 << 20;
const MAGIC = Uint8Array.of(0x41, 0x47, 0x45, 0x46, 0x01); // "AGEF" 0x01
const OBJECT_MAGICS = [Uint8Array.of(0x41, 0x47, 0x45, 0x4f, 0x01), Uint8Array.of(0x41, 0x47, 0x45, 0x56, 0x01)];
export const PREFIX = MAGIC.length + 4;
const HEX = /^[0-9a-f]+$/;

export class BadFrame extends Error {}
const bad = (why) => new BadFrame(why);

const startsWith = (bytes, prefix) => bytes.length >= prefix.length && prefix.every((b, i) => bytes[i] === b);
const isLowerHex = (s, n) => typeof s === 'string' && s.length === n && HEX.test(s);
const isUint = (n, max) => Number.isSafeInteger(n) && n >= 0 && n <= max;

function split(frame) {
  if (frame.length < PREFIX || !startsWith(frame, MAGIC)) throw bad('wrong magic');
  const n = new DataView(frame.buffer, frame.byteOffset).getUint32(MAGIC.length, true);
  if (n > MAX_HEADER) throw bad('header is larger than the limit');
  if (PREFIX + n > frame.length) throw bad('header runs past the frame');
  return [frame.subarray(PREFIX, PREFIX + n), frame.subarray(PREFIX + n)];
}

export function parseHeader(raw) {
  let h;
  try {
    h = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw));
  } catch {
    throw bad('header is not valid JSON');
  }
  if (h === null || typeof h !== 'object' || h.V !== 1) throw bad('unknown header version');
  if (!isLowerHex(h.cell_key_id, 32)) throw bad('cell key id is not 32 lowercase hex');
  if (!Array.isArray(h.objects) || h.objects.length === 0) throw bad('frame holds no object');
  return h;
}

function checkRef(ref, want, seen) {
  if (ref === null || typeof ref !== 'object' || !isLowerHex(ref.rid, 64)) throw bad('object id is not 64 lowercase hex');
  if (seen.has(ref.rid)) throw bad('object id appears twice');
  if (ref.off !== want) throw bad('objects are not contiguous');
  if (!isUint(ref.len, 0xffffffff)) throw bad('object length is not a u32');
}

function carve(refs, payload) {
  const objects = [];
  const seen = new Set();
  let next = 0;
  for (const ref of refs) {
    checkRef(ref, next, seen);
    const end = next + ref.len;
    if (end > payload.length) throw bad('object runs past the payload');
    const body = payload.subarray(next, end);
    if (!OBJECT_MAGICS.some((m) => startsWith(body, m))) throw bad('object is not sealed');
    seen.add(ref.rid);
    objects.push({ rid: ref.rid, off: ref.off, len: ref.len, bytes: body });
    next = end;
  }
  if (next !== payload.length) throw bad('payload holds bytes no object names');
  return objects;
}

/**
 * decode validates a frame and answers its cell key id, the payload's offset inside the frame
 * and its objects (views of frame, each with its offset in the payload and its length).
 */
export function decode(frame) {
  if (frame.length > MAX_FRAME) throw bad('frame is larger than the limit');
  const [raw, payload] = split(frame);
  const h = parseHeader(raw);
  return { cellKeyId: h.cell_key_id, base: frame.length - payload.length, objects: carve(h.objects, payload) };
}

/**
 * headerRefs reads only the front of a frame: the prefix and the JSON header.
 * It answers {need} while front is too short to hold the whole header, else
 * {base, refs}, where base is the payload's offset inside the frame.
 */
export function headerRefs(front) {
  if (front.length < PREFIX || !startsWith(front, MAGIC)) throw bad('wrong magic');
  const n = new DataView(front.buffer, front.byteOffset).getUint32(MAGIC.length, true);
  if (n > MAX_HEADER) throw bad('header is larger than the limit');
  if (PREFIX + n > front.length) return { need: PREFIX + n };
  return { base: PREFIX + n, refs: parseHeader(front.subarray(PREFIX, PREFIX + n)).objects };
}
