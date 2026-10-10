// Place mutations for the undo stack (d5-pl-undo). Each write goes through the Places client and returns the inverse
// of that write: the engine's receipt ids, oldest first, plus the sentence the toast says. Applying the inverse is
// POST /places/undo with those ids; this module never applies it, because the stack (20 steps, one window) is not
// owned here. A write the engine reports as unchanged returns no inverse — there is nothing to take back.
//
// ⌥ is a move (Places 8g): a place's parents become the one target, and a chat leaves the place it was dragged from.
// A plain drop, and "Add to another place…", add a parent and leave the ones it already has. Interactions describes
// the place gesture the other way around; that disagreement is PA-1 in desktop/docs/DESIGN-QUESTIONS.md.

import { PlacesError, type AddedBy, type Mutation, type PlaceDetail, type PlacesClient, type Tint } from './client.ts';

/** Interactions "Delete": the toast offers Undo for 10 seconds. Every other structural place action uses the shared toast duration. */
export const PLACE_DELETE_UNDO_MS = 10_000;

/** What was done, so the undo stack can keep the step without parsing the sentence. */
export type PlaceActionKind =
  | 'create' | 'rename' | 'setTint' | 'addParent' | 'move' | 'pin' | 'unpin' | 'reorder'
  | 'archive' | 'delete' | 'addMembership' | 'removeMembership';

/**
 * The inverse d5-pl-undo stores. `receipts` is the order the engine made them, which is the order
 * POST /places/undo takes them back (newest last in this list, newest first on the wire's walk).
 */
export type PlaceInverse = {
  kind: PlaceActionKind;
  /** The toast's sentence, in the person's words. */
  text: string;
  /** The place or chat the sentence names in medium weight. Absent when no one name was known. */
  subject?: string;
  receipts: readonly string[];
  /** Set only for delete. Absent means the shared toast duration. */
  durationMs?: number;
};

/** One committed write. `inverse` is absent when the engine changed nothing. */
export type PlaceChange = {
  inverse?: PlaceInverse;
  revision: number;
  place?: PlaceDetail;
};

/**
 * What delete reports. A field is present only when the engine sent it: a missing count is not zero.
 * `chats` is how many chats were filed here (they keep their other places, or become unplaced).
 * `children` is how many child places moved up to the deleted place's parents. No chat is deleted.
 */
export type DeleteCounts = {
  chats?: number;
  children?: number;
  nowUnplaced?: readonly string[];
};

export type PlaceActionNames = {
  place: (id: string) => string | undefined;
  chat: (id: string) => string | undefined;
};

/** The slice of the Places client these mutations call. Tests stand a fake in for it. */
export type PlaceMutationClient = Pick<PlacesClient,
  'createPlace' | 'updatePlace' | 'setParents' | 'pinPlace' | 'unpinPlace' | 'railOp' | 'archivePlace' | 'deletePlace' | 'addChats' | 'removeChats'>;

export type PlaceWriteOptions = { generation?: number };

const quoted = (name: string | undefined, fallback: string): { text: string; subject?: string } =>
  name ? { text: `“${name}”`, subject: name } : { text: fallback };

/**
 * The words a cycle is refused with. The store's own text is a package prefix plus an "A -> B -> A" chain;
 * neither is something a person should read. A sentence the engine already wrote is kept.
 */
export function cycleWords(raw: string): string {
  const stripped = raw.replace(/^placegraph:\s*/i, '').replace(/that would make a place its own ancestor:?\s*/i, '').trim();
  if (stripped && !stripped.includes('->')) return /[.!?]$/.test(stripped) ? stripped : `${stripped}.`;
  const names = stripped.split('->').map(part => part.trim()).filter(Boolean);
  if (names.length >= 2 && names.every(name => name === names[0])) return "A place can't be inside itself.";
  // A closed chain ends where it started. The parent that would close it is the name just before that repeat,
  // which is the place the person tried to add — not the first hop in between.
  if (names.length >= 2) {
    const parent = names[0] === names[names.length - 1] ? names[names.length - 2] : names[1];
    return `That would put “${names[0]}” inside “${parent}”, which is already inside it.`;
  }
  return 'That would put a place inside itself.';
}

