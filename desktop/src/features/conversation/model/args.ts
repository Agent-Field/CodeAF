// Readers for a tool call's arguments and results. Args are compact JSON the
// engine capped at 32KB; long strings end "… (N more bytes)".

export type Fields = Record<string, unknown>;

const CAP_MARK = '… (';
const CAP_END = ' more bytes)';

export function argsOf(args?: string): Fields {
  if (!args) return {};
  try {
    const parsed: unknown = JSON.parse(args);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Fields) : {};
  } catch {
    return {};
  }
}

export function argString(fields: Fields, key: string): string {
  const value = fields[key];
  return typeof value === 'string' ? value : '';
}

/** The text without the cap sentence, and whether the cap was there. */
export function argBody(text: string): { body: string; capped: boolean } {
  if (!text.endsWith(CAP_END)) return { body: text, capped: false };
  const at = text.lastIndexOf(CAP_MARK);
  return at < 0 ? { body: text, capped: false } : { body: text.slice(0, at), capped: true };
}

/** A trailing newline ends the last line; it does not open an empty one. */
export function splitLines(text: string): string[] {
  if (!text) return [];
  return (text.endsWith('\n') ? text.slice(0, -1) : text).split('\n');
}

export const lineCount = (text: string): number => splitLines(text).length;

export function firstLine(text: string): string {
  return text.trim().split('\n')[0]?.trim() ?? '';
}

export function baseName(path: string): string {
  const parts = path.replace(/\/+$/, '').split('/');
  return parts[parts.length - 1] ?? path;
}

export function dirName(path: string): string {
  const at = path.lastIndexOf('/');
  return at <= 0 ? '' : path.slice(0, at);
}
