/** Pure helpers for paths, file kinds and site identity. No DOM, so they run under node --test. */

export type FileKind = 'image' | 'code' | 'pdf' | 'audio' | 'video' | 'folder' | 'doc';
export type PreviewKind = 'image' | 'text' | 'pdf' | 'none';

const imageExt = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'svg', 'ico']);
const audioExt = new Set(['mp3', 'wav', 'ogg', 'm4a', 'flac', 'aac', 'opus']);
const videoExt = new Set(['mp4', 'mov', 'webm', 'mkv', 'm4v']);
const docExt = new Set(['md', 'mdx', 'txt', 'rst', 'rtf', 'csv', 'tsv', 'log', 'doc', 'docx']);
const codeExt = new Set([
  'ts', 'tsx', 'js', 'jsx', 'mjs', 'cjs', 'go', 'rs', 'py', 'rb', 'java', 'kt', 'swift', 'c', 'h', 'cc', 'cpp',
  'cs', 'php', 'sh', 'bash', 'zsh', 'sql', 'html', 'css', 'scss', 'json', 'yaml', 'yml', 'toml', 'xml', 'lua',
  'mod', 'sum', 'lock', 'env', 'ini', 'conf', 'proto', 'vue', 'svelte',
]);

export function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
}

export function splitPath(path: string): { name: string; dir: string } {
  const trimmed = path.length > 1 ? path.replace(/\/+$/, '') : path;
  const cut = trimmed.lastIndexOf('/');
  if (cut < 0) return { name: trimmed, dir: '' };
  return { name: trimmed.slice(cut + 1) || trimmed, dir: trimmed.slice(0, cut) };
}

export function fileKind(name: string, isDir = false): FileKind {
  if (isDir) return 'folder';
  const ext = extensionOf(name);
  if (imageExt.has(ext)) return 'image';
  if (ext === 'pdf') return 'pdf';
  if (audioExt.has(ext)) return 'audio';
  if (videoExt.has(ext)) return 'video';
  if (codeExt.has(ext)) return 'code';
  return 'doc';
}

/** SVG previews as an image; unknown extensions are tried as text because the engine sniffs nothing. */
export function previewKind(name: string): PreviewKind {
  const kind = fileKind(name);
  if (kind === 'image') return 'image';
  if (kind === 'pdf') return 'pdf';
  if (kind === 'code' || kind === 'doc') return extensionOf(name) === 'docx' || extensionOf(name) === 'doc' ? 'none' : 'text';
  return 'none';
}

/** Workspace-relative form of a path, or null when it clearly lies outside the workspace. */
export function relativePath(path: string, workspace: string): string | null {
  const root = workspace.replace(/\/+$/, '');
  if (!path.startsWith('/')) return path.split('/').includes('..') ? null : path.replace(/^\.\//, '');
  if (!root) return path;
  if (path === root) return '.';
  return path.startsWith(`${root}/`) ? path.slice(root.length + 1) : null;
}

export function absolutePath(path: string, workspace: string): string {
  if (path.startsWith('/') || !workspace) return path;
  return `${workspace.replace(/\/+$/, '')}/${path.replace(/^\.\//, '')}`;
}

/** Keeps both ends of a long directory: the root hints where, the tail says which. */
export function middleTruncate(text: string, max: number): string {
  if (text.length <= max || max < 5) return text;
  const keep = max - 1;
  const head = Math.ceil(keep / 2);
  return `${text.slice(0, head)}…${text.slice(text.length - (keep - head))}`;
}

const secondLevel = new Set(['co', 'com', 'org', 'net', 'gov', 'ac', 'edu']);

export function hostnameOf(href: string): string | null {
  try {
    const url = new URL(href);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
    return url.hostname.replace(/^www\./, '').toLowerCase();
  } catch {
    return null;
  }
}

/** Last two labels, or three under a country second level such as co.uk. */
export function registrableDomain(host: string): string {
  const labels = host.split('.').filter(Boolean);
  if (labels.length <= 2) return labels.join('.');
  const tail = labels.slice(-2);
  const take = secondLevel.has(tail[0]) && tail[1].length === 2 ? 3 : 2;
  return labels.slice(-take).join('.');
}

export const monogramHues = 6;

/** Stable 1..6 bucket from the domain, so a site keeps its colour across sessions. */
export function monogramHue(domain: string): number {
  let hash = 5381;
  for (const char of domain) hash = (Math.imul(hash, 33) ^ char.charCodeAt(0)) >>> 0;
  return (hash % monogramHues) + 1;
}

export function monogramLetter(domain: string): string {
  const first = registrableDomain(domain).charAt(0);
  return first ? first.toUpperCase() : '·';
}

const looseExt = /\.[A-Za-z][A-Za-z0-9]{0,7}$/;

/** Cheap pre-filter: only text that can be a path is worth a stat call. */
export function looksLikePath(text: string): boolean {
  if (text.length === 0 || text.length > 260) return false;
  if (/[\s<>|*?"'`(){}$,;=]/.test(text) || text.startsWith('-') || /^[a-z][a-z0-9+.-]*:/i.test(text)) return false;
  if (text.includes('/')) return true;
  const ext = extensionOf(text);
  return looseExt.test(text) && (imageExt.has(ext) || codeExt.has(ext) || docExt.has(ext) || ext === 'pdf');
}

/** A markdown href with no scheme is a local reference; the engine decides whether it exists. */
export function localPathFromHref(href: string): string | null {
  const value = href.trim();
  if (!value || value.startsWith('#') || value.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(value) || /[\0\n\r]/.test(value)) return null;
  const bare = value.split(/[?#]/)[0];
  try {
    return decodeURIComponent(bare) || null;
  } catch {
    return bare || null;
  }
}

export function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

export function aspectOf(meta: string): number | null {
  const match = /(\d{2,5})\s*[×x]\s*(\d{2,5})/.exec(meta);
  return match ? Number(match[1]) / Number(match[2]) : null;
}