function refuseCycle(error: unknown): never {
  if (error instanceof PlacesError && error.code === 'cycle') {
    const words = cycleWords(error.message);
    if (words === error.message) throw error;
    throw new PlacesError(words, error.status || 409, 'cycle', error.applied, error.undone);
  }
  // A caller that still holds the store's Go error, rather than the bridge's sentence, is translated the same way.
  if (error instanceof Error && /placegraph:\s*that would make a place its own ancestor/i.test(error.message)) {
    throw new PlacesError(cycleWords(error.message), 409, 'cycle');
  }
  throw error;
}

function inverseOf(kind: PlaceActionKind, text: string, mutation: Mutation, subject?: string, durationMs?: number): PlaceInverse | undefined {
  const receipts = Array.isArray(mutation.undo) ? mutation.undo.filter((id): id is string => typeof id === 'string' && id.length > 0) : [];
  if (mutation.noop || receipts.length === 0) return undefined;
  return { kind, text, ...(subject ? { subject } : {}), receipts: receipts.slice(), ...(durationMs !== undefined ? { durationMs } : {}) };
}

function changeOf(kind: PlaceActionKind, text: string, mutation: Mutation, subject?: string, durationMs?: number): PlaceChange {
  const inverse = inverseOf(kind, text, mutation, subject, durationMs);
  return { ...(inverse ? { inverse } : {}), revision: mutation.revision, ...(mutation.place ? { place: mutation.place } : {}) };
}

function deleteCounts(mutation: Mutation): DeleteCounts {
  const counts: DeleteCounts = {};
  const result = mutation.result;
  if (typeof mutation.chats === 'number') counts.chats = mutation.chats;
  else if (result && typeof result.unfiled === 'number') counts.chats = result.unfiled;
  if (typeof mutation.children === 'number') counts.children = mutation.children;
  else if (result && typeof result.childrenMoved === 'number') counts.children = result.childrenMoved;
  if (result && Array.isArray(result.nowUnplaced) && result.nowUnplaced.every(id => typeof id === 'string')) counts.nowUnplaced = result.nowUnplaced.slice();
  return counts;
}

function chatsPhrase(ids: readonly string[], title: (id: string) => string | undefined): string {
  if (ids.length === 1) return quoted(title(ids[0]), 'the chat').text;
  return `${ids.length} chats`;
}

