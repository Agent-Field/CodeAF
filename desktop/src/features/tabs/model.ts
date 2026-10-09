// The workspace model: types, the persisted shape and the pure reducer.
// The reducer is a composition of slices (reducers/): add a lane's actions as a new slice file, add
// its action type to WorkspaceAction and its function to `slices` below. Nothing here touches React.
import { cleanView } from './view-state.ts';
import { kindOrDefault } from './kinds/types.ts';
import { initialWorkspace, layoutFits, makeSplit, titleRank, withSplitTitle, splitCapacity } from './helpers.ts';
import { reduceGroups, type GroupAction } from './reducers/groups.ts';
import { reduceSplit, type SplitAction } from './reducers/split.ts';
import { reduceTabs, type TabAction } from './reducers/tabs.ts';
import type { Pane, SplitLayout, Tab, TabGroup, TitleSource, WorkspaceState } from './types.ts';

export type { Pane, Split, SplitLayout, Tab, TabGroup, TitleSource, WorkspaceState } from './types.ts';
export { initialWorkspace, panesOf, focusedPane, isSplit, visibleTabs, tabHolding, splitCapacity } from './helpers.ts';
export const storageKey = 'codeaf.desktop.workspace.v1';

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
  return makeSplit(panes as Pane[], focus, layout && layoutFits(layout, panes.length) ? layout : undefined);
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

const unique = (ids: string[]) => new Set(ids).size === ids.length;

/** Reads the saved workspace; anything that does not validate yields a fresh one. v1 saves (no kind, no split) load as conversations. */
export function readWorkspace(): WorkspaceState {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null') as Partial<WorkspaceState> | null;
    if (!saved || !Array.isArray(saved.tabs) || !saved.tabs.length || !Array.isArray(saved.groups) || !Array.isArray(saved.closed)) return initialWorkspace();
    const read = saved.tabs.map(readTab);
    const closed = saved.closed.map(readTab);
    if (read.some(t => !t) || closed.some(t => !t) || !saved.groups.every(isGroup)) return initialWorkspace();
    const tabsIn = read as Tab[];
    const closedIn = closed as Tab[];
    if (!unique(tabsIn.flatMap(t => [t.id, ...(t.split?.panes.map(p => p.id) ?? [])])) || !unique(tabsIn.map(t => t.id)) || !unique(saved.groups.map(g => g.id)) || !unique(closedIn.map(t => t.id)) || closedIn.some(t => tabsIn.some(open => open.id === t.id))) return initialWorkspace();
    const groupIds = new Set(saved.groups.map(g => g.id));
    const tabs = tabsIn.map(tab => ({ ...tab, groupId: !tab.pinned && tab.groupId && groupIds.has(tab.groupId) ? tab.groupId : undefined }));
    const activeId = tabs.some(tab => tab.id === saved.activeId) ? saved.activeId! : tabs[0].id;
    return {
      tabs, groups: saved.groups.filter(g => tabs.some(t => t.groupId === g.id)), closed: closedIn.slice(-20),
      recentIds: [...new Set([activeId, ...(Array.isArray(saved.recentIds) ? saved.recentIds.filter(id => isString(id) && tabs.some(t => t.id === id)) : []), ...tabs.map(t => t.id)])],
      activeId,
      nextNumber: Number.isSafeInteger(saved.nextNumber) && saved.nextNumber! > 0 && saved.nextNumber! < 1000000 ? saved.nextNumber! : tabs.length + 1,
    };
  } catch { return initialWorkspace(); }
}

export type WorkspaceAction = TabAction | GroupAction | SplitAction;
type Slice = (state: WorkspaceState, action: { type: string }) => WorkspaceState | undefined;
const slices: readonly Slice[] = [reduceTabs, reduceGroups, reduceSplit];

export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  for (const slice of slices) {
    const next = slice(state, action);
    if (next) return next;
  }
  return state;
}

