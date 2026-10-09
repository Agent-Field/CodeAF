// The overview's order and filter, pure. Grid and filmstrip read the same sections, so they never disagree.
import type { Tab, TabGroup } from './types.ts';

export type OverviewSection =
  | { kind: 'pinned'; id: 'pinned'; title: string; tabs: Tab[] }
  | { kind: 'group'; id: string; title: string; tabs: Tab[]; group: TabGroup }
  | { kind: 'other'; id: 'other'; title: string; tabs: Tab[] };

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

/** "Trailing commas · 1 of 4": the position inside the cursor's own section. */
export function sectionPosition(sections: readonly OverviewSection[], id: string | undefined): string {
  const section = sections.find(candidate => candidate.tabs.some(tab => tab.id === id));
  return section ? `${section.title} · ${section.tabs.findIndex(tab => tab.id === id) + 1} of ${section.tabs.length}` : '';
}
