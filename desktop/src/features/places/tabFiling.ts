// A tab dropped on a rail row joins that place in the strip (reducers/home.ts `tab-place`) and, when it holds saved
// conversations, files those chats there too, because the engine's membership is the truth a place's Home reads
// (engine truth: a place lists only chats that are really in it). Nothing is moved out of any other place.
import { panesOf } from '../tabs/helpers.ts';
import type { Tab } from '../tabs/types.ts';
import { chatIdFromSessionFile } from './client.ts';
import type { PlacesShell } from './shell/PlacesShell.tsx';

/** The chat ids of every saved conversation a tab shows (a split may show several). */
export function chatIdsOf(tab: Tab): string[] {
  return panesOf(tab).flatMap(pane => (pane.kind === 'conversation' && pane.sessionFile ? [chatIdFromSessionFile(pane.sessionFile)] : [])).filter((id): id is string => !!id);
}

export function fileTabChats(shell: PlacesShell, tab: Tab | undefined, placeId: string) {
  const ids = tab ? chatIdsOf(tab) : [];
  if (!ids.length) return;
  const name = shell.index?.byId.get(placeId)?.name;
  void shell.write(`Added ${ids.length === 1 ? 'a chat' : `${ids.length} chats`} to “${name ?? 'the place'}”`, () => shell.client.addChats(placeId, ids), { subject: name }).catch(shell.warn);
}
