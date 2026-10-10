// The workspace's read of background work: closed tabs whose work goes on, and open tabs that need the person.
import type { WorldRow, AttentionItem as WorldAttention } from '../../chat/world-client.ts';
import type { WorldState } from '../../chat/world-store.ts';
import { chatIdFromSessionFile } from '../../places/client.ts';
import { panesOf, type Pane, type Tab } from '../model.ts';
import { paneRunning, type Summaries } from './running.ts';

/**
 * One Inbox row. `id` is the row's own key. `tabId` is set when this window has a tab for the conversation (open or
 * closed), so a click can go there; without it the conversation lives only in the engine and a click needs `openChat`.
 * `stale` marks a row read from the world feed while that feed is unreachable: it is the last thing seen, not the present.
 */
type Row = { id: string; title: string; chatId?: string; tabId?: string; stale?: boolean };
export type BackgroundItem = Row & { state: 'running' | 'waiting'; since?: number };
/** `asked` is when the question was put (ms), from the engine's own stamp; absent when nothing says. */
export type NeedsYouItem = Row & { text?: string; asked?: number };
export type BackgroundWork = {
  running: readonly BackgroundItem[];
  needsYou: readonly NeedsYouItem[];
  /** One muted line when what is listed may be out of date; absent while everything shown is current. */
  notice?: string;
};

const stateOf = (tab: Tab, summaries: Summaries): BackgroundItem['state'] | undefined => {
  const marks = panesOf(tab).map(pane => (paneRunning(summaries, pane.id) ? summaries[pane.id]?.mark : undefined));
  return marks.includes('waiting') ? 'waiting' : marks.includes('working') ? 'running' : undefined;
};

/** Closed tabs still being worked on, newest close first. `since` is when this window saw the tab close. */
export function backgroundItems(closed: readonly Tab[], summaries: Summaries, since: Readonly<Record<string, number>>): BackgroundItem[] {
  return closed.flatMap(tab => {
    const state = stateOf(tab, summaries);
    return state ? [{ id: tab.id, tabId: tab.id, title: tab.title, state, since: since[tab.id] }] : [];
  }).reverse();
}

/** Open tabs that wait on the person. The Inbox itself never lists itself. */
export function needsYouItems(tabs: readonly Tab[], summaries: Summaries): NeedsYouItem[] {
  return tabs.filter(tab => panesOf(tab).some(pane => summaries[pane.id]?.mark === 'waiting')).map(tab => ({ id: tab.id, tabId: tab.id, title: tab.title }));
}

/** How many of the newest closed tabs are read once after a reload, to learn whether their work still goes on. */
const unknownCheckLimit = 5;

/**
 * Closed panes the workspace keeps reading: the ones last seen running (so the Inbox follows them to the end) and
 * the newest few nothing is known about yet (a reload forgets summaries). A pane read once with no mark drops out.
 */
export function observedClosedPanes(closed: readonly Tab[], summaries: Summaries): Pane[] {
  const recent = new Set(closed.slice(-unknownCheckLimit).map(tab => tab.id));
  return closed.flatMap(tab => panesOf(tab).filter(pane => pane.sessionFile && (paneRunning(summaries, pane.id) || (recent.has(tab.id) && !summaries[pane.id]))));
}

