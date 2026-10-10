// The workspace model: types, the persisted shape and the pure reducer.
// The reducer is a composition of slices (reducers/): add a lane's actions as a new slice file, add
// its action type to WorkspaceAction and its function to `slices` below. Nothing here touches React.
import { cleanView } from './view-state.ts';
import { kindOrDefault } from './kinds/types.ts';
import { arrange, mintWith, clampRatio, initialWorkspace, isArranged, layoutFits, makeSplit, titleRank, withSplitTitle, splitCapacity } from './helpers.ts';
import { reduceNewTab, type NewTabAction } from './reducers/newtab.ts';
import { reduceGroups, type GroupAction } from './reducers/groups.ts';
import { reduceHistory, type HistoryAction } from './reducers/history.ts';
import { reduceSplit, type SplitAction } from './reducers/split.ts';
import { closedAtOf, reduceClosing, type ClosingAction } from './reducers/closing.ts';
import { reduceHandoff, type HandoffAction } from './reducers/handoff.ts';
import { reduceTabs, type TabAction } from './reducers/tabs.ts';
import { reduceHome, type HomeAction } from './reducers/home.ts';
import { reduceUndo, type UndoAction } from './reducers/undo.ts';
import type { ClosedPlace, ClosedTab, Pane, SplitLayout, Tab, TabGroup, TitleSource, WorkspaceState } from './types.ts';

export type { ClosedPlace, ClosedTab, Pane, Split, SplitLayout, Tab, TabGroup, TitleSource, WorkspaceState } from './types.ts';
export { initialWorkspace, panesOf, focusedPane, isSplit, visibleTabs, stripItems, tabHolding, splitCapacity, type StripItem } from './helpers.ts';
export const storageKey = 'codeaf.desktop.workspace.v1';

/**
 * Where a window place's tab set is saved. Now keeps the v1 key, so the tabs a person had before Places existed are
 * Now's tabs; each design-graph place has its own key. Two windows on one place share the key, and so the tabs.
 */
export const workspaceKey = (place: string) => (place === 'now' ? storageKey : `${storageKey}:${place}`);

/** A strip nobody has opened yet: Now starts with one quiet new conversation, a place with only its Home. */
export function freshWorkspace(place?: { id: string; title: string }): WorkspaceState {
  if (!place) return initialWorkspace();
  const home = { ...initialWorkspace().tabs[0], kind: 'home' as const, place: place.id, title: place.title, titleSource: 'manual' as const, pinned: true };
  return { tabs: [home], groups: [], activeId: home.id, closed: [], nextNumber: 1, recentIds: [home.id] };
}

const isString = (value: unknown): value is string => typeof value === 'string';

/** The fields a pane and a tab share, validated. An unknown kind becomes a conversation (every v1 tab was one). */
function readPane(value: unknown): Pane | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const p = value as Partial<Pane>;
  if (!isString(p.id) || !p.id || !isString(p.title) || !isString(p.draft)) return undefined;
  if (p.titleSource !== undefined && titleRank[p.titleSource as TitleSource] === undefined) return undefined;
  const pane: Pane = { id: p.id, kind: kindOrDefault(p.kind), title: p.title, draft: p.draft, titleSource: p.titleSource, ...cleanView(p as unknown as Record<string, unknown>) };
  return pane;
}

const layouts: readonly SplitLayout[] = ['1x2', '2x1', '2x2'];

/** A split that does not validate is dropped and the tab stays a plain tab: its panes are not worth discarding the workspace for. */
function readSplit(value: unknown): Tab['split'] {
  if (!value || typeof value !== 'object') return undefined;
  const s = value as { layout?: unknown; focus?: unknown; panes?: unknown };
  if (!Array.isArray(s.panes) || s.panes.length < 2 || s.panes.length > splitCapacity) return undefined;
  const panes = s.panes.map(readPane);
  if (panes.some(p => !p) || new Set(panes.map(p => p!.id)).size !== panes.length) return undefined;
  const layout = layouts.find(l => l === s.layout);
  const focus = Number.isInteger(s.focus) ? (s.focus as number) : 0;
  const r = s as { ratios?: { col?: unknown; row?: unknown } };
  const share = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? clampRatio(v) : 0.5);
  const ratios = r.ratios && typeof r.ratios === 'object' ? { col: share(r.ratios.col), row: share(r.ratios.row) } : undefined;
  return makeSplit(panes as Pane[], focus, layout && layoutFits(layout, panes.length) ? layout : undefined, ratios);
}

function readTab(value: unknown): Tab | undefined {
  const pane = readPane(value);
  const t = value as Partial<Tab> | null;
  if (!pane || !t || typeof t.pinned !== 'boolean' || (t.groupId !== undefined && !isString(t.groupId))) return undefined;
  const split = readSplit(t.split);
  const tab: Tab = { ...pane, pinned: t.pinned, groupId: t.groupId, split };
  return split ? withSplitTitle(tab) : { ...tab, split: undefined };
}

const isGroup = (value: unknown): value is TabGroup => {
  if (!value || typeof value !== 'object') return false;
  const group = value as Partial<TabGroup>;
  return isString(group.id) && !!group.id && isString(group.title) && typeof group.collapsed === 'boolean';
};

