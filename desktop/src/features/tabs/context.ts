import { createContext, useContext, type Dispatch } from 'react';
import type { TabSummary } from '../conversation/tabSummary';
import type { BackgroundWork } from './closing/background';
import type { Tab, WorkspaceAction, WorkspaceState } from './model';

/** What every host (menu, preview, drag, keys) may read and do. Built once by Workspace and passed down; hosts never reach for globals. */
export type TabsApi = {
  state: WorkspaceState;
  dispatch: Dispatch<WorkspaceAction>;
  /** Engine-derived summaries keyed by pane id (a plain tab's pane id is its tab id). */
  summaries: Readonly<Record<string, TabSummary>>;
  now: number;
  /** Closes with focus restoration. Closing detaches; it never stops engine work. A running tab offers Stop it and Undo in a toast. */
  closeTab: (id: string) => void;
  /** Closes the tab and asks the engine to stop the running turn and tasks of each of its panes. */
  closeAndStop: (id: string) => void;
  /** True while any pane of the tab is working or waiting on the person, so closing it leaves work running. */
  isRunning: (tab: Tab) => boolean;
  /** Work that outlived its tab, and open tabs that need the person: what the Inbox lists. */
  background: BackgroundWork;
  /** Reopens a closed tab where it was (its place in the strip and its group). */
  reopenClosed: (id: string) => void;
  /** Opens the rename dialog for a tab or, with `group`, a group. */
  startRename: (id: string, group?: boolean) => void;
  /** True while the switcher, overview or rename dialog is open, so hover cards stay shut. */
  overlayOpen: boolean;
};

export const TabsApiContext = createContext<TabsApi | null>(null);

/** The workspace's api for a pane body (the Inbox needs it; a conversation does not). */
export function useTabsApi(): TabsApi {
  const api = useContext(TabsApiContext);
  if (!api) throw new Error('useTabsApi needs a TabsApiContext provider');
  return api;
}
