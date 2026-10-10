import type { EngineEntry, EngineSnapshot } from './engine-client.ts';

/** The header of a tail answer: every snapshot field but the transcript, plus how many entries the transcript holds. */
export type SnapshotHeader = Omit<EngineSnapshot, 'entries'> & { entryCount: number };
/** GET /sessions/{id}?since=N: entries from index `from` on; `reset` says to discard what is held. */
export type SnapshotTail = { header: SnapshotHeader; from: number; entries: EngineEntry[] | null; reset?: boolean };

/** True when the answer is a tail rather than a whole snapshot, which carries `entries` at the top level. */
export const isTail = (value: unknown): value is SnapshotTail =>
  !!value && typeof value === 'object' && 'header' in value && 'from' in value;

/**
 * Folds a tail into the snapshot a window already holds. The header always
 * replaces the old one, so a header-only tail refreshes running state without
 * touching the transcript. A reset replaces the transcript wholesale, because
 * the engine rewrote it under us (compaction shrinks it). An entry whose
 * output the engine left out keeps `Output` empty and `OutputOmitted` set until
 * the view asks for it with readToolResult; nothing here ever fetches.
 * A tail that does not meet the held transcript, or whose total disagrees with
 * the header, throws so the caller refetches whole instead of showing a lie.
 */
export function mergeTail(held: EngineSnapshot, tail: SnapshotTail): EngineSnapshot {
  const incoming = tail.entries ?? [];
  const { entryCount, ...header } = tail.header;
  const entries = tail.reset ? incoming : joined(held.entries, tail.from, incoming);
  if (entries.length !== entryCount) throw new Error(`The snapshot tail holds ${entries.length} entries but its header counts ${entryCount}.`);
  return { ...header, entries };
}

function joined(held: EngineEntry[], from: number, incoming: EngineEntry[]): EngineEntry[] {
  if (!Number.isSafeInteger(from) || from < 0 || from > held.length) throw new Error(`The snapshot tail starts at ${from} but ${held.length} entries are held.`);
  return held.slice(0, from).concat(incoming);
}
