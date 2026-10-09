// Every verb a Home can ask for, wired to the Places client through the shell's one write path (so each structural
// change is a receipt with Undo) and to the window's strip. A verb with nothing real behind it is left out, so the
// Ledger suggestions are read and decided by PlaceOfferLine; the
// folder chooser that makes a place from a repo exists only in the desktop app, so `openFolderAsPlace` is absent in a
// browser (place-actions.ts "a verb that is not passed has no control").

import { newTab } from '../../tabs/helpers';
import type { Tab, WorkspaceAction, WorkspaceState } from '../../tabs/model';
import { panesOf } from '../../tabs/model';
import type { HomeDigest } from '../client';
import type { PlaceActions } from '../place-actions';
import type { PlacesShell } from './PlacesShell';
import { sessionFileOf } from './selectors';

export type HomeActionDeps = {
  shell: PlacesShell;
  /** The Home this is: `root` or a place id. */
  homeId: string;
  /** Every digest the page has read (its own, and Now's for the root): where a chat row's session file is found. */
  digests: readonly (HomeDigest | undefined)[];
  /** The strip this Home sits in. */
  strip?: { state: WorkspaceState; dispatch: (action: WorkspaceAction) => void };
  quickLook: (placeId: string) => void;
  retry: () => void;
};

const quoted = (name: string | undefined) => (name ? `“${name}”` : 'the place');

/** The conversation tab already showing this journal, if the strip has one. */
function tabShowing(state: WorkspaceState, sessionFile: string): string | undefined {
  for (const tab of state.tabs) for (const pane of panesOf(tab)) if (pane.sessionFile === sessionFile) return pane.id;
  return undefined;
}