/** Compact age for a background row: "now" under a minute, then 2m, 3h, 4d. */
export function compactAge(ms: number): string {
  const minutes = Math.floor(ms / 60000);
  if (minutes < 1) return 'now';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h` : `${Math.floor(hours / 24)}d`;
}

/** A conversation counts as a recent failure for this long after its last activity; older ones are history, not attention. */
export const recentFailureMs = 3 * 24 * 60 * 60 * 1000;
export const staleNotice = 'Cannot reach the engine. Showing what was last seen.';

const askedAt = (asked?: string): { asked?: number } => {
  const at = asked ? Date.parse(asked) : NaN;
  return Number.isFinite(at) ? { asked: at } : {};
};

/** Questions with a known time come first, oldest first (the one that has waited longest); the rest keep their order after them. */
export const oldestFirst = <T extends { asked?: number }>(items: readonly T[]): T[] => [
  ...items.filter(item => item.asked !== undefined).sort((a, b) => a.asked! - b.asked!),
  ...items.filter(item => item.asked === undefined),
];

type Build = {
  tabs: readonly Tab[];
  closed: readonly Tab[];
  summaries: Summaries;
  /** When this window saw each closed tab close. */
  since: Readonly<Record<string, number>>;
  /** Closed tabs whose stop was asked for: they leave the list at once. */
  stopping: ReadonlySet<string>;
  world: Pick<WorldState, 'status' | 'rows' | 'items'>;
};

const chatsOf = (tab: Tab) => panesOf(tab).flatMap(pane => (pane.sessionFile ? [chatIdFromSessionFile(pane.sessionFile)] : [])).filter(Boolean);

/**
 * The Inbox's content, from two real sources and no others: the reads this window makes of its own tabs, and the
 * engine-wide world feed. A conversation this window has a tab for is described by that tab; the feed adds what the
 * window cannot see (work in other windows, or in no window) and fills in a closed tab it has not read yet.
 */
export function buildBackgroundWork(input: Build): BackgroundWork {
  const { tabs, closed, summaries, since, stopping, world } = input;
  const stale = world.status === 'unavailable' && world.rows.length > 0;
  const rows = new Map<string, WorldRow>(world.rows.filter(row => !row.archived && !row.deletionPending).map(row => [row.session, row] as const));
  const openChats = new Map<string, Tab>(tabs.flatMap(tab => chatsOf(tab).map(chat => [chat, tab] as const)));
  const closedChats = new Map<string, Tab>(closed.filter(tab => !stopping.has(tab.id)).flatMap(tab => chatsOf(tab).map(chat => [chat, tab] as const)));

  // Closed tabs: this window's own reads first; the feed answers for a tab nothing has been read for yet (a reload forgets).
  const fromTabs = backgroundItems(closed.filter(tab => !stopping.has(tab.id)), summaries, since);
  const known = new Set(fromTabs.map(item => item.id));
  const fromFeed: BackgroundItem[] = closed.filter(tab => !stopping.has(tab.id) && !known.has(tab.id) && !panesOf(tab).some(pane => summaries[pane.id])).flatMap(tab => {
    const row = chatsOf(tab).map(chat => rows.get(chat)).find(r => r && (r.running || r.needsYou));
    return row ? [{ id: tab.id, title: tab.title, tabId: tab.id, chatId: row.session, state: row.needsYou ? 'waiting' as const : 'running' as const, since: since[tab.id], ...(stale ? { stale } : {}) }] : [];
  });
  // Conversations with no tab here at all: running in another window, or in none.
  // A row that names no conversation is not that work. The session mirror the inactive tabs
  // read has a chat id and no session, and listing it would pin an Inbox for a chat this window already has.
  const elsewhere: BackgroundItem[] = [...rows.values()].filter(row => row.session && row.running && !row.needsYou && !openChats.has(row.session) && !closedChats.has(row.session))
    .map(row => ({ id: `chat:${row.session}`, title: row.title, chatId: row.session, state: 'running' as const, ...(stale ? { stale } : {}) }));

  const waiting: NeedsYouItem[] = world.items.filter((item: WorldAttention) => !openChats.has(item.session) && !closedChats.has(item.session))
    .map(item => ({ id: `chat:${item.session}:${item.key}`, title: item.title || rows.get(item.session)?.title || 'Conversation', chatId: item.session, text: item.text, ...askedAt(item.asked), ...(stale ? { stale } : {}) }));

  return {
    running: [...fromTabs, ...fromFeed, ...elsewhere],
    needsYou: oldestFirst([...needsYouItems(tabs, summaries), ...waiting]),
    ...(stale ? { notice: staleNotice } : {}),
  };
}
