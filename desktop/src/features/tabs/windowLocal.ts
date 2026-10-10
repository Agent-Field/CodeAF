// The split of a workspace between what every window on a place shares and what one window keeps to itself
// (Interactions, Flows "Multiple windows"; decision row 11). Shared fields travel through workspaceSync; the
// window-local ones live in sessionStorage under the window's label, so a second window opens on the same tabs
// without moving the first window's active tab, selection or rail. Nothing here touches React or storage.
import type { WorkspaceState } from './types.ts';

/** What the engine stores and every window on the place sees: the tab set itself. */
export type SharedWorkspace = Pick<WorkspaceState, 'tabs' | 'groups' | 'closed' | 'nextNumber'>;

/** What one window keeps. `undo` is opaque here because the stack's entries belong to the undo slice. */
export type WindowLocal<U = unknown> = {
  activeId: string;
  recentIds: string[];
  selection: string[];
  overviewOpen: boolean;
  undo: U[];
  railCollapsed: boolean;
  focusMode: boolean;
};

/** Every id a window can point at: tabs, and the panes inside split tabs (a selection may name either). */
function liveIds(shared: SharedWorkspace): Set<string> {
  const ids = new Set<string>();
  for (const tab of shared.tabs) {
    ids.add(tab.id);
    for (const pane of tab.split?.panes ?? []) ids.add(pane.id);
  }
  return ids;
}

/**
 * The tab to show when the active one vanished. `was` is the strip order this window last saw: the nearest
 * surviving neighbour wins, the right one before the left one (as closing a tab in a browser does), and the first
 * tab is the answer when the window never saw an order or nothing it saw survives.
 */
export function nearestSurvivor(was: readonly string[], gone: string, alive: ReadonlySet<string>): string | undefined {
  const at = was.indexOf(gone);
  if (at >= 0) {
    for (let step = 1; step < was.length; step++) {
      const right = was[at + step];
      if (right !== undefined && alive.has(right)) return right;
      const left = was[at - step];
      if (left !== undefined && alive.has(left)) return left;
    }
  }
  return undefined;
}

/**
 * Lays one window's local state over the shared tab set, repairing whatever another window's edit invalidated:
 * an active tab that was closed elsewhere moves to its nearest neighbour, and selection and recents drop ids that
 * no longer exist. `was` is the strip order before the shared change arrived. Pure: neither input is mutated, so
 * two windows merging against one shared state stay independent.
 */
export function mergeWindowLocal<U>(shared: SharedWorkspace, local: WindowLocal<U>, was: readonly string[] = []): WorkspaceState & WindowLocal<U> {
  const alive = liveIds(shared);
  const tabIds = new Set(shared.tabs.map(tab => tab.id));
  const activeId = tabIds.has(local.activeId) ? local.activeId : nearestSurvivor(was, local.activeId, tabIds) ?? shared.tabs[0]?.id ?? local.activeId;
  const recentIds = [...new Set([activeId, ...local.recentIds.filter(id => tabIds.has(id))])];
  return {
    ...shared, ...local, activeId, recentIds,
    selection: local.selection.filter(id => alive.has(id)),
    picked: local.selection.filter(id => alive.has(id) && id !== activeId),
  };
}
