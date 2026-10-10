// What two windows on one place share, and what each window keeps for itself (Places 6d "Same place, two
// windows: both windows show the same tab set, live"; PLACES-ARCHITECTURE §3.4).
//
// SHARED: the tabs (with their panes' content — drafts, routes, folds, what a pane points at), the groups, pins,
// splits and their layout and dividers, the closed list for Reopen, and the tab counter.
// WINDOW-LOCAL: which tab this window shows, its recency order (Ctrl+Tab), which pane of each split it focuses,
// and where each pane is scrolled. The engine REFUSES a shared document that carries any of these
// (internal/workspacestore/validate.go), so a window that forgot to strip one learns it at once instead of making
// every other window jump.
//
// Everything here is pure: no storage, no network, no React.
import { retainSelection } from '../tabs/selection.ts';
import { cleanView } from '../tabs/view-state.ts';
import { kindOrDefault } from '../tabs/kinds/types.ts';
import { clampRatio, layoutFits, makeSplit, titleRank, visibleTabs } from '../tabs/helpers.ts';
import { closedAtOf } from '../tabs/reducers/closing.ts';
import type { ClosedTab, Pane, SplitLayout, Tab, TabGroup, TitleSource, WorkspaceState } from '../tabs/model.ts';
import { limits } from './limits.ts';

/** A split as it is shared: no focused pane. */
export type SharedSplit = Omit<NonNullable<Tab['split']>, 'focus'>;
export type SharedTab = Omit<ClosedTab, 'split'> & { split?: SharedSplit };

/** The document the engine stores for one place (internal/workspacestore). */
export type SharedWorkspace = {
  schema: 1;
  tabs: SharedTab[];
  groups: TabGroup[];
  closed: SharedTab[];
  nextNumber: number;
};

/** What one window keeps for itself. Scroll is in pixels from the top, per pane id. */
export type WindowLocal = {
  activeId?: string;
  recentIds: string[];
  /** The focused pane index of each split tab this window has focused, by split tab id. */
  focus: Record<string, number>;
  scroll: Record<string, number>;
};

export const emptyLocal = (): WindowLocal => ({ recentIds: [], focus: {}, scroll: {} });

/** Legacy offsets belong to this window even when they were saved on a pane. */
const stripScroll = <P extends Pane>(pane: P): P => {
  const { scrollOffset: _scroll, ...rest } = pane;
  return rest as P;
};

const stripFocus = (source: Tab): SharedTab => {
  const tab = stripScroll(source);
  if (!tab.split) { const { split: _drop, ...rest } = tab; return rest; }
  const { focus: _focus, ...split } = tab.split;
  return { ...tab, split: { ...split, panes: split.panes.map(stripScroll) } };
};

/** The shared half of a window's state. Closed split tabs lose their focus too: Reopen lands on the first pane. */
export function sharedOf(state: WorkspaceState): SharedWorkspace {
  return { schema: 1, tabs: state.tabs.map(stripFocus), groups: state.groups, closed: state.closed.map(stripFocus), nextNumber: state.nextNumber };
}

/** A stable text of the shared half, to tell a shared change from a window-local one. */
export const sharedText = (state: WorkspaceState) => JSON.stringify(sharedOf(state));

/** The window-local half, merged over what the window already kept (scroll is never in WorkspaceState). */
export function localOf(state: WorkspaceState, previous: WindowLocal): WindowLocal {
  const focus: Record<string, number> = {};
  for (const tab of [...state.tabs, ...state.closed]) if (tab.split && tab.split.focus > 0) focus[tab.id] = tab.split.focus;
  const scroll = { ...previous.scroll };
  for (const tab of [...state.tabs, ...state.closed]) for (const pane of tab.split?.panes ?? [tab]) {
    if (pane.scrollOffset !== undefined && scroll[pane.id] === undefined) scroll[pane.id] = pane.scrollOffset;
  }
  return { activeId: state.activeId, recentIds: state.recentIds, focus, scroll };
}

/**
 * The window's state: the shared tab set with this window's own focus laid over it. A local active tab that no
 * longer exists (another window closed it) falls to its nearest surviving neighbour in `before`'s reading order,
 * never to an arbitrary first tab.
 */
export function compose(shared: SharedWorkspace, local: WindowLocal, before?: WorkspaceState): WorkspaceState {
  const tabs: Tab[] = shared.tabs.map(tab => (tab.split ? { ...tab, split: makeSplit(tab.split.panes, local.focus[tab.id] ?? 0, tab.split.layout, tab.split.ratios) } : (tab as Tab)));
  const closed: Tab[] = shared.closed.map(tab => (tab.split ? { ...tab, split: makeSplit(tab.split.panes, local.focus[tab.id] ?? 0, tab.split.layout, tab.split.ratios) } : (tab as Tab)));
  const holds = (id: string | undefined) => !!id && tabs.some(tab => tab.id === id);
  let activeId = local.activeId;
  if (!holds(activeId)) {
    // A pane id (the split tab was merged elsewhere) resolves to its holder.
    const holder = activeId ? tabs.find(tab => tab.split?.panes.some(pane => pane.id === activeId)) : undefined;
    activeId = holder?.id ?? neighbour(before, tabs, activeId) ?? tabs[0].id;
  }
  const recentIds = [...new Set([activeId!, ...local.recentIds.filter(holds), ...tabs.map(tab => tab.id)])];
  return { tabs, groups: shared.groups, closed, nextNumber: shared.nextNumber, activeId: activeId!, recentIds, ...(before?.picked ? { picked: retainSelection(before.picked, tabs) } : {}) };
}

