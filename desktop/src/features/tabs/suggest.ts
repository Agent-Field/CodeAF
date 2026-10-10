// The workspace rule for a group suggestion (Shell 2h, "3 or more tabs on one repo or topic, at most once
// per session per set"). "Topic" here is the workspace root the world row already recorded for that
// conversation. Titles are never read: two chats with the same words and different roots are not a set.
// Dismissal is the caller's in-memory set for this app session. This function writes nothing.
import { chatIdFromSessionFile } from '../places/client.ts';
import type { WorldRow } from '../world/types.ts';
import type { Pane, Tab, TabGroup } from './types.ts';

/** Design: a suggestion needs at least this many tabs on one workspace root. */
export const suggestionMinimum = 3;

/** The fields this rule reads off a world row. Anything else on the row is irrelevant to the set. */
export type SessionWorkspace = Pick<WorldRow, 'workspace'>;

/** One suggestion. `ids` are tab ids in strip order. `title` is the workspace folder, never a tab title. */
export type GroupSuggestion = { ids: string[]; title: string };

/**
 * Canonical key of a tab-id set. Order does not matter, so the same tabs rearranged in the strip stay one
 * dismissed set. The caller keeps these keys in memory for the app session and passes them back as `dismissed`.
 */
export function suggestionSetKey(ids: readonly string[]): string {
  return [...ids].sort().join('\0');
}

type Rows = ReadonlyMap<string, SessionWorkspace> | Readonly<Record<string, SessionWorkspace | undefined>>;

function rowFor(rows: Rows, session: string): SessionWorkspace | undefined {
  // A Map is the live index (chat id to row). A plain record is the same index in a test.
  // ReadonlyMap does not narrow under instanceof, so the lookup is chosen by the method.
  if (typeof (rows as { get?: unknown }).get === 'function') return (rows as ReadonlyMap<string, SessionWorkspace>).get(session);
  return (rows as Record<string, SessionWorkspace | undefined>)[session];
}

/**
 * The session a tab's world row is filed under: the chat id in its session file (the folder that holds the
 * transcript), which is how the world client keys a row. A tab with no session file uses `target.sessionId`.
 * A split is one tab, so only the focused pane is asked; the other panes are not extra members.
 */
function sessionOf(tab: Tab): string | undefined {
  const pane: Pane = tab.split ? (tab.split.panes[tab.split.focus] ?? tab.split.panes[0] ?? tab) : tab;
  if (pane.sessionFile) {
    const id = chatIdFromSessionFile(pane.sessionFile);
    if (id) return id;
  }
  const session = pane.target?.sessionId?.trim();
  return session || undefined;
}

/**
 * Identity and folder name of a recorded workspace. A trailing slash is the same root. The path is not
 * resolved on disk, so two different spellings of one folder stay two roots. A blank path, `.`, or a
 * path with no folder name contributes nothing (emptiness: unknown is not a guess).
 */
export function workspaceFolder(workspace: string | undefined): { key: string; title: string } | undefined {
  if (!workspace) return undefined;
  const bare = workspace.trim().replace(/[\\/]+$/, '');
  if (!bare || bare === '.' || bare === '..') return undefined;
  const parts = bare.split(/[\\/]/).filter(part => part.length > 0 && part !== '.');
  const title = parts[parts.length - 1];
  if (!title || title === '..' || /^[A-Za-z]:$/.test(title)) return undefined;
  return { key: bare, title };
}

/**
 * The group suggestion for these tabs, or nothing.
 * A tab counts when it is unpinned, it is not a member of a group in `groups` (a leftover group id is
 * not membership), and its session's world row has a workspace folder. Tabs are gathered by that
 * folder. The first folder in strip order with at least {@link suggestionMinimum} tabs is the suggestion,
 * unless that exact id set is in `dismissed`. A dismissed set yields the next folder, not silence for
 * every folder. `dismissed` is this app session's memory only; nothing here is stored.
 */
export function suggestGroup(
  tabs: readonly Tab[],
  groups: readonly TabGroup[],
  rowsBySession: Rows,
  dismissed: ReadonlySet<string>,
): GroupSuggestion | undefined {
  const knownGroups = new Set(groups.map(group => group.id));
  const seen = new Set<string>();
  const buckets = new Map<string, { title: string; ids: string[] }>();
  const order: string[] = [];
  for (const tab of tabs) {
    if (seen.has(tab.id) || tab.pinned || (tab.groupId !== undefined && knownGroups.has(tab.groupId))) continue;
    const session = sessionOf(tab);
    const folder = session ? workspaceFolder(rowFor(rowsBySession, session)?.workspace) : undefined;
    if (!folder) continue;
    seen.add(tab.id);
    let bucket = buckets.get(folder.key);
    if (!bucket) {
      bucket = { title: folder.title, ids: [] };
      buckets.set(folder.key, bucket);
      order.push(folder.key);
    }
    bucket.ids.push(tab.id);
  }
  for (const key of order) {
    const bucket = buckets.get(key);
    if (!bucket || bucket.ids.length < suggestionMinimum) continue;
    if (dismissed.has(suggestionSetKey(bucket.ids))) continue;
    return { ids: bucket.ids, title: bucket.title };
  }
  return undefined;
}
