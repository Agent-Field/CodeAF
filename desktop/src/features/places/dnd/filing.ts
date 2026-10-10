import type { PlaceDropAction } from './placeDnd.ts';
import type { SourceKind } from '../wire.ts';

/** Filing performs what placeDnd decided: it is the only code that turns a resolved drop into engine writes. Places 6e: a file, folder
 * or URL becomes a source of the place; a tab (already resolved to its saved chat) becomes membership. It owns no state and imports no
 * client, so the root can hand it the real Places client and the tests a recorder. */
export type FilingClient = {
  addSource: (placeId: string, ask: { kind: SourceKind; ref: string }) => Promise<unknown>;
  addChats: (placeId: string, chats: string[], opts?: { moveFrom?: string }) => Promise<unknown>;
};
export type FilingDeps = {
  client: FilingClient;
  /** The absolute path the platform holds for a dropped File. A browser tab has none, and a path is never guessed from a name. */
  nativePath: (file: File) => string | undefined;
  /** What a dropped path is on disk, from the native shell. Undefined when the shell cannot say, which refuses that one item. */
  kindOf: (path: string) => Promise<Extract<SourceKind, 'file' | 'folder' | 'repo'> | undefined>;
};
/** `added` counts what the engine accepted; `skipped` names, in the person's words, what it could not take. Nothing is thrown for a
 * skipped item so one bad file in a drop of ten does not undo the other nine. */
export type FilingOutcome = { added: number; skipped: string[] };

/** Sources go in one write each, in drop order. An engine refusal stops the drop there (it propagates) and what was added stays undoable. */
async function fileSources(action: Extract<PlaceDropAction, { kind: 'sources-add' }>, deps: FilingDeps): Promise<FilingOutcome> {
  const { targetPlaceId, sources } = action;
  const outcome: FilingOutcome = { added: 0, skipped: [] };
  if (sources.kind === 'urls') {
    for (const ref of sources.urls) { await deps.client.addSource(targetPlaceId, { kind: 'url', ref }); outcome.added++; }
    return outcome;
  }
  for (const file of sources.files) {
    const path = deps.nativePath(file);
    const kind = path ? await deps.kindOf(path) : undefined;
    if (!path || !kind) { outcome.skipped.push(file.name); continue; }
    await deps.client.addSource(targetPlaceId, { kind, ref: path });
    outcome.added++;
  }
  return outcome;
}

export async function performFiling(action: PlaceDropAction, deps: FilingDeps): Promise<FilingOutcome> {
  switch (action.kind) {
    case 'sources-add': return fileSources(action, deps);
    case 'chat-add':
    case 'chat-move':
      await deps.client.addChats(action.targetPlaceId, [...action.chatIds], action.kind === 'chat-move' && action.fromPlaceId ? { moveFrom: action.fromPlaceId } : {});
      return { added: action.chatIds.length, skipped: [] };
    default: return { added: 0, skipped: [] };
  }
}

/** The sentence for a drop that left something behind; empty when nothing was skipped (unknown renders as nothing). */
export function skippedSentence(skipped: readonly string[]): string {
  if (!skipped.length) return '';
  const what = skipped.length === 1 ? `“${skipped[0]}”` : `${skipped.length} items`;
  return `${what} could not be added because its location is not available here. Add it with Add files or links.`;
}