function neighbour(before: WorkspaceState | undefined, tabs: Tab[], gone: string | undefined): string | undefined {
  if (!before || !gone) return undefined;
  const order = visibleTabs(before).map(tab => tab.id);
  const at = order.indexOf(gone);
  if (at < 0) return undefined;
  const alive = new Set(tabs.map(tab => tab.id));
  // The tab after it first (where a closed tab's neighbour slides in), then the one before.
  for (let step = 1; step < order.length; step++) {
    if (alive.has(order[at + step])) return order[at + step];
    if (alive.has(order[at - step])) return order[at - step];
  }
  return undefined;
}

// ---- reading what the engine sent --------------------------------------------------------------

const isString = (value: unknown): value is string => typeof value === 'string';
const isObject = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const layouts: readonly SplitLayout[] = ['1x2', '2x1', '2x2'];

/** A pane's fields, validated as tabs/model.ts readWorkspace validates them. */
function readPane(value: unknown): Pane | undefined {
  if (!isObject(value)) return undefined;
  if (!isString(value.id) || !value.id || !isString(value.title) || !isString(value.draft)) return undefined;
  if (value.titleSource !== undefined && titleRank[value.titleSource as TitleSource] === undefined) return undefined;
  return { id: value.id, kind: kindOrDefault(value.kind), title: value.title, draft: value.draft, titleSource: value.titleSource as TitleSource | undefined, ...cleanView(value) };
}

function readTab(value: unknown): SharedTab | undefined {
  const pane = readPane(value);
  if (!pane || !isObject(value) || typeof value.pinned !== 'boolean' || (value.groupId !== undefined && !isString(value.groupId))) return undefined;
  const stood = isObject(value.stood) ? value.stood : undefined;
  const position = stood && (stood.before === undefined || isString(stood.before)) && (stood.after === undefined || isString(stood.after)) && (stood.group === undefined || isGroup(stood.group)) ? { before: stood.before as string | undefined, after: stood.after as string | undefined, group: stood.group as TabGroup | undefined } : undefined;
  const closedAt = closedAtOf(value.closedAt);
  const tab: SharedTab = { ...pane, ...(position ? { stood: position } : {}), ...(closedAt === undefined ? {} : { closedAt }), pinned: value.pinned, ...(isString(value.groupId) ? { groupId: value.groupId } : {}) };
  const s = value.split;
  if (!isObject(s) || !Array.isArray(s.panes)) return tab;
  const panes = s.panes.map(readPane);
  if (panes.length < 2 || panes.length > limits.panes || panes.some(p => !p)) return tab;
  const layout = layouts.find(l => l === s.layout);
  const share = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? clampRatio(v) : 0.5);
  const ratios = isObject(s.ratios) ? { col: share(s.ratios.col), row: share(s.ratios.row) } : undefined;
  const { focus: _focus, ...split } = makeSplit(panes as Pane[], 0, layout && layoutFits(layout, panes.length) ? layout : undefined, ratios);
  return { ...tab, split };
}

const isGroup = (value: unknown): value is TabGroup => isObject(value) && isString(value.id) && !!value.id && isString(value.title) && typeof value.collapsed === 'boolean';

/** Ids that must be unique across one document: open tabs and their panes, closed tabs, groups. */
export function duplicateIds(state: Pick<WorkspaceState, 'tabs' | 'closed' | 'groups'>): boolean {
  const open = state.tabs.flatMap(tab => [tab.id, ...(tab.split?.panes.map(pane => pane.id) ?? [])]);
  const closed = state.closed.map(tab => tab.id);
  const groups = state.groups.map(group => group.id);
  return new Set(open).size !== open.length || new Set(closed).size !== closed.length || new Set(groups).size !== groups.length || closed.some(id => open.includes(id));
}

/** Validates a shared document from the engine; undefined when it does not hold a usable tab set. */
export function parseShared(value: unknown): SharedWorkspace | undefined {
  if (!isObject(value) || value.schema !== 1 || !Array.isArray(value.tabs) || !Array.isArray(value.groups) || !Array.isArray(value.closed)) return undefined;
  const tabs = value.tabs.map(readTab);
  const closed = value.closed.map(readTab);
  if (!tabs.length || tabs.some(t => !t) || closed.some(t => !t) || !value.groups.every(isGroup)) return undefined;
  const doc: SharedWorkspace = {
    schema: 1, tabs: tabs as SharedTab[], groups: value.groups as TabGroup[], closed: closed as SharedTab[],
    nextNumber: Number.isSafeInteger(value.nextNumber) && (value.nextNumber as number) > 0 ? value.nextNumber as number : tabs.length + 1,
  };
  if (duplicateIds(doc as unknown as WorkspaceState)) return undefined;
  const groupIds = new Set(doc.groups.map(group => group.id));
  doc.tabs = doc.tabs.map(tab => (tab.groupId && (tab.pinned || !groupIds.has(tab.groupId)) ? { ...tab, groupId: undefined } : tab));
  return doc;
}
