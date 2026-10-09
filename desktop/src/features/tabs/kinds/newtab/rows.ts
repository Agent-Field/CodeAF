// The rows of the new-tab field (design 3f, 4c): pure, so a node test covers the ordering and filtering.
// The first row always turns the typed text into a conversation; every other row is a way to jump or open.
import type { IconName } from '../../../../components/ui/Icon';
import type { HistoryItem } from '../../../history/types.ts';
import type { Tab } from '../../types.ts';
import { addressParts, looksLikeAddress, toAddress } from '../../../web/address.ts';

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
  closed: Tab[];
  files: FileHit[];
  /** True while the terminal kind is backed by the engine; otherwise the row is absent, not broken. */
  terminal: boolean;
  terminalShortcut: string;
  fileShortcut: string;
  /** What History found for the query, when it has answered for exactly this query. */
  history?: HistoryMatches;
  seeAllShortcut?: string;
};

export const askLimit = 40;

/** What the first row says for an address: the site and path as the web tab will show them. */
export function webLabel(url: string): string {
  const { site, rest } = addressParts(url);
  return `Open ${clip(`${site}${rest}`, askLimit)} in a web tab`;
}
const clip = (text: string, limit: number) => (text.length <= limit ? text : `${text.slice(0, limit - 1).trimEnd()}…`);
const firstLine = (text: string) => text.trim().split('\n')[0];

/** The first row: the typed text, clipped to one line and quoted the way the design quotes it. */
export const askLabel = (query: string) => `Ask “${clip(firstLine(query), askLimit)}” in a new conversation`;

/** The title a conversation started from the field takes: the first line of what was typed. */
export const titleFromText = (text: string): string => clip(firstLine(text), askLimit);

const has = (title: string, query: string) => title.toLowerCase().includes(query.toLowerCase());

export function buildSections(input: RowInput): NewTabSection[] {
  const query = input.query.trim();
  const sections: NewTabSection[] = [];
  // An address is offered as a page first; the conversation row stays one arrow away for a URL that is a question.
  const address = query && looksLikeAddress(query) ? toAddress(query) : undefined;
  const web: NewTabRow[] = address && 'url' in address ? [{ id: 'web', kind: 'web', icon: 'web', label: webLabel(address.url), hint: '↵', target: address.url }] : [];
  if (query) sections.push({ rows: [...web, { id: 'ask', kind: 'ask', icon: 'tab', label: askLabel(query), hint: web.length ? undefined : '↵' }] });
  const found = input.history;
  if (found && found.query === query && (found.rows.length || found.total > 0)) {
    const rows = found.rows.map((match): NewTabRow => ({ id: `history:${match.id}`, kind: 'history', icon: 'tab', label: match.item.title, detail: historyDetail(match), target: match.id, conversation: match.item }));
    rows.push({ id: 'seeall', kind: 'seeall', icon: 'history', label: `See all ${found.total} in History`, hint: input.seeAllShortcut });
    sections.push({ title: 'From history', rows });
  }
  const start: NewTabRow[] = [
    ...(input.terminal ? [{ id: 'terminal', kind: 'terminal' as const, icon: 'terminal' as const, label: 'New terminal', hint: input.terminalShortcut }] : []),
    { id: 'openfile', kind: 'openfile', icon: 'findFiles', label: 'Open file…', hint: input.fileShortcut },
  ];
  sections.push({ title: 'Start', rows: start });
  if (!query) return sections;
  const matching: NewTabRow[] = [
    ...input.files.map((file): NewTabRow => ({ id: `file:${file.path}`, kind: 'file', icon: 'fileCode', label: file.name, detail: file.dir, target: file.path })),
    ...input.tabs.filter(item => has(item.tab.title, query)).map((item): NewTabRow => ({ id: `tab:${item.tab.id}`, kind: 'tab', icon: 'tab', label: item.tab.title, detail: 'open tab', hint: item.shortcut, dot: item.waiting, target: item.tab.id })),
    ...input.closed.filter(tab => tab.kind !== 'newtab' && has(tab.title, query)).reverse().map((tab): NewTabRow => ({ id: `closed:${tab.id}`, kind: 'closed', icon: 'history', label: tab.title, detail: 'closed', target: tab.id })),
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