export function buildHomeActions({ shell, homeId, digests, strip, quickLook, retry }: HomeActionDeps): PlaceActions {
  const name = (id: string) => (id === 'root' ? 'All places' : id === 'now' ? 'Now' : shell.index?.byId.get(id)?.name);
  const chatTitle = (chatId: string) => {
    for (const digest of digests) { const row = digest?.chats.find(chat => chat.id === chatId); if (row) return row.title || 'Untitled chat'; }
    return 'Untitled chat';
  };
  const openChat = (chatId: string, background: boolean) => {
    if (!strip) throw new Error('Chats open in the workspace.');
    const sessionFile = sessionFileOf(digests, chatId);
    // A row the engine could not place on disk cannot be reattached; saying so beats opening an empty tab.
    if (!sessionFile) throw new Error('This chat cannot be opened here: the engine did not say where it is saved.');
    const existing = tabShowing(strip.state, sessionFile);
    if (existing) { if (!background) strip.dispatch({ type: 'select', id: existing }); return; }
    const tab: Tab = newTab({ kind: 'conversation', title: chatTitle(chatId), titleSource: 'engine', sessionFile });
    strip.dispatch({ type: 'open', tab, background });
  };
  const ready = shell.places.status === 'ready' || shell.places.status === 'offline';
  if (!ready && !shell.places.graph) return { retry };
  const { client, write } = shell;

  return {
    goTo: id => shell.goTo(id),
    goToInNewWindow: id => shell.goToInNewWindow(id),
    quickLook,
    openChat: id => openChat(id, false),
    openChatInNewTab: id => openChat(id, true),

    create: async draft => {
      await write(`Created ${quoted(draft.name)}`, () => client.createPlace({ name: draft.name, tint: draft.tint, parent: draft.parent }), { subject: draft.name });
    },
    rename: async (id, next) => { await write(`Renamed ${quoted(name(id))} to ${quoted(next)}`, () => client.updatePlace(id, { name: next }), { subject: next }); },
    setTint: async (id, tint) => { await write(`Changed the tint of ${quoted(name(id))}`, () => client.updatePlace(id, { tint }), { subject: name(id) }); },
    pin: async id => { await write(`Pinned ${quoted(name(id))} to the rail`, () => client.pinPlace(id), { subject: name(id) }); },
    unpin: async id => { await write(`Unpinned ${quoted(name(id))}`, () => client.unpinPlace(id), { subject: name(id) }); },
    archive: async id => { await write(`Archived ${quoted(name(id))}`, () => client.archivePlace(id), { subject: name(id) }); },
    restore: async id => { await write(`Restored ${quoted(name(id))}`, () => client.restorePlace(id), { subject: name(id) }); },
    loadDeletePreview: id => client.deletePreview(id),
    // Delete keeps its Undo longer than other toasts (Interactions "a toast with Undo for 10s").
    remove: async id => {
      const gone = name(id);
      await write(`Deleted ${quoted(gone)}. No chat was deleted.`, () => client.deletePlace(id), { subject: gone, durationMs: 10_000 });
      if (homeId === id) await shell.goTo('now');
    },
    chooseMergeTarget: id => shell.openChooser({ kind: 'merge', placeId: id, placeName: name(id) ?? 'this place' }),
    chooseAnotherParent: id => shell.openChooser({ kind: 'parent', placeId: id, placeName: name(id) ?? 'this place' }),
    chooseChatPlace: chatId => {
      const already = digests.flatMap(digest => digest?.chats.find(chat => chat.id === chatId)?.places.map(place => place.id) ?? []);
      shell.openChooser({ kind: 'file', chatIds: [chatId], chatTitle: chatTitle(chatId), exclude: [...new Set(already)] });
    },
    file: async (drop, target, mode) => {
      const into = name(target);
      if (drop.kind === 'chat') {
        // A chat dragged from a place's Home moves out of THAT place with ⌥; from All places it comes from no place.
        const from = homeId === 'root' ? 'now' : homeId;
        const many = drop.ids.length === 1 ? quoted(chatTitle(drop.ids[0])) : `${drop.ids.length} chats`;
        await write(`${mode === 'move' ? 'Moved' : 'Added'} ${many} to ${quoted(into)}`,
          () => client.addChats(target, [...drop.ids], mode === 'move' && from !== target ? { moveFrom: from } : {}), { subject: into });
        return;
      }
      for (const id of drop.ids) {
        // ⌥ makes it a child of the target only; a plain drop adds the target as one more parent (Places 8g).
        const job = mode === 'move' ? () => client.setParents(id, { set: [target] }) : () => client.setParents(id, { add: target });
        await write(`${mode === 'move' ? 'Moved' : 'Added'} ${quoted(name(id))} into ${quoted(into)}`, job, { subject: name(id) });
      }
    },
    removeChat: async (chatId, from) => { await write(`Removed ${quoted(chatTitle(chatId))} from ${quoted(name(from))}`, () => client.removeChats(from, [chatId]), { subject: name(from) }); },
    removeSource: async (placeId, sourceId) => { await write(`Removed a source from ${quoted(name(placeId))}`, () => client.removeSource(placeId, sourceId), { subject: name(placeId) }); },
    addSources: placeId => shell.openDialog({ kind: 'sources', placeId }),
    writeInstructions: placeId => shell.openDialog({ kind: 'instructions', placeId }),
    openFolderAsPlace: shell.native.desktop ? () => { void createFromFolder(shell).catch(shell.warn); } : undefined,
    retry,
  };
}

/** First launch's "Open a folder or repo": the native chooser, then a place named after the folder with it as its one source. */
export async function createFromFolder(shell: PlacesShell): Promise<void> {
  const picked = await shell.native.pickFolder('Open a folder or repo as a place');
  if (picked.status === 'busy') throw new Error('Another chooser is already open.');
  if (picked.status !== 'picked') return;
  const folder = picked.paths[0];
  const created = await shell.write(`Created “${folder.name}” from ${folder.name}`, async () => {
    const made = await shell.client.createPlace({ name: folder.name });
    const id = made.place?.id;
    if (!id) return made;
    try { return [made, await shell.client.addSource(id, { kind: 'folder', ref: folder.path })]; }
    catch (failure) {
      // The place exists; its source was refused. Undo stays available for the place itself.
      shell.warn(failure);
      return made;
    }
  }, { subject: folder.name });
  const id = created[0]?.place?.id;
  if (id) await shell.goTo(id);
}
