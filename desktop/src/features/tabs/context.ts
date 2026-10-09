import type { Dispatch } from 'react';
import type { TabSummary } from '../conversation/tabSummary';
import type { PreviewStore } from './preview/previewStore';
import type { TintName } from '../places/components/PlaceSwatch';
import type { Tab, WorkspaceAction, WorkspaceState } from './model';
import type { MenuEntry } from '../../components/ui';

/** What every host (menu, preview, drag, keys) may read and do. Built once by Workspace and passed down; hosts never reach for globals. */
export type TabsApi = {
  state: WorkspaceState;
  dispatch: Dispatch<WorkspaceAction>;
  /** Engine-derived summaries keyed by pane id (a plain tab's pane id is its tab id). */
  summaries: Readonly<Record<string, TabSummary>>;
  now: number;
  /** Closes with focus restoration. Closing detaches; it never stops engine work. */
  closeTab: (id: string) => void;
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
