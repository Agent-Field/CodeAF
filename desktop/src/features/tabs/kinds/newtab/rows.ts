// The rows of the new-tab field (design 3f, 4c): pure, so a node test covers the ordering and filtering.
// The first row always turns the typed text into a conversation; every other row is a way to jump or open.
import type { IconName } from '../../../../components/ui/Icon';
import type { Tab } from '../../types.ts';

export type RowKind = 'ask' | 'terminal' | 'openfile' | 'file' | 'tab' | 'closed';
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
  /** Payload: a tab id, a closed tab id or a file path. */
  target?: string;
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
};

export const askLimit = 40;
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
  if (query) sections.push({ rows: [{ id: 'ask', kind: 'ask', icon: 'tab', label: askLabel(query), hint: '↵' }] });
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
