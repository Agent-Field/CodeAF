import { EngineError, fetchEngine } from '../chat/engine-client.ts';

/**
 * One folder of a conversation, as GET /sessions/{id}/files/list answers it.
 *
 * Tree and folder sources read this instead of the disk: the engine may be on
 * another machine, and it is the only process that can see the two roots.
 * `path` is the path the engine resolved. The client never builds one.
 */
export type FolderEntry = {
  name: string;
  /** True for a directory. A file sends false; the field is never absent. */
  dir: boolean;
  /** Bytes. Zero is an empty file, so it stays. */
  size: number;
  /** Unix seconds. Absent when the engine sent none (Q-FL1). */
  modTime?: number;
};

export type FolderListing = {
  path: string;
  entries: FolderEntry[];
  truncated: boolean;
};

function invalid(): never {
  throw new EngineError('The engine returned an invalid folder listing.');
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

/** A JSON number the bridge can have sent: a whole count, not a float and not past what JS can hold. */
function whole(value: unknown): value is number {
  return Number.isSafeInteger(value);
}

/**
 * Copies one wire row into the fields this window reads. Mime is dropped: the
 * route does not carry it (Q-FL1). A missing time stays missing; a present
 * zero is the same absence, because the bridge omits a zero.
 */
function entryFrom(value: unknown): FolderEntry {
  if (!isRecord(value)) invalid();
  const { name, dir, size, modTime } = value;
  if (typeof name !== 'string' || !name || typeof dir !== 'boolean' || !whole(size)) invalid();
  if (size < 0) invalid();
  const entry: FolderEntry = { name, dir, size };
  if (modTime === undefined || modTime === 0) return entry;
  if (!whole(modTime)) invalid();
  if (modTime < 0) invalid();
  entry.modTime = modTime;
  return entry;
}

/**
 * Refuses a body that is not the documented listing. A missing `truncated`
 * must not be read as "the folder ended here", and a missing `path` must not
 * be replaced with the path we asked for.
 */
function listingFrom(value: unknown): FolderListing {
  if (!isRecord(value) || typeof value.path !== 'string' || !Array.isArray(value.entries) || typeof value.truncated !== 'boolean') invalid();
  return { path: value.path, entries: value.entries.map(entryFrom), truncated: value.truncated };
}

/**
 * Lists one folder through the engine. The path is sent unchanged, including
 * a blank one: the engine reads a blank path as the workspace root (".") and
 * confines it. Order is the engine's (directories first, then files, each
 * half by name). Nothing here re-sorts, and nothing here cuts the page — the
 * bridge already stops at its own ceiling and sets `truncated`.
 */
export async function listEngineFolder(id: string, path: string): Promise<FolderListing> {
  const response = await fetchEngine(`/sessions/${encodeURIComponent(id)}/files/list?path=${encodeURIComponent(path)}`);
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    invalid();
  }
  return listingFrom(body);
}
