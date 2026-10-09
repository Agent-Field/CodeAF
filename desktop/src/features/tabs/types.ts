import type { TabKind } from './kinds/types.ts';
import type { TabView } from './view-state.ts';

export type TitleSource = 'message' | 'engine' | 'manual';

/** What one pane shows: a kind plus the per-pane state (draft, session, route, folds). A plain tab is exactly one pane. */
export type Pane = { id: string; kind: TabKind; title: string; draft: string; titleSource?: TitleSource } & TabView;

/** 1x2 = one row, two columns. 2x1 = two rows, one column. 2x2 = a grid of three or four panes. */
export type SplitLayout = '1x2' | '2x1' | '2x2';
/** Where the dividers sit: `col` is the first column's share of the width, `row` the first row's share of the height (0..1, default one half). */
export type SplitRatios = { col: number; row: number };
export type Split = { layout: SplitLayout; focus: number; panes: Pane[]; ratios?: SplitRatios };

/**
 * A tab in the strip. A plain tab carries its pane's fields itself. A split tab is ONE merged tab:
 * it keeps the strip position, group and pin, and holds up to four panes in `split`; its own
 * content fields are then unused (see panesOf).
 */
export type Tab = Pane & { pinned: boolean; groupId?: string; split?: Split };
export type TabGroup = { id: string; title: string; collapsed: boolean };

/**
 * Where a closed tab stood, recorded by the reducer when it closed so Reopen puts it back there: before the tab that
 * followed it, else after the tab that preceded it. `group` is the group it was in, kept whole so a group that closing
 * emptied (and so removed) comes back with its name.
 */
export type ClosedPlace = { before?: string; after?: string; group?: TabGroup };
/** A closed tab and, when it was closed in this build, where it stood. */
/** `stood` is where the tab stood when it closed (not `place`, which a place's Home already uses for its place id). */
export type ClosedTab = Tab & { stood?: ClosedPlace };

export type WorkspaceState = {
  tabs: Tab[];
  groups: TabGroup[];
  activeId: string;
  /** Closed tabs, newest last, for Reopen. A closed split tab keeps its panes. */
  closed: ClosedTab[];
  nextNumber: number;
  recentIds: string[];
  /**
   * Tabs picked with ⌘-click (Ctrl-click on Linux) for ⌘G, besides the active tab, which is always part of the
   * selection. Window-local and never read back from a save: a reload starts with nothing picked.
   */
  picked?: string[];
};