export function createPlaceActions(client: PlaceMutationClient, names: PlaceActionNames = { place: () => undefined, chat: () => undefined }) {
  const place = (id: string) => quoted(names.place(id), 'the place');
  const run = async (job: () => Promise<Mutation>): Promise<Mutation> => {
    try { return await job(); }
    catch (error) { refuseCycle(error); }
  };
  // A place inside itself is a cycle the engine would refuse. Saying it here means the client is not asked.
  const refuseSelf = (id: string, parent: string) => {
    if (id === parent) throw new PlacesError("A place can't be inside itself.", 409, 'cycle');
  };

  return {
    create: async (draft: { name: string; tint?: Tint; parent?: string; parents?: string[]; instructions?: string }, options?: PlaceWriteOptions): Promise<PlaceChange> => {
      const named = quoted(draft.name, 'the place');
      const mutation = await run(() => client.createPlace({ ...draft, ifGeneration: options?.generation }));
      return changeOf('create', `Created ${named.text}`, mutation, named.subject);
    },

    rename: async (id: string, name: string, options?: PlaceWriteOptions): Promise<PlaceChange> => {
      const next = quoted(name, 'the place');
      const mutation = await run(() => client.updatePlace(id, { name, ifGeneration: options?.generation }));
      return changeOf('rename', `Renamed ${place(id).text} to ${next.text}`, mutation, next.subject);
    },

    setTint: async (id: string, tint: Tint | '', options?: PlaceWriteOptions): Promise<PlaceChange> => {
      const named = place(id);
      const mutation = await run(() => client.updatePlace(id, { tint, ifGeneration: options?.generation }));
      return changeOf('setTint', `Changed the tint of ${named.text}`, mutation, named.subject);
    },

    /** Adds one parent and keeps the ones the place already has. Tint follows the first parent, so this never changes it. */
    addParent: async (id: string, parentId: string, options?: PlaceWriteOptions): Promise<PlaceChange> => {
      refuseSelf(id, parentId);
      const named = place(id);
      const mutation = await run(() => client.setParents(id, { add: parentId, ifGeneration: options?.generation }));
      return changeOf('addParent', `Added ${named.text} into ${place(parentId).text}`, mutation, named.subject);
    },

    /**
     * ⌥ on a place: its only parent becomes the target. Children and chats stay filed in it.
     * A cycle (the target is this place, or already inside it) is the engine's sentence, in words.
     */
    move: async (id: string, intoId: string, options?: PlaceWriteOptions): Promise<PlaceChange> => {
      refuseSelf(id, intoId);
      const named = place(id);
      const mutation = await run(() => client.setParents(id, { set: [intoId], ifGeneration: options?.generation }));
      return changeOf('move', `Moved ${named.text} into ${place(intoId).text}`, mutation, named.subject);
    },

    pin: async (id: string, options?: PlaceWriteOptions & { index?: number }): Promise<PlaceChange> => {
      const named = place(id);
      const mutation = await run(() => client.pinPlace(id, options?.index, options?.generation));
      return changeOf('pin', `Pinned ${named.text} to the rail`, mutation, named.subject);
    },

    unpin: async (id: string, options?: PlaceWriteOptions): Promise<PlaceChange> => {
      const named = place(id);
      const mutation = await run(() => client.unpinPlace(id, options?.generation));
      return changeOf('unpin', `Unpinned ${named.text}`, mutation, named.subject);
    },

    /** The pinned rail, in the person's order. The sentence names no one place (PA-2). */
    reorder: async (order: readonly string[], options?: PlaceWriteOptions): Promise<PlaceChange> => {
      const mutation = await run(() => client.railOp({ op: 'reorder', order: [...order], ifGeneration: options?.generation }));
      return changeOf('reorder', 'Reordered the pinned places.', mutation);
    },

    /**
     * Archive first: the place is marked archived and leaves the rail in the one commit the engine makes.
     * It stays in the graph, so ⌘P and History can still find it, and its chats are not deleted.
     * Undo of the returned receipt puts the rail pin back when the place had one.
     */
    archive: async (id: string, options?: PlaceWriteOptions): Promise<PlaceChange & { archived?: boolean }> => {
      const named = place(id);
      const mutation = await run(() => client.archivePlace(id, options?.generation));
      const change = changeOf('archive', `Archived ${named.text}`, mutation, named.subject);
      return mutation.place ? { ...change, archived: mutation.place.archived } : change;
    },

    /** Deletes the place only. Returns the engine's counts and an inverse whose toast stays for 10 seconds. */
    delete: async (id: string, options?: PlaceWriteOptions): Promise<PlaceChange & { counts: DeleteCounts }> => {
      const named = place(id);
      const mutation = await run(() => client.deletePlace(id, options?.generation));
      return { ...changeOf('delete', `Deleted ${named.text}. No chat was deleted.`, mutation, named.subject, PLACE_DELETE_UNDO_MS), counts: deleteCounts(mutation) };
    },

    /**
     * Files chats in a place. `moveFrom` is ⌥: each chat leaves that place as it joins this one
     * ('now' is allowed). The same place as the destination is not a move.
     */
    addMembership: async (placeId: string, chatIds: readonly string[], options?: PlaceWriteOptions & { moveFrom?: string; addedBy?: AddedBy }): Promise<PlaceChange> => {
      const into = place(placeId);
      const moving = !!options?.moveFrom && options.moveFrom !== placeId;
      const mutation = await run(() => client.addChats(placeId, [...chatIds], {
        ...(options?.addedBy ? { addedBy: options.addedBy } : {}),
        ...(moving ? { moveFrom: options?.moveFrom } : {}),
        ifGeneration: options?.generation,
      }));
      const verb = moving ? 'Moved' : 'Added';
      return changeOf(moving ? 'move' : 'addMembership', `${verb} ${chatsPhrase(chatIds, names.chat)} to ${into.text}`, mutation, into.subject);
    },

    removeMembership: async (placeId: string, chatIds: readonly string[], options?: PlaceWriteOptions): Promise<PlaceChange> => {
      const from = place(placeId);
      const mutation = await run(() => client.removeChats(placeId, [...chatIds], options?.generation));
      return changeOf('removeMembership', `Removed ${chatsPhrase(chatIds, names.chat)} from ${from.text}`, mutation, from.subject);
    },
  };
}

export type PlaceMutations = ReturnType<typeof createPlaceActions>;
