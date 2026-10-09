// The pure side of the History tab: grouping by time, what a row says, the virtual list's arithmetic and
// term highlighting. No React, no CSS, no tokens import (the caller passes the metrics), so a node test covers it.
import type { HistoryItem } from './types.ts';

const MINUTE = 60_000;
const DAY = 24 * 60 * MINUTE;
const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'] as const;
const months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'] as const;
const pad = (value: number) => String(value).padStart(2, '0');
const startOfDay = (time: number) => { const day = new Date(time); day.setHours(0, 0, 0, 0); return day.getTime(); };
/** Whole calendar days between two instants, by local date (so 23:50 to 00:10 is one day). */
const daysBetween = (from: number, to: number) => Math.round((startOfDay(to) - startOfDay(from)) / DAY);
/** Monday of the week a time falls in, at local midnight. */
const startOfWeek = (time: number) => { const day = new Date(startOfDay(time)); day.setDate(day.getDate() - ((day.getDay() + 6) % 7)); return day.getTime(); };

export const clock = (time: number) => { const date = new Date(time); return `${pad(date.getHours())}:${pad(date.getMinutes())}`; };

/** The heading a conversation sits under: Today, Yesterday, a weekday of this week, Last week, then months. */
export function groupLabel(at: number, now: number): string {
  const days = daysBetween(at, now);
  if (days <= 0) return 'Today';
  if (days === 1) return 'Yesterday';
  if (startOfWeek(at) === startOfWeek(now)) return weekdays[new Date(at).getDay()];
  if (startOfWeek(at) === startOfWeek(now) - 7 * DAY) return 'Last week';
  const date = new Date(at);
  return date.getFullYear() === new Date(now).getFullYear() ? months[date.getMonth()] : `${months[date.getMonth()]} ${date.getFullYear()}`;
}

/** The short stamp at a row's right edge: a time inside a day-headed group, a weekday under Last week, a date under a month. */
export function rowStamp(at: number, now: number): string {
  const group = groupLabel(at, now);
  const date = new Date(at);
  if (group === 'Last week') return weekdays[date.getDay()].slice(0, 3);
  if (months.some(month => group.startsWith(month))) return `${months[date.getMonth()].slice(0, 3)} ${date.getDate()}`;
  return clock(at);
}

/** A search result's date: Today, a short weekday within the week, or a short date. */
export function resultStamp(at: number, now: number): string {
  const days = daysBetween(at, now);
  if (days <= 0) return 'Today';
  if (days === 1) return 'Yesterday';
  const date = new Date(at);
  if (days < 7) return weekdays[date.getDay()].slice(0, 3);
  return `${months[date.getMonth()].slice(0, 3)} ${date.getDate()}`;
}

/** "Tuesday 14:02": the recap pane's first words and the best match's. */
export function fullStamp(at: number, now: number): string {
  const days = daysBetween(at, now);
  const date = new Date(at);
  const day = days <= 0 ? 'Today' : days === 1 ? 'Yesterday' : days < 7 ? weekdays[date.getDay()] : `${months[date.getMonth()].slice(0, 3)} ${date.getDate()}`;
  return `${day} ${clock(at)}`;
}