/** A closed tab's recorded place; a place that does not validate is dropped and the tab reopens at the end. */
function readPlace(value: unknown): ClosedPlace | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const p = value as Partial<Record<keyof ClosedPlace, unknown>>;
  const id = (v: unknown) => (isString(v) && v ? v : undefined);
  return { before: id(p.before), after: id(p.after), group: isGroup(p.group) ? p.group : undefined };
}

function readClosed(value: unknown): ClosedTab | undefined {
  const tab = readTab(value);
  if (!tab) return undefined;
  const stood = readPlace((value as { stood?: unknown }).stood);
  const closedAt = closedAtOf((value as { closedAt?: unknown }).closedAt);
  const closed: ClosedTab = stood ? { ...tab, stood } : tab;
  return closedAt === undefined ? closed : { ...closed, closedAt };
}

/**
 * A save from before the strip's laws (helpers `arrange`) drew pinned tabs, then loose tabs, then each group in
 * `groups` order, whatever order `tabs` held. Such a save is put into exactly that order once, so a person sees the
 * strip they left.
 */
function drawnOrder(tabs: Tab[], groups: readonly TabGroup[]): Tab[] {
  if (isArranged(tabs)) return tabs;
  return [...tabs.filter(t => t.pinned), ...tabs.filter(t => !t.pinned && !t.groupId), ...groups.flatMap(g => tabs.filter(t => !t.pinned && t.groupId === g.id))];
}

const unique = (ids: string[]) => new Set(ids).size === ids.length;

/** Reads the saved workspace; anything that does not validate yields a fresh one. v1 saves (no kind, no split) load as conversations. */
export function readWorkspace(key: string = storageKey, fresh: () => WorkspaceState = initialWorkspace): WorkspaceState {
  try {
    return parseWorkspace(localStorage.getItem(key)) ?? fresh();
  } catch { return fresh(); }
}

/** Validates a saved tab set; undefined when it does not (the caller then starts fresh). Also reads another window's write. */
export function parseWorkspace(text: string | null): WorkspaceState | undefined {
  try {
    const saved = JSON.parse(text ?? 'null') as Partial<WorkspaceState> | null;
    if (!saved || !Array.isArray(saved.tabs) || !saved.tabs.length || !Array.isArray(saved.groups) || !Array.isArray(saved.closed)) return undefined;
    const read = saved.tabs.map(readTab);
    const closed = saved.closed.map(readClosed);
    if (read.some(t => !t) || closed.some(t => !t) || !saved.groups.every(isGroup)) return undefined;
    const tabsIn = read as Tab[];
    const closedIn = closed as ClosedTab[];
    if (!unique(tabsIn.flatMap(t => [t.id, ...(t.split?.panes.map(p => p.id) ?? [])])) || !unique(tabsIn.map(t => t.id)) || !unique(saved.groups.map(g => g.id)) || !unique(closedIn.map(t => t.id)) || closedIn.some(t => tabsIn.some(open => open.id === t.id))) return undefined;
    const groupIds = new Set(saved.groups.map(g => g.id));
    const tabs = drawnOrder(tabsIn.map(tab => ({ ...tab, groupId: !tab.pinned && tab.groupId && groupIds.has(tab.groupId) ? tab.groupId : undefined })), saved.groups);
    const activeId = tabs.some(tab => tab.id === saved.activeId) ? saved.activeId! : tabs[0].id;
    return arrange({
      tabs, groups: saved.groups.filter(g => tabs.some(t => t.groupId === g.id)), closed: closedIn.slice(-20),
      recentIds: [...new Set([activeId, ...(Array.isArray(saved.recentIds) ? saved.recentIds.filter(id => isString(id) && tabs.some(t => t.id === id)) : []), ...tabs.map(t => t.id)])],
      activeId,
      nextNumber: Number.isSafeInteger(saved.nextNumber) && saved.nextNumber! > 0 && saved.nextNumber! < 1000000 ? saved.nextNumber! : tabs.length + 1,
    });
  } catch { return undefined; }
}

export type WorkspaceAction = (TabAction | GroupAction | SplitAction | NewTabAction | HistoryAction | ClosingAction | HandoffAction | HomeAction | UndoAction) & { mint?: readonly string[] };
type Slice = (state: WorkspaceState, action: { type: string }) => WorkspaceState | undefined;
// The Home slice goes first: it refuses the moves a place's Home may not make before the other slices see them.
const slices: readonly Slice[] = [reduceHome, reduceTabs, reduceGroups, reduceSplit, reduceNewTab, reduceHistory, reduceClosing, reduceHandoff, reduceUndo];

/** Runs the slice that owns the action, then holds the strip's laws (helpers `arrange`) whatever that slice did. */
export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  if (action.mint) return mintWith(action.mint, () => applySlices(state, action)).result;
  return applySlices(state, action);
}

function applySlices(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  for (const slice of slices) {
    const next = slice(state, action);
    if (next) return next === state ? state : arrange(next);
  }
  return state;
}

