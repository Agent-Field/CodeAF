// Pure rules for showing a picture in the File view. No DOM, so they run under node --test.
import { extensionOf } from '../conversation/assets/paths.ts';

/**
 * The most a File view will decode: exactly the engine's own read cap (FetchFile, 16 << 20 bytes, see
 * docs/ENGINE.md "GET /files"). The engine refuses anything larger, so this is the same bound stated once
 * on the renderer; it is the only place the frontend names it.
 */
export const maxImageBytes = 16 * 1024 * 1024;

/**
 * Raster formats only. SVG is deliberately absent: it is markup that can carry script, so it stays
 * the plain binary line here (the preview sheet's own rules for SVG are unchanged).
 */
const rasterExt = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'ico']);
const rasterMime = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/avif', 'image/bmp', 'image/x-icon', 'image/vnd.microsoft.icon']);
const canonicalBase64 = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

/** Whether the File view should ask for this path as a picture rather than as text. */
export const isRasterImagePath = (path: string): boolean => rasterExt.has(extensionOf(path));

export type ImageVerdict = { ok: true; url: string } | { ok: false; reason: 'too-large' | 'unsafe' };

/**
 * Decides whether an engine answer may become an <img> source; the answer is untrusted data. The size must be
 * a finite non-negative integer (there is no guessed default: anything else is refused) and the payload must
 * be canonical base64, so nothing can break out of the data URL.
 */
export function imageVerdict(file: { mime: string; size: number; dataBase64: string }): ImageVerdict {
  if (!Number.isSafeInteger(file.size) || file.size < 0 || typeof file.mime !== 'string' || typeof file.dataBase64 !== 'string') return { ok: false, reason: 'unsafe' };
  const base64Cap = Math.ceil(maxImageBytes / 3) * 4;
  if (file.size > maxImageBytes || file.dataBase64.length > base64Cap) return { ok: false, reason: 'too-large' };
  const mime = file.mime.split(';')[0].trim().toLowerCase();
  if (!rasterMime.has(mime) || !canonicalBase64.test(file.dataBase64)) return { ok: false, reason: 'unsafe' };
  return { ok: true, url: `data:${mime};base64,${file.dataBase64}` };
}

const verdicts = new WeakMap<object, ImageVerdict>();

/** `imageVerdict` once per engine answer: a render re-reads the same multi-MiB payload several times otherwise. */
export function imageVerdictOf(file: { mime: string; size: number; dataBase64: string }): ImageVerdict {
  let verdict = verdicts.get(file);
  if (!verdict) verdicts.set(file, verdict = imageVerdict(file));
  return verdict;
}

/** What a tagged read may show for `key`: an answer read for any other key, or no key at all, is still loading. */
export function settleImage<T extends { key: string }>(key: string | null, state: { status: 'loading' } | { status: 'failed'; message: string } | { status: 'ready'; value: T }) {
  if (key === null) return { status: 'loading' } as const;
  if (state.status === 'ready' && state.value.key !== key) return { status: 'loading' } as const;
  return state;
}