const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? '' : 's'}`;

/** The state half of an open conversation's line, or '' when it has nothing to say. */
function stateWords(item: HistoryItem): string {
  if (item.state === 'needs-you') return item.reason ? `waiting on you: ${item.reason}` : 'waiting on you';
  if (item.tasksRunning > 0) return `${plural(item.tasksRunning, 'task')} running`;
  if (item.state === 'working') return 'working';
  return '';
}

/**
 * What a row says under its title (design 4d): one sentence of substance, never a count of messages or a
 * duration. An open conversation leads with its live state; a conversation with no recap yet says nothing.
 */
export function rowLine(item: HistoryItem): string {
  const live = item.open ? stateWords(item) : '';
  if (live) return `Open · ${live}`;
  return item.line?.trim() ?? '';
}

/** The right-edge label: "Open" for a conversation a window holds now, else its stamp. */
export function rowTrail(item: HistoryItem, now: number): string {
  return item.open ? 'Open' : rowStamp(Date.parse(item.at), now);
}

/** Which glyph leads a row: a state dot for live work or a question, the conversation glyph otherwise. */
export type Lead = 'working' | 'needs-you' | 'conversation';
export function rowLead(item: HistoryItem): Lead {
  if (item.state === 'needs-you') return 'needs-you';
  if (item.open && (item.state === 'working' || item.tasksRunning > 0)) return 'working';
  return 'conversation';
}

/** "Tuesday 14:02 · 9 messages · 2 tasks": the recap pane's meta line. */
export function metaLine(item: HistoryItem, now: number): string {
  const parts = [fullStamp(Date.parse(item.at), now)];
  if (item.messages > 0) parts.push(plural(item.messages, 'message'));
  if (item.tasks > 0) parts.push(plural(item.tasks, 'task'));
  if (item.archived) parts.push('archived');
  return parts.join(' · ');
}

/** "who" under a decision: "you", or "codeaf, accepted". */
export function decidedBy(by: string, how?: string): string {
  return [by, how].filter(Boolean).join(', ');
}

// ---- the virtual list ------------------------------------------------------

export type Metrics = { group: number; row: number; rowFiles: number; overscan: number };

export type ListEntry =
  | { kind: 'group'; key: string; label: string; top: number; height: number }
  | { kind: 'row'; key: string; item: HistoryItem; top: number; height: number };

/** The row's height is fixed by what it holds: a row with files has one artifact line, no row has more. */
export const rowHeight = (item: HistoryItem, metrics: Metrics) => (item.files.length > 0 ? metrics.rowFiles : metrics.row);

/** Lays the items out under their time headings with a running top, newest first as given. */
export function layout(items: readonly HistoryItem[], now: number, metrics: Metrics): { entries: ListEntry[]; height: number } {
  const entries: ListEntry[] = [];
  let top = 0;
  let current = '';
  for (const item of items) {
    const label = groupLabel(Date.parse(item.at), now);
    if (label !== current) {
      current = label;
      entries.push({ kind: 'group', key: `group:${label}:${entries.length}`, label, top, height: metrics.group });
      top += metrics.group;
    }
    const height = rowHeight(item, metrics);
    entries.push({ kind: 'row', key: item.id, item, top, height });
    top += height;
  }
  return { entries, height: top };
}

/** The entries to draw for a scroll position: those intersecting the viewport, plus a few either side. */
export function windowOf(entries: readonly ListEntry[], scrollTop: number, viewport: number, overscan: number): { start: number; end: number } {
  if (!entries.length) return { start: 0, end: 0 };
  // The first entry whose bottom is below the top edge, by binary search on the monotonic tops.
  let low = 0;
  let high = entries.length - 1;
  while (low < high) {
    const mid = (low + high) >> 1;
    if (entries[mid].top + entries[mid].height <= scrollTop) low = mid + 1;
    else high = mid;
  }
  let end = low;
  while (end < entries.length && entries[end].top < scrollTop + viewport) end++;
  return { start: Math.max(0, low - overscan), end: Math.min(entries.length, end + overscan) };
}

/** The heading that sticks to the top once its own line has scrolled away: the last group that starts above the viewport. */
export function stickyGroup(entries: readonly ListEntry[], scrollTop: number): { label: string; push: number } | undefined {
  let found = -1;
  let next = -1;
  for (let i = 0; i < entries.length; i++) {
    const entry = entries[i];
    if (entry.kind !== 'group') continue;
    if (entry.top < scrollTop) found = i;
    else { next = i; break; }
  }
  if (found < 0) return undefined;
  const group = entries[found] as Extract<ListEntry, { kind: 'group' }>;
  // The next heading pushes this one up as it arrives, so two never overlap.
  const gap = next >= 0 ? entries[next].top - scrollTop : Number.POSITIVE_INFINITY;
  return { label: group.label, push: Math.min(0, gap - group.height) };
}

/** The index of the entry holding a row id, or -1. */
export const indexOfRow = (entries: readonly ListEntry[], id: string) => entries.findIndex(entry => entry.kind === 'row' && entry.item.id === id);

/** Next or previous row (skipping headings) from an index; clamps at both ends. */
export function stepRow(entries: readonly ListEntry[], from: number, direction: 1 | -1): number {
  for (let i = from + direction; i >= 0 && i < entries.length; i += direction) if (entries[i].kind === 'row') return i;
  return from;
}

// ---- highlighting ----------------------------------------------------------

export type Segment = { text: string; mark: boolean };

const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/**
 * Splits text around the search terms so the caller can wrap the hits. Case-insensitive; empty terms are ignored.
 * Only whole words are marked: the design marks "lexer" and leaves "decided" alone for the term "decide", because a
 * mark that ends mid-word reads as a rendering fault.
 */
export function highlight(text: string, terms: readonly string[]): Segment[] {
  const wanted = [...new Set(terms.map(term => term.trim()).filter(Boolean))].sort((a, b) => b.length - a.length);
  if (!wanted.length || !text) return [{ text, mark: false }];
  const pattern = new RegExp(`(?<![\\p{L}\\p{N}])(${wanted.map(escape).join('|')})(?![\\p{L}\\p{N}])`, 'giu');
  const lower = new Set(wanted.map(term => term.toLowerCase()));
  return text.split(pattern).filter(Boolean).map(part => ({ text: part, mark: lower.has(part.toLowerCase()) }));
}

/** The content words of a query: what the engine marks and the page highlights. Stop words drop out. */
const stop = new Set(['a', 'an', 'the', 'and', 'or', 'of', 'to', 'in', 'on', 'for', 'about', 'what', 'did', 'we', 'do', 'does', 'is', 'are', 'was', 'how', 'why', 'with', 'it', 'i', 'me', 'my']);
export function queryTerms(query: string): string[] {
  return query.toLowerCase().split(/[^\p{L}\p{N}_.-]+/u).filter(word => word.length > 1 && !stop.has(word));
}

// ---- archive and ids -------------------------------------------------------

/** The history id of a conversation: the folder its transcript lives in (the engine names the id after it). */
export function historyIdOf(sessionFile: string): string {
  const parts = sessionFile.split(/[\\/]/).filter(Boolean);
  const name = parts[parts.length - 1] ?? '';
  if (parts.length >= 2 && /^transcript\.jsonl$/.test(name)) return parts[parts.length - 2];
  return name.replace(/\.jsonl$/, '');
}

export const IDLE_ARCHIVE_MS = 12 * 60 * MINUTE;

/** What the workspace remembers about a tab so idleness survives a restart. `hold` is true when it was pinned, running or waiting on the person. */
export type TabActivity = { at: number; hold: boolean };

export type ArchiveCandidate = { id: string; kind: string; pinned: boolean; hasSession: boolean };

/**
 * The tabs that archive themselves at launch (design 4d): idle for 12 hours, and neither pinned, running nor
 * waiting on the person. The active tab and a tab with no conversation behind it stay.
 */
export function idleTabs(tabs: readonly ArchiveCandidate[], activeId: string, activity: Readonly<Record<string, TabActivity | undefined>>, now: number, after = IDLE_ARCHIVE_MS): string[] {
  return tabs
    .filter(tab => tab.id !== activeId && !tab.pinned && tab.hasSession && (tab.kind === 'conversation' || tab.kind === 'task'))
    .filter(tab => { const seen = activity[tab.id]; return !!seen && !seen.hold && now - seen.at >= after; })
    .map(tab => tab.id);
}
