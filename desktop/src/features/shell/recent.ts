// The rail's Recent section, as data. There is ONE store of recent things: the workspace's `closed` list, the same
// list ⌘⇧T reopens from and ⌘T searches. This module only reads it into the five rows the rail draws and hands them
// across the window (the workspace owns its tabs; the rail is in App), so a second list can never drift from it.
import { useSyncExternalStore } from 'react';
import type { IconName } from '../../components/ui';
import { panesOf } from '../tabs/helpers.ts';
import { shortAge } from '../tabs/kinds/newtab/rows.ts';
import type { ClosedTab, Tab } from '../tabs/types.ts';

/** How many things the section shows. */
export const RECENT_LIMIT = 5;

export type RecentMark = 'working' | 'waiting' | 'failed';

export type RecentItem = {
  id: string;
  title: string;
  icon: IconName;
  /** Milliseconds since the epoch when the tab was closed. A save from before the stamp has none, and the row then shows no age. */
  closedAt?: number;
  /** The place a Home tab belongs to, else the first place the tab was filed in. */
  placeId?: string;
  mark?: RecentMark;
};

/** The only two things about a tab the model cannot know without the renderers: its glyph and its live state. */
export type DescribeTab = (tab: Tab) => { icon: IconName; mark?: RecentMark };

const placeOfTab = (tab: Tab) => (tab.kind === 'home' ? (tab.place === 'root' ? undefined : tab.place) : tab.memberOf?.[0]);
const sessionsOf = (tab: Tab) => panesOf(tab).flatMap(pane => (pane.sessionFile ? [pane.sessionFile] : []));

/**
 * The newest closed tabs, newest first. A conversation that is open again (reopened through History, say, which gives
 * it a new tab) is not "recent", and a conversation closed twice is listed once, at its latest close.
 */
export function recentItems(closed: readonly ClosedTab[], open: readonly Tab[], describe: DescribeTab): RecentItem[] {
  const seen = new Set(open.flatMap(sessionsOf));
  const items: RecentItem[] = [];
  for (const tab of [...closed].reverse()) {
    const sessions = sessionsOf(tab);
    if (sessions.some(file => seen.has(file))) continue;
    sessions.forEach(file => seen.add(file));
    items.push({ id: tab.id, title: tab.title, closedAt: tab.closedAt, placeId: placeOfTab(tab), ...describe(tab) });
    if (items.length === RECENT_LIMIT) break;
  }
  return items;
}

/** The age at rest, in the rail's own few characters: `now`, `5m`, `3h`, `2d`. Unknown is nothing. */
export function recentAge(closedAt: number | undefined, now: number): string | undefined {
  if (closedAt === undefined) return undefined;
  // The new-tab search says "5m ago"; one clock serves both, and the rail drops the word it has no room for.
  const age = shortAge(closedAt, now);
  return age === 'just now' ? 'now' : age.replace(' ago', '');
}

const collapsedKey = (label: string) => `codeaf.desktop.window.${label}.rail-recent-collapsed`;

/** Collapsed or not, per window. Anything but the literal `1` reads as open, so a damaged value falls back safely. */
export function readRecentCollapsed(label: string, storage: Pick<Storage, 'getItem'> = localStorage): boolean {
  try { return storage.getItem(collapsedKey(label)) === '1'; } catch { return false; }
}
export function writeRecentCollapsed(label: string, collapsed: boolean, storage: Pick<Storage, 'setItem' | 'removeItem'> = localStorage) {
  try { if (collapsed) storage.setItem(collapsedKey(label), '1'); else storage.removeItem(collapsedKey(label)); } catch { /* Not remembering a fold is not worth an interruption. */ }
}

let current: readonly RecentItem[] = [];
const listeners = new Set<() => void>();
const same = (a: readonly RecentItem[], b: readonly RecentItem[]) => a.length === b.length && a.every((item, i) => JSON.stringify(item) === JSON.stringify(b[i]));

/** The workspace says what is recent now. An unchanged list does not wake the rail. */
export function publishRecent(items: readonly RecentItem[]) {
  if (same(current, items)) return;
  current = items;
  listeners.forEach(listener => listener());
}
export function useRecent(): readonly RecentItem[] {
  return useSyncExternalStore(listener => { listeners.add(listener); return () => { listeners.delete(listener); }; }, () => current);
}
