import { createContext, useContext, type Dispatch } from 'react';
import type { FailureId } from '../chat/world-client';
import type { TabSummary } from '../conversation/tabSummary';
import type { TabActions } from './actions';
import type { BackgroundWork } from './closing/background';
import type { PreviewStore } from './preview/previewStore';
import type { TintName } from '../places/components/PlaceSwatch';
import type { Tab, WorkspaceAction, WorkspaceState } from './model';
import type { MenuEntry } from '../../components/ui';

/** What every host (menu, preview, drag, keys) may read and do. Built once by Workspace and passed down; hosts never reach for globals. */
export type TabsApi = {
  /** Canonical window place used for read-only pane resources. */
  workspaceKey?: string;
  state: WorkspaceState;
  dispatch: Dispatch<WorkspaceAction>;
  /** Engine-derived summaries keyed by pane id (a plain tab's pane id is its tab id). */
  summaries: Readonly<Record<string, TabSummary>>;
  now: number;
  /** Closes with focus restoration. Closing detaches; it never stops engine work. A running tab offers Stop it and Undo in a toast. */
  closeTab: (id: string) => void;
  /** Closes the tab and asks the engine to stop the running turn and tasks of each of its panes. */
  closeAndStop: (id: string) => void;
  /** Closes a group, the other tabs, or the tabs to the right behind one Undo that restores every pane, group and place. */
  closeMany: (action: Extract<WorkspaceAction, { type: 'close-others' | 'close-right' | 'close-group' }>) => void;
  /** True while any pane of the tab is working or waiting on the person, so closing it leaves work running. */
  isRunning: (tab: Tab) => boolean;
  /** Work that outlived its tab, and open tabs that need the person: what the Inbox lists. */
  background: BackgroundWork;
  /**
   * Records that a conversation's failure was looked at, so the Inbox stops listing it. With `failure` (the identity the world feed
   * sent) the engine keeps the mark for every window and device and a refusal is shown; without it, only this window's own
   * record is possible (an engine that predates the mark). See closing/failedSeen.ts and closing/seenMarks.ts.
   */
  markFailedSeen: (chatId: string, count: number, failure?: FailureId) => void;
  /** Opens a conversation that has no tab here, by chat id. Absent until the shell supplies it; the Inbox then shows such rows without a click. */
  openChat?: (chatId: string) => void;
  /** Whether an external Inbox row has a durable journal the opener can attach. */
  canOpenChat?: (chatId: string) => boolean;
  /** What a tab can do beyond the workspace: copy a link (when one exists) and move to another window. */
  actions: TabActions;
  /** Reopens a closed tab where it was (its place in the strip and its group). */
  reopenClosed: (id: string) => void;
  /** Opens the rename dialog for a tab or, with `group`, a group. */
  startRename: (id: string, group?: boolean) => void;
  /** Records a fresh summary for a pane, the way a background read does (the hover preview uses it after an answer). */
  receiveSummary: (paneId: string, summary: TabSummary) => void;
  /** True while the switcher, overview or rename dialog is open, so hover cards stay shut. */
  overlayOpen: boolean;
  /** The hover-preview state of this workspace: which card is open and whether the next one swaps in. */
  previews: PreviewStore;
  /** The tint of the place this strip belongs to: its Home tab carries it as a swatch (Shell 3j "Pinned"). */
  placeTint?: TintName;
  /** The place menu, for this strip's Home tab (Interactions "Home tab (pinned): right-click · Place menu"). */
  placeMenu?: MenuEntry[];
  /** With the rail put away, the Home tab opens this place switcher (Places 9c); `alert` names another place that needs you. */
  placeSwitcher?: { items: MenuEntry[]; alert?: string };
  /** Moves a tab to a new window on this place; absent where there are no windows (a browser). */
  moveToNewWindow?: (tab: Tab) => void;
};

export const TabsApiContext = createContext<TabsApi | null>(null);

/** The workspace's api for a pane body (the Inbox needs it; a conversation does not). */
export function useTabsApi(): TabsApi {
  const api = useContext(TabsApiContext);
  if (!api) throw new Error('useTabsApi needs a TabsApiContext provider');
  return api;
}
