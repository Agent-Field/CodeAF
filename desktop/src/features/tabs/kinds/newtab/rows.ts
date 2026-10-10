// The rows of the new-tab field (design 3f, 4c): pure, so a node test covers the ordering and filtering.
// The first row always turns the typed text into a conversation; every other row is a way to jump or open.
import type { IconName } from '../../../../components/ui/Icon';
import type { HistoryItem } from '../../../history/types.ts';
import { closedAtOf } from '../../reducers/closing.ts';
import type { Tab } from '../../types.ts';
import { webRows } from './urlRow.ts';
export { webLabel } from './urlRow.ts';

export type RowKind = 'ask' | 'history' | 'seeall' | 'web' | 'terminal' | 'openfile' | 'file' | 'tab' | 'closed';
import { historyDetail, type HistoryMatches } from './historyRows.ts';
export type NewTabRow = {
  id: string;
  kind: RowKind;
  icon: IconName;
  /** Drawn with the query set in medium weight, then `detail` muted. */
  label: string;
  detail?: string;
  hint?: string;
  /** Needs-you tabs draw the amber dot where the icon would be. */
  dot?: boolean;
  /** Payload: a tab id, a closed tab id, a file path or a web address. */
  target?: string;
  /** A history row's conversation, already read, so opening it needs no second request. */
  conversation?: HistoryItem;
};
export type NewTabSection = { title?: string; rows: NewTabRow[] };

export type FileHit = { path: string; name: string; dir: string };
export type RowInput = {
  query: string;
  /** Tabs in strip order, without the field's own tab. */
  tabs: { tab: Tab; shortcut?: string; waiting: boolean }[];
  /** Closed tabs, newest last. `closedAt` is optional: a save from before the stamp has none. */
  closed: (Tab & { closedAt?: number })[];
  /** Clock for the closed-row age. The field omits it and the rows use the moment they are built. */
  now?: number;
  files: FileHit[];
  /** True while the terminal kind is backed by the engine; otherwise the row is absent, not broken. */
  terminal: boolean;
  /** True while the web kind is backed (the desktop app); otherwise an address is only a question. */
  web: boolean;
  terminalShortcut: string;
  fileShortcut: string;
  /** What History found for the query, when it has answered for exactly this query. */
  history?: HistoryMatches;
  seeAllShortcut?: string;
};

export const askLimit = 40;

const clip = (text: string, limit: number) => (text.length <= limit ? text : `${text.slice(0, limit - 1).trimEnd()}…`);
const firstLine = (text: string) => text.trim().split('\n')[0];

/** The first row: the typed text, clipped to one line and quoted the way the design quotes it. */
export const askLabel = (query: string) => `Ask “${clip(firstLine(query), askLimit)}” in a new conversation`;

/** The title a conversation started from the field takes: the first line of what was typed. */
export const titleFromText = (text: string): string => clip(firstLine(text), askLimit);

const has = (title: string, query: string) => title.toLowerCase().includes(query.toLowerCase());

/** The compact age Shell 3f draws ("1h ago"): a row is 36px and the age shares it with a title, so the long form would crowd it. */
export function shortAge(at: number, now: number): string {
  const seconds = Math.max(0, Math.floor((now - at) / 1000));
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

/** Shell 3f: "closed 1h ago". No stamp (an old save) says "closed" and nothing more. */
function closedDetail(tab: { closedAt?: number }, now: number): string {
  const at = closedAtOf(tab.closedAt);
  return at === undefined ? 'closed' : `closed ${shortAge(at, now)}`;
}

export function buildSections(input: RowInput): NewTabSection[] {
  const query = input.query.trim();
  const sections: NewTabSection[] = [];
  // An address is offered as a page first while web is backed; the conversation row stays one arrow away for a URL that is a question.
  const web = webRows(query, input.web);
  if (query) sections.push({ rows: [...web, { id: 'ask', kind: 'ask', icon: 'tab', label: askLabel(query), hint: web.length ? undefined : '↵' }] });
  const found = input.history;
  if (found && found.query === query && (found.rows.length || found.total > 0)) {
    const rows = found.rows.map((match): NewTabRow => ({ id: `history:${match.id}`, kind: 'history', icon: 'tab', label: match.item.title, detail: historyDetail(match), target: match.id, conversation: match.item }));
    rows.push({ id: 'seeall', kind: 'seeall', icon: 'history', label: `See all ${found.total} in History`, hint: input.seeAllShortcut });
    sections.push({ title: 'From history', rows });
  }
  // Shell 3f Start. New terminal is drawn only while that kind is backed: an unbacked row would be a control that cannot work.
  // Choosing it turns this tab into a terminal. ⌃` itself belongs to the terminal key, not to this row.
  const start: NewTabRow[] = [
    ...(input.terminal ? [{ id: 'terminal', kind: 'terminal' as const, icon: 'terminal' as const, label: 'New terminal', hint: input.terminalShortcut }] : []),
    { id: 'openfile', kind: 'openfile', icon: 'findFiles', label: 'Open file…', hint: input.fileShortcut },
  ];
  sections.push({ title: 'Start', rows: start });
  if (!query) return sections;
  const now = input.now ?? Date.now();
  const matching: NewTabRow[] = [
    ...input.files.map((file): NewTabRow => ({ id: `file:${file.path}`, kind: 'file', icon: 'fileCode', label: file.name, detail: file.dir, target: file.path })),
    ...input.tabs.filter(item => has(item.tab.title, query)).map((item): NewTabRow => ({ id: `tab:${item.tab.id}`, kind: 'tab', icon: 'tab', label: item.tab.title, detail: 'open tab', hint: item.shortcut, dot: item.waiting, target: item.tab.id })),
    ...input.closed.filter(tab => tab.kind !== 'newtab' && has(tab.title, query)).reverse().map((tab): NewTabRow => ({ id: `closed:${tab.id}`, kind: 'closed', icon: 'history', label: tab.title, detail: closedDetail(tab, now), target: tab.id })),
  ];
  if (matching.length) sections.push({ title: 'Matching', rows: matching });
  return sections;
}

/** Row order as the keyboard walks it. */
export const flatRows = (sections: NewTabSection[]): NewTabRow[] => sections.flatMap(section => section.rows);

/** The label split around the first case-insensitive occurrence of the query, for the medium-weight match. */
export function splitMatch(label: string, query: string): [string, string, string] {
  const needle = query.trim();
  const at = needle ? label.toLowerCase().indexOf(needle.toLowerCase()) : -1;
  return at < 0 ? [label, '', ''] : [label.slice(0, at), label.slice(at, at + needle.length), label.slice(at + needle.length)];
}

/** 1 to 8 by position; 9 is always the last tab (the same rule useTabKeys applies). */
export const tabDigit = (index: number, count: number): number | undefined => (index < 8 ? index + 1 : index === count - 1 ? 9 : undefined);
