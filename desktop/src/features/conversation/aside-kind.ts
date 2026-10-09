// Asides the engine labelled with a kind. The label decides how the notice is
// drawn; the text is never reworded and no title is made up.

import type { EngineEntry } from '../chat/engine-client.ts';
import { personWords } from './sessionNote.ts';
import type { TurnItem } from './types.ts';

export type AsideKind = 'task' | 'job' | 'watch' | 'resume' | 'places' | '';
// The Go side adds these to the record; the type stays optional so older engines compile.
type KindFields = { AsideKind?: AsideKind; AsideTitle?: string };

/** Longer than this and a model-directed note is a wall; it is drawn folded. */
export const LONG_NOTE = 240;

/** A note of several lines, or one over the limit, shows its first line and folds the rest. */
const isLong = (text: string) => text.length > LONG_NOTE || text.includes('\n');

export function asideKindOf(entry: EngineEntry): AsideKind {
  return (entry as EngineEntry & KindFields).AsideKind ?? '';
}

/** The engine's own one-line name for the aside, '' when it gave none. */
export function asideTitleOf(entry: EngineEntry): string {
  return (entry as EngineEntry & KindFields).AsideTitle?.trim() ?? '';
}

/** An aside is the session's own note: drawn in a person's words, folded when it is more than a line. */
export function noteItem(id: string, text: string, fromAside: boolean): TurnItem {
  if (!fromAside) return { kind: 'note', id, text };
  const words = personWords(text);
  return { kind: 'note', id, text: words, long: isLong(words) ? true : undefined };
}

/** job, watch and resume notices; undefined for task or an unlabelled aside. */
export function kindedAside(entry: EngineEntry, id: string): TurnItem | undefined {
  const kind = asideKindOf(entry);
  if (kind === 'places') return { kind: 'note', id, text: entry.Text, undoReceipts: entry.UndoReceipts?.filter(id => typeof id === 'string' && id.length > 0) };
  if (kind === 'resume') return { kind: 'note', id, text: entry.Text };
  if (kind !== 'job' && kind !== 'watch') return undefined;
  return { kind: 'aside', id, aside: kind, title: asideTitleOf(entry), body: entry.Text };
}
