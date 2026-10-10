// Pure helpers shared by the reducer slices, the persistence reader and the views.
import { cleanView } from './view-state.ts';
import { defaultKind } from './kinds/types.ts';
import type { Pane, Split, SplitLayout, SplitRatios, Tab, TabGroup, TitleSource, WorkspaceState } from './types.ts';

/** Replaceable so tests are deterministic. */
export let createId: () => string = () => crypto.randomUUID();
export const setIdSource = (source: () => string) => { createId = source; };

/**
 * Runs `run` with createId answering `ids` in order (then fresh ids), and reports every id handed out. An action that
 * carries the ids its reducer minted (`mint`) therefore produces the same tab set every time it is applied: by the
 * window that recorded it, by an Undo that predicted it, and by a replay over another window's tab set.
 */
export function mintWith<T>(ids: readonly string[] | undefined, run: () => T): { result: T; ids: string[] } {
  const fresh = createId;
  const used: string[] = [];
  let at = 0;
  setIdSource(() => { const id = ids && at < ids.length ? ids[at++] : fresh(); used.push(id); return id; });
  try { return { result: run(), ids: used }; } finally { setIdSource(fresh); }
}

export const newConversationTitle = 'New conversation';
export const newTabTitle = 'New tab';

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

/**
 * THE STRIP'S ORDER IS `state.tabs`, AND TWO LAWS HOLD AFTER EVERY ACTION: pinned tabs stand first and belong to no
 * group, and each group's members stand together as one run (design 2b draws loose tabs on both sides of a group).
 * workspaceReducer applies this after every slice, so the strip, the keys, ⌘1–9 and the overview read one order and no
 * renderer ever re-sorts. A member found away from its run joins the run where the group's first member stands.
 * Groups are kept in the order their runs appear, and a group with no members is gone.
 */
export function arrange(state: WorkspaceState): WorkspaceState {
  const known = new Set(state.groups.map(group => group.id));
  const own = (tab: Tab): Tab => (tab.groupId && (tab.pinned || !known.has(tab.groupId)) ? { ...tab, groupId: undefined } : tab);
  const all = state.tabs.map(own);
  const loose = all.filter(tab => !tab.pinned);
  const order = all.filter(tab => tab.pinned);
  const runs: string[] = [];
  for (const tab of loose) {
    if (!tab.groupId) order.push(tab);
    else if (!runs.includes(tab.groupId)) { runs.push(tab.groupId); order.push(...loose.filter(member => member.groupId === tab.groupId)); }
  }
  const tabs = order.length === state.tabs.length && order.every((tab, index) => tab === state.tabs[index]) ? state.tabs : order;
  const sorted = runs.map(id => state.groups.find(group => group.id === id)!);
  const groups = sorted.length === state.groups.length && sorted.every((group, index) => group === state.groups[index]) ? state.groups : sorted;
  const picked = state.picked?.filter(id => tabs.some(tab => tab.id === id));
  const samePicks = picked === undefined || (picked.length === state.picked!.length);
  if (tabs === state.tabs && groups === state.groups && samePicks) return state;
  return { ...state, tabs, groups, ...(state.picked ? { picked: samePicks ? state.picked : picked } : {}) };
}

/** Whether a saved order already obeys the laws of `arrange` (a save from before the laws drew groups after loose tabs). */
export function isArranged(tabs: readonly Tab[]): boolean {
  const firstLoose = tabs.findIndex(tab => !tab.pinned);
  if (firstLoose >= 0 && tabs.slice(firstLoose).some(tab => tab.pinned)) return false;
  const closed = new Set<string>();
  return tabs.every((tab, index) => {
    const previous = tabs[index - 1]?.groupId;
    if (previous && previous !== tab.groupId) closed.add(previous);
    return !tab.groupId || !closed.has(tab.groupId);
  });
}

/** The run of a group's members in an arranged order: the first and last index, or undefined when it has none. */
export function runOf(tabs: readonly Tab[], groupId: string | undefined): { start: number; end: number } | undefined {
  if (!groupId) return undefined;
  const start = tabs.findIndex(tab => tab.groupId === groupId);
  if (start < 0) return undefined;
  let end = start;
  while (tabs[end + 1]?.groupId === groupId) end++;
  return { start, end };
}

/** How many pinned tabs lead an arranged order. */
export const pinnedCount = (tabs: readonly Tab[]) => tabs.filter(tab => tab.pinned).length;

/**
 * The nearest index to `index` where `tab` may stand in `tabs` (which does not hold it) without breaking the laws:
 * a pinned tab among the pinned, a member of a group that has members at or inside that run, any other tab never
 * between two members of one group.
 */
export function fitIndex(tabs: readonly Tab[], tab: Tab, index: number): number {
  const pinned = pinnedCount(tabs);
  if (tab.pinned) return Math.min(Math.max(index, 0), pinned);
  const at = Math.min(Math.max(index, pinned), tabs.length);
  const run = runOf(tabs, tab.groupId);
  if (run) return Math.min(Math.max(at, run.start), run.end + 1);
  const left = tabs[at - 1]?.groupId;
  return left && left === tabs[at]?.groupId ? runOf(tabs, left)!.end + 1 : at;
}

export const insertAt = (tabs: readonly Tab[], tab: Tab, index: number): Tab[] => [...tabs.slice(0, index), tab, ...tabs.slice(index)];

/** What the strip draws after its pinned tabs, in order: a loose tab, or a group with its members. */
export type StripItem = { kind: 'tab'; tab: Tab } | { kind: 'group'; group: TabGroup; members: Tab[] };

export function stripItems(state: Pick<WorkspaceState, 'tabs' | 'groups'>): StripItem[] {
  const items: StripItem[] = [];
  for (const tab of state.tabs) {
    if (tab.pinned) continue;
    const group = tab.groupId ? state.groups.find(g => g.id === tab.groupId) : undefined;
    if (!group) { items.push({ kind: 'tab', tab }); continue; }
    const last = items[items.length - 1];
    if (last?.kind === 'group' && last.group.id === group.id) last.members.push(tab);
    else items.push({ kind: 'group', group, members: [tab] });
  }
  return items;
}

/** The strip's reading order: `state.tabs` as it stands, less the hidden members of a collapsed group (its active member stays). */
export function visibleTabs(state: Pick<WorkspaceState, 'tabs' | 'groups' | 'activeId'>): Tab[] {
  const collapsed = new Set(state.groups.filter(group => group.collapsed).map(group => group.id));
  return state.tabs.filter(tab => !tab.groupId || !collapsed.has(tab.groupId) || tab.id === state.activeId);
}

export function nextGroupTitle(groups: readonly TabGroup[]): string {
  const names = new Set(groups.map(group => group.title.toLocaleLowerCase()));
  let number = 1;
  while (names.has(number === 1 ? 'new group' : `new group ${number}`)) number++;
  return number === 1 ? 'New group' : `New group ${number}`;
}
