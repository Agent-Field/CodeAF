import { getPlatformProxy } from 'wrangler';

/** A real R2 bucket from workerd's local simulator, the engine `wrangler dev --local` uses. */
export async function openBucket() {
  const proxy = await getPlatformProxy({ configPath: new URL('./wrangler.toml', import.meta.url).pathname, persist: false });
  return { bucket: proxy.env.FRAMES, close: () => proxy.dispose() };
}

const hex = (n, c) => c.repeat(n);
export const rid = (i) => i.toString(16).padStart(64, '0');

/** frame builds a valid frame of single-byte-tagged objects: AGEO 0x01 then the given text. */
export function frame(rids, text = 'x') {
  const objs = rids.map((r) => Uint8Array.from([0x41, 0x47, 0x45, 0x4f, 0x01, ...new TextEncoder().encode(text + r.slice(-4))]));
  let off = 0;
  const refs = objs.map((o, i) => ({ rid: rids[i], off: (off += o.length) - o.length, len: o.length }));
  const header = new TextEncoder().encode(JSON.stringify({ V: 1, cell_key_id: hex(32, 'a'), objects: refs }));
  const out = new Uint8Array(9 + header.length + off);
  out.set([0x41, 0x47, 0x45, 0x46, 0x01]);
  new DataView(out.buffer).setUint32(5, header.length, true);
  out.set(header, 9);
  let at = 9 + header.length;
  for (const o of objs) (out.set(o, at), (at += o.length));
  return { bytes: out, objs };
}

export const init = (head = 'h0') => ({ head, class: 'work', size: 0, keys: { '': { id_x: '' } } });

/** countingBucket answers the bucket and a tally of the calls made on it, by method name. */
export function countingBucket(bucket) {
  const calls = {};
  const counted = new Proxy(bucket, {
    get: (target, name) => (typeof target[name] === 'function' ? (...a) => ((calls[name] = (calls[name] ?? 0) + 1), target[name](...a)) : target[name]),
  });
  return { bucket: counted, calls };
}
