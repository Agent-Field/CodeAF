// Pure rules for showing a picture in the File view. No DOM, so they run under node --test.
import { extensionOf } from '../conversation/assets/paths.ts';

/**
 * The most a File view will decode. The engine reads up to 16 MiB; a picture this size is already
 * more than a pane can use, and decoding stays bounded on the renderer. Larger files take the
 * "Too large to show" line and the Open in menu.
 */
export const maxImageBytes = 8 * 1024 * 1024;

/**
 * Raster formats only. SVG is deliberately absent: it is markup that can carry script, so it stays
 * the plain binary line here (the preview sheet's own rules for SVG are unchanged).
 */
const rasterExt = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'ico']);
const rasterMime = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/avif', 'image/bmp', 'image/x-icon', 'image/vnd.microsoft.icon']);

/** Whether the File view should ask for this path as a picture rather than as text. */
export const isRasterImagePath = (path: string): boolean => rasterExt.has(extensionOf(path));

export type ImageVerdict = { ok: true; url: string } | { ok: false; reason: 'too-large' | 'unsafe' };

/** Decides whether an engine answer may become an <img> source; the answer is untrusted data. */
export function imageVerdict(file: { mime: string; size: number; dataBase64: string }): ImageVerdict {
  const mime = file.mime.split(';')[0].trim().toLowerCase();
  const base64Cap = Math.ceil(maxImageBytes / 3) * 4;
  if (file.size > maxImageBytes || file.dataBase64.length > base64Cap) return { ok: false, reason: 'too-large' };
  if (!rasterMime.has(mime) || !/^[A-Za-z0-9+/]*={0,2}$/.test(file.dataBase64)) return { ok: false, reason: 'unsafe' };
  return { ok: true, url: `data:${mime};base64,${file.dataBase64}` };
}
