// The Inbox's data (design 3l "Inbox, background work"): closed tabs whose work goes on, and open tabs that need the person.
import { panesOf, type Pane, type Tab } from '../model';
import { paneRunning, type Summaries } from './running';

export type BackgroundItem = { id: string; title: string; state: 'running' | 'waiting'; since?: number };
export type NeedsYouItem = { id: string; title: string };
export type BackgroundWork = { running: readonly BackgroundItem[]; needsYou: readonly NeedsYouItem[] };

const stateOf = (tab: Tab, summaries: Summaries): BackgroundItem['state'] | undefined => {
  const marks = panesOf(tab).map(pane => (paneRunning(summaries, pane.id) ? summaries[pane.id]?.mark : undefined));
  return marks.includes('waiting') ? 'waiting' : marks.includes('working') ? 'running' : undefined;
};

/** Closed tabs still being worked on, newest close first. `since` is when this window saw the tab close. */
export function backgroundItems(closed: readonly Tab[], summaries: Summaries, since: Readonly<Record<string, number>>): BackgroundItem[] {
  return closed.flatMap(tab => {
    const state = stateOf(tab, summaries);
    return state ? [{ id: tab.id, title: tab.title, state, since: since[tab.id] }] : [];
  }).reverse();
}

/** Open tabs that wait on the person. The Inbox itself never lists itself. */
export function needsYouItems(tabs: readonly Tab[], summaries: Summaries): NeedsYouItem[] {
  return tabs.filter(tab => tab.kind !== 'inbox' && panesOf(tab).some(pane => summaries[pane.id]?.mark === 'waiting')).map(tab => ({ id: tab.id, title: tab.title }));
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
