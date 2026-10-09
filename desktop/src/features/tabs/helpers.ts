// Pure helpers shared by the reducer slices, the persistence reader and the views.
import { cleanView } from './view-state.ts';
import { defaultKind } from './kinds/types.ts';
import type { Pane, Split, SplitLayout, SplitRatios, Tab, TabGroup, TitleSource, WorkspaceState } from './types.ts';

/** Replaceable so tests are deterministic. */
export let createId: () => string = () => crypto.randomUUID();
export const setIdSource = (source: () => string) => { createId = source; };

export const newConversationTitle = 'New conversation';

// A title only replaces one of equal or lower rank: the person's name always wins.
export const titleRank: Record<TitleSource, number> = { message: 1, engine: 2, manual: 3 };
export const rankOf = (tab: { titleSource?: TitleSource }) => (tab.titleSource ? titleRank[tab.titleSource] : 0);

export const splitCapacity = 4;
export const splitMinimum = 2;

/** How many panes a layout draws. The 2x2 grid draws three (the third spans the bottom row) or four. */
export const layoutFits = (layout: SplitLayout, count: number) => (layout === '2x2' ? count >= 3 && count <= splitCapacity : count === splitMinimum);
export const defaultLayout = (count: number): SplitLayout => (count > splitMinimum ? '2x2' : '1x2');

export function newTab(init: Partial<Tab> = {}): Tab {
  return { id: createId(), title: newConversationTitle, draft: '', pinned: false, ...init, kind: init.kind ?? defaultKind };
}

export function initialWorkspace(): WorkspaceState {
  const tab = newTab();
  return { tabs: [tab], groups: [], activeId: tab.id, closed: [], nextNumber: 2, recentIds: [tab.id] };
}

/** The panes a tab shows: its split's panes, or itself as the one pane. */
export function panesOf(tab: Tab): Pane[] {
  return tab.split ? tab.split.panes : [tab];
}

export const isSplit = (tab: Tab): boolean => !!tab.split;
export const focusedPane = (tab: Tab): Pane => (tab.split ? tab.split.panes[tab.split.focus] ?? tab.split.panes[0] : tab);

/** The fields a pane owns, lifted off a tab. */
export function paneOf(tab: Tab): Pane {
  const base: Pane = { id: tab.id, kind: tab.kind, title: tab.title, draft: tab.draft, titleSource: tab.titleSource };
  return { ...base, ...cleanView(tab as unknown as Record<string, unknown>) };
}

/** A pane promoted back to a plain tab (pin and group come from the tab it leaves). */
export function tabOf(pane: Pane, over: Partial<Tab> = {}): Tab {
  return { ...pane, pinned: false, ...over };
}

export function splitTitle(panes: readonly Pane[]): string {
  return panes.map(pane => pane.title).join(' · ');
}

/** A split tab's own title follows its panes unless the person named it. */
export function withSplitTitle(tab: Tab): Tab {
  if (!tab.split || tab.titleSource === 'manual') return tab;
  const title = splitTitle(tab.split.panes);
  return title === tab.title ? tab : { ...tab, title };
}

export function makeSplit(panes: Pane[], focus: number, layout: SplitLayout = defaultLayout(panes.length), ratios?: SplitRatios): Split {
  return { layout: layoutFits(layout, panes.length) ? layout : defaultLayout(panes.length), focus: Math.min(Math.max(focus, 0), panes.length - 1), panes, ...(ratios ? { ratios } : {}) };
}

/** The share a divider may take: never so small that a pane is lost. */
export const splitRatioMin = 0.2;
export const clampRatio = (value: number) => Math.min(1 - splitRatioMin, Math.max(splitRatioMin, value));

/** Applies `change` to the tab or pane with this id; a pane change re-derives its split tab's title. */
export function mapContent(state: WorkspaceState, id: string, change: (content: Pane) => Pane): WorkspaceState {
  let touched = false;
  const tabs = state.tabs.map(tab => {
    if (tab.id === id) { touched = true; return withSplitTitle({ ...tab, ...change(tab) }); }
    const split = tab.split;
    if (!split || !split.panes.some(pane => pane.id === id)) return tab;
    touched = true;
    return withSplitTitle({ ...tab, split: { ...split, panes: split.panes.map(pane => (pane.id === id ? { ...pane, ...change(pane) } : pane)) } });
  });
  return touched ? { ...state, tabs } : state;
}

/** The tab that holds this id, as a tab id or as one of a split's pane ids. */
export function tabHolding(state: WorkspaceState, id: string): Tab | undefined {
  return state.tabs.find(tab => tab.id === id || tab.split?.panes.some(pane => pane.id === id));
}

export function normalize(state: WorkspaceState): WorkspaceState {
  return {
    ...state,
    recentIds: [...new Set([state.activeId, ...state.recentIds.filter(id => state.tabs.some(t => t.id === id)), ...state.tabs.map(t => t.id)])],
    groups: state.groups.filter(g => state.tabs.some(t => t.groupId === g.id)),
  };
}

/** The strip's reading order: pinned, then ungrouped, then each group; a collapsed group keeps only its active member. */
export function visibleTabs(state: Pick<WorkspaceState, 'tabs' | 'groups' | 'activeId'>): Tab[] {
  const { tabs, groups, activeId } = state;
  return [
    ...tabs.filter(t => t.pinned),
    ...tabs.filter(t => !t.pinned && !t.groupId),
    ...groups.flatMap(g => tabs.filter(t => t.groupId === g.id && (!g.collapsed || t.id === activeId))),
  ];
}

export function nextGroupTitle(groups: readonly TabGroup[]): string {
  const names = new Set(groups.map(group => group.title.toLocaleLowerCase()));
  let number = 1;
  while (names.has(number === 1 ? 'new group' : `new group ${number}`)) number++;
  return number === 1 ? 'New group' : `New group ${number}`;
}
