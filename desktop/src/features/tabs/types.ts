import type { TabKind } from './kinds/types.ts';
import type { TabView } from './view-state.ts';

export type TitleSource = 'message' | 'engine' | 'manual';

/** What one pane shows: a kind plus the per-pane state (draft, session, route, folds). A plain tab is exactly one pane. */
export type Pane = { id: string; kind: TabKind; title: string; draft: string; titleSource?: TitleSource } & TabView;

/** 1x2 = one row, two columns. 2x1 = two rows, one column. 2x2 = a grid of three or four panes. */
export type SplitLayout = '1x2' | '2x1' | '2x2';
export type Split = { layout: SplitLayout; focus: number; panes: Pane[] };

/**
 * A tab in the strip. A plain tab carries its pane's fields itself. A split tab is ONE merged tab:
 * it keeps the strip position, group and pin, and holds up to four panes in `split`; its own
 * content fields are then unused (see panesOf).
 */
export type Tab = Pane & { pinned: boolean; groupId?: string; split?: Split };
export type TabGroup = { id: string; title: string; collapsed: boolean };

export type WorkspaceState = {
  tabs: Tab[];
  groups: TabGroup[];
  activeId: string;
  /** Closed tabs, newest last, for Reopen. A closed split tab keeps its panes. */
  closed: Tab[];
  nextNumber: number;
  recentIds: string[];
};
