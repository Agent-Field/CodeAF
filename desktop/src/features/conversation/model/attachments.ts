// The person's attachments on a user entry. Structured fields win; the
// "attached file(s):" sentence is read only when the engine sent none.

import type { FileRef } from '../types.ts';
import type { RichEntry } from './entry.ts';

const ONE = /(?:^|\n\n)attached file: ([^\n]+)\s*$/;
const MANY = /(?:^|\n\n)attached files:\n([\s\S]+?)\s*$/;

function structured(entry: RichEntry): FileRef[] {
  const files = (entry.Attachments ?? []).map((a): FileRef => ({
    path: a.Path,
    source: a.Kind === 'image' || a.MIME?.startsWith('image/') ? 'image' : 'attachment',
  }));
  const pictures = (entry.ImageRefs ?? []).map((path): FileRef => ({ path, source: 'image' }));
  return dedupe([...pictures, ...files]);
}

function dedupe(refs: FileRef[]): FileRef[] {
  const seen = new Set<string>();
  return refs.filter((r) => (seen.has(r.path) ? false : (seen.add(r.path), true)));
}

/** Files named by the legacy sentence, and the text without it. */
function fromSentence(text: string): { files: FileRef[]; text: string } {
  const many = MANY.exec(text);
  const one = many ? undefined : ONE.exec(text);
  const hit = many ?? one;
  if (!hit) return { files: [], text };
  const paths = hit[1].split('\n').map((p) => p.trim()).filter(Boolean);
  return {
    files: paths.map((path): FileRef => ({ path, source: 'attachment' })),
    text: text.slice(0, hit.index),
  };
}

export function attachmentsOf(entry: RichEntry): { attachments: FileRef[]; user: string } {
  const pictures = structured(entry);
  if (entry.Attachments) return { attachments: pictures, user: entry.Text };
  const legacy = fromSentence(entry.Text);
  return { attachments: dedupe([...pictures, ...legacy.files]), user: legacy.text };
}
