// Reducer slice: closing many tabs, reopening a chosen one where it was, duplicating, and the pinned Inbox.
// Closing only detaches a view (the engine keeps the work), so every closed tab lands in `closed` and can be reopened.
import { createId, normalize, visibleTabs } from '../helpers.ts';
import { reduceTabs, type TabAction } from './tabs.ts';
import type { Pane, Tab, TabGroup, WorkspaceState } from '../types.ts';

/**
 * A close time worth showing. `closedRecord` stamps milliseconds when a tab enters the closed list; a save from
 * before that, or a value that is not a time, is dropped so the new-tab row says "closed" rather than a nonsense age.
 */
export function closedAtOf(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined;
}

export type ClosingAction =
  /** Closes every tab but this one. Pinned tabs stay, like a browser's. */
  | { type: 'close-others'; id: string }
  /** Closes the unpinned tabs after this one in the strip's reading order. */
  | { type: 'close-right'; id: string }
  /** Closes every member of a group. */
  | { type: 'close-group'; id: string }
  /**
   * Reopens one closed tab where it was: before the tab that followed it, else after the tab that preceded it, else last.
   * `group` re-creates its group if closing the tab removed it.
   */
  | { type: 'reopen-id'; id: string; before?: string; after?: string; group?: TabGroup }
  /**
   * Takes a bulk close back. The payload is the whole restore point, taken before the close, so nothing depends on the
   * bounded `closed` list: `tabs` are the closed tabs themselves (every pane of a split rides inside its tab), `order` is
   * the strip's tab order before the close, and `groups` is every group as it was (title, collapsed). A group the close
   * removed comes back with its own metadata; a tab or group that exists again is left alone.
   */
  | { type: 'restore-closed'; tabs: readonly Tab[]; order: readonly string[]; groups: readonly TabGroup[]; activeId: string }
  /** A copy of the tab right after it: the same session, draft and view, new ids. */
  | { type: 'duplicate'; id: string }
  /** Makes sure the one pinned Inbox tab exists. It is first in the strip and never takes focus. */
  | { type: 'ensure-inbox' }
  /** The rail's Inbox row (Interactions, "Rail · Inbox"): makes sure the Inbox tab exists and focuses it. */
  | { type: 'open-inbox' };

export const inboxId = 'inbox';

/** Closes each id in turn through the ordinary close, so the active tab moves exactly as it does for a single close. */
function closeAll(state: WorkspaceState, ids: readonly string[]): WorkspaceState {
  return ids.reduce((current, id) => {
    const close: TabAction = { type: 'close', id };
    return reduceTabs(current, close) ?? current;
  }, state);
}

/** The tab the person closed others around becomes active when the active tab was one of the closed. */
const keep = (next: WorkspaceState, before: WorkspaceState, id: string): WorkspaceState => (next.tabs.some(t => t.id === before.activeId) ? next : { ...next, activeId: id, recentIds: [id, ...next.recentIds.filter(r => r !== id)] });

/** Closable means a tab the person put there: never the Inbox. */
const closable = (tab: Tab) => tab.kind !== 'inbox';

const copyPane = (pane: Pane): Pane => ({ ...pane, id: createId() });

export function reduceClosing(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as ClosingAction;
  switch (a.type) {
    case 'close-others': return keep(closeAll(state, state.tabs.filter(t => t.id !== a.id && !t.pinned && closable(t)).map(t => t.id)), state, a.id);
    case 'close-right': {
      const order = visibleTabs(state);
      const index = order.findIndex(t => t.id === a.id);
      return index < 0 ? state : keep(closeAll(state, order.slice(index + 1).filter(t => !t.pinned && closable(t)).map(t => t.id)), state, a.id);
    }
    case 'close-group': return closeAll(state, state.tabs.filter(t => t.groupId === a.id).map(t => t.id));
    case 'reopen-id': {
      const tab = state.closed.find(t => t.id === a.id);
      if (!tab) return state;
      const groups = a.group && a.group.id === tab.groupId && !state.groups.some(g => g.id === a.group!.id) ? [...state.groups, { ...a.group, collapsed: false }] : state.groups;
      const groupId = groups.some(g => g.id === tab.groupId) ? tab.groupId : undefined;
      const tabs = [...state.tabs];
      const before = tabs.findIndex(t => t.id === a.before);
      const after = tabs.findIndex(t => t.id === a.after);
      // The close time belongs to the closed list. An open tab that kept it would save a stale age.
      const { closedAt: _closedAt, ...opening } = tab;
      tabs.splice(before >= 0 ? before : after >= 0 ? after + 1 : tabs.length, 0, { ...opening, groupId });
      return normalize({ ...state, tabs, groups: groups.map(g => (g.id === groupId ? { ...g, collapsed: false } : g)), closed: state.closed.filter(t => t.id !== a.id), activeId: tab.id, recentIds: [tab.id, ...state.recentIds.filter(id => id !== tab.id)] });
    }
    case 'restore-closed': {
      const present = new Set(state.tabs.map(t => t.id));
      const back = a.tabs.filter(t => !present.has(t.id));
      if (!back.length) return state;
      const groups = [...state.groups, ...a.groups.filter(g => !state.groups.some(have => have.id === g.id) && back.some(t => t.groupId === g.id)).map(g => ({ ...g }))];
      const byId = new Map<string, Tab>([...state.tabs, ...back].map(t => [t.id, t] as const));
      const placed = a.order.flatMap(id => (byId.has(id) ? [byId.get(id)!] : []));
      const rest = state.tabs.filter(t => !a.order.includes(t.id));
      const tabs = [...placed, ...rest].map(t => (t.groupId && !groups.some(g => g.id === t.groupId) ? { ...t, groupId: undefined } : t));
      const restored = new Set(back.map(t => t.id));
      const activeId = restored.has(a.activeId) ? a.activeId : state.activeId;
      return normalize({ ...state, tabs, groups, closed: state.closed.filter(t => !restored.has(t.id)), activeId, recentIds: [activeId, ...state.recentIds.filter(id => id !== activeId)] });
    }
    case 'duplicate': {
      const index = state.tabs.findIndex(t => t.id === a.id);
      if (index < 0 || !closable(state.tabs[index])) return state;
      const source = state.tabs[index];
      const copy: Tab = { ...source, id: createId(), pinned: false, split: source.split && { ...source.split, panes: source.split.panes.map(copyPane) } };
      const tabs = [...state.tabs];
      tabs.splice(index + 1, 0, copy);
      return normalize({ ...state, tabs, activeId: copy.id, recentIds: [copy.id, ...state.recentIds], nextNumber: state.nextNumber + 1 });
    }
    case 'ensure-inbox': {
      if (state.tabs.some(t => t.kind === 'inbox')) return state;
      const inbox: Tab = { id: inboxId, kind: 'inbox', title: 'Inbox', draft: '', pinned: true };
      return normalize({ ...state, tabs: [inbox, ...state.tabs], recentIds: [...state.recentIds, inbox.id] });
    }
    case 'open-inbox': {
      const ensured = reduceClosing(state, { type: 'ensure-inbox' }) ?? state;
      return { ...ensured, activeId: inboxId, recentIds: [inboxId, ...ensured.recentIds.filter(id => id !== inboxId)] };
    }
    default: return undefined;
  }
}

