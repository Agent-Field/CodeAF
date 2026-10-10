// The overview's order and filter, pure. Grid and filmstrip read the same sections, so they never disagree.
import type { TabSummary } from '../conversation/tabSummary.ts';
import { focusedPane, panesOf } from './helpers.ts';
import { stateOf, type CardState, type FieldLine } from './preview/content.ts';
import type { Tab, TabGroup } from './types.ts';
import { routeTask } from './view-state.ts';

export type OverviewSection =
  | { kind: 'pinned'; id: 'pinned'; title: string; tabs: Tab[] }
  | { kind: 'group'; id: string; title: string; tabs: Tab[]; group: TabGroup }
  | { kind: 'other'; id: 'other'; title: string; tabs: Tab[] };

/**
 * Interactions "Overview card · ⌘-click / middle: Background tab". A press is a background press when it is a
 * middle-button press or a click with the platform's primary modifier (⌘ on a Mac, Ctrl elsewhere; Control on a Mac
 * is the context menu). Every card is a tab that is already open, so a background press opens nothing.
 */
export const isBackgroundPress = (event: { button: number; metaKey: boolean; ctrlKey: boolean }, mac: boolean): boolean => (
  event.button === 1 || (mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey)
);

export const matchesQuery = (haystack: string, query: string) => haystack.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());

/**
 * Pinned tabs first, then one section per group in strip order, then the tabs with no group. The titles are
 * "Pinned", the group's name and "Other tabs" (design 3h). `haystack` is what a tab is searched by; an empty
 * query keeps every tab, and a section with no match is dropped.
 */
export function overviewSections(tabs: readonly Tab[], groups: readonly TabGroup[], query: string, haystack: (tab: Tab) => string): OverviewSection[] {
  const kept = query.trim() ? tabs.filter(tab => matchesQuery(haystack(tab), query)) : tabs;
  const sections: OverviewSection[] = [
    { kind: 'pinned', id: 'pinned', title: 'Pinned', tabs: kept.filter(tab => tab.pinned) },
    ...groups.map((group): OverviewSection => ({ kind: 'group', id: group.id, title: group.title, group, tabs: kept.filter(tab => !tab.pinned && tab.groupId === group.id) })),
    { kind: 'other', id: 'other', title: 'Other tabs', tabs: kept.filter(tab => !tab.pinned && !tab.groupId) },
  ];
  return sections.filter(section => section.tabs.length);
}

export const overviewOrder = (sections: readonly OverviewSection[]): Tab[] => sections.flatMap(section => section.tabs);

/** "Search 18 tabs" */
export const searchPlaceholder = (count: number) => `Search ${count} ${count === 1 ? 'tab' : 'tabs'}`;

/** Where the cursor sits after a move: wraps at both ends; a cursor that left the list lands on the first card. */
export function moveCursor(ids: readonly string[], current: string | undefined, step: number): string | undefined {
  if (!ids.length) return undefined;
  const index = ids.indexOf(current ?? '');
  return index < 0 ? ids[0] : ids[(index + step + ids.length) % ids.length];
}

const taskIdOf = (pane: { kind: string; route?: { taskId?: string; back: string[]; forward: string[] } }) => (
  pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined
);

/**
 * The header state of one overview card. A conversation says "N running" when the engine sent a running-task count,
 * and otherwise Needs you, Failed or Working. A task says "step N" only when that task's live step was sent; otherwise
 * its own state word. A split takes the most urgent pane. Nothing is returned when the engine sent none of these.
 */
export function overviewCardState(tab: Tab, summaries: Readonly<Record<string, TabSummary>>): CardState {
  const states = panesOf(tab).map(pane => {
    const summary = summaries[pane.id];
    const taskId = taskIdOf(pane);
    const state = stateOf(summary, taskId);
    const step = taskId ? summary?.taskLive?.[taskId]?.step : undefined;
    // "step 7" replaces "Running" only. A finished task keeps the word the engine sent.
    if (step && !state.lead && (state.words === 'Running' || state.words === undefined)) return { dot: 'accent' as const, words: `step ${step}` };
    return state;
  });
  const rank = (state: CardState) => (state.lead === 'amber' ? 0 : state.lead === 'danger' ? 1 : state.words ? 2 : 3);
  return states.reduce((best, state) => (rank(state) < rank(best) ? state : best), {} as CardState);
}

/**
 * The one live command a task card may quote. The engine sends at most the command in progress; a second line is not
 * invented to match the specimen. The leading "$ " is the shell prompt the overview draws in front of that command.
 */
export function liveCommandLines(tab: Tab, summaries: Readonly<Record<string, TabSummary>>): FieldLine[] {
  if (tab.split) return [];
  const pane = focusedPane(tab);
  const taskId = taskIdOf(pane);
  const command = taskId ? summaries[pane.id]?.taskLive?.[taskId]?.command?.trim() : '';
  if (!command) return [];
  return [{ text: command.startsWith('$') ? command : `$ ${command}`, tone: 'ink' }];
}

/** "Trailing commas · 1 of 4": the position inside the cursor's own section. */
export function sectionPosition(sections: readonly OverviewSection[], id: string | undefined): string {
  const section = sections.find(candidate => candidate.tabs.some(tab => tab.id === id));
  return section ? `${section.title} · ${section.tabs.findIndex(tab => tab.id === id) + 1} of ${section.tabs.length}` : '';
}
