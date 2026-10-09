// A recorded aside -> a task notice, a job/watch note, or a plain note.
// Ported from transcript.ts: the canonical task row outranks words parsed out
// of the note.

import type { EngineEntry, EngineSnapshot } from '../../chat/engine-client.ts';
import type { TurnItem } from '../types.ts';
import { kindedAside, noteItem } from '../aside-kind.ts';
import { personWords } from '../sessionNote.ts';
import { firstSentence, parseTaskAside } from '../transcript-parse.ts';

// The record may carry these on an aside; the Go side is adding them.
type AsideFields = { TaskIDs?: string[] | null; TaskStatus?: string };
type Row = { ID?: unknown; Title?: string; Status?: string; Note?: string };
type Match = (row: Row) => boolean;

/** Rows arrive oldest run first, so the last match is the newest record. */
function lastRow(tasks: unknown[], matches: Match): Row | undefined {
  return [...(tasks as Row[])].reverse().find(matches);
}

function findRow(tasks: unknown[], given: string | undefined, text: string, title: string): Row | undefined {
  const byId = given ? lastRow(tasks, (t) => String(t.ID) === given) : undefined;
  if (byId) return byId;
  const byNote = lastRow(tasks, (t) => Boolean(t.Note?.trim()) && text.includes(t.Note as string));
  if (byNote) return byNote;
  return title ? lastRow(tasks, (t) => t.Title === title) : undefined;
}

/** The aside as a person reads it: what the session told the model about, not how it told it. */
export function asideItem(recorded: EngineEntry, id: string, snapshot: EngineSnapshot): TurnItem {
  const entry = { ...recorded, Text: personWords(recorded.Text) };
  const kinded = kindedAside(entry, id);
  if (kinded) return kinded;
  const parsed = parseTaskAside(entry.Text);
  const fields = entry as EngineEntry & AsideFields;
  const given = fields.TaskIDs?.[0];
  if (!parsed && !given) return noteItem(id, entry.Text, true);
  const row = findRow(snapshot.tasks, given, entry.Text, parsed?.title ?? '');
  const byId = given && String(row?.ID) === given ? row : undefined;
  // A notice with no name cannot be read or opened; it is only a note.
  const title = row?.Title || parsed?.title || '';
  if (!title) return noteItem(id, entry.Text, true);
  return {
    kind: 'task',
    id,
    taskId: given ?? (row?.ID ? String(row.ID) : undefined),
    title,
    status: fields.TaskStatus ?? byId?.Status ?? parsed?.status ?? row?.Status ?? '',
    summary: parsed?.summary ?? firstSentence(entry.Text),
    body: entry.Text,
  };
}
