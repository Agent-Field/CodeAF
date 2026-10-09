// Reducer slice for the History tab (design 4a-4d): opening it, replacing it with the conversation a person
// continues, and archiving idle tabs. A slice returns undefined for an action it does not own.
import { newTab, normalize, visibleTabs } from '../helpers.ts';
import type { Tab, WorkspaceState } from '../types.ts';

export const historyTitle = 'History';

export type HistoryAction =
  /** Selects the History tab, or opens one at the end of the strip when there is none. */
  | { type: 'history-open' }
  /** Puts `tab` where tab `id` sits (position, pin and group stay) and selects it: "Continue" replaces History. */
  | { type: 'history-replace'; id: string; tab: Tab }
  /** Takes tabs off the strip without adding them to the Reopen list: they live on in History. */
  | { type: 'history-archive'; ids: string[] };

export function reduceHistory(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as HistoryAction;
  switch (a.type) {
    case 'history-open': {
      const held = state.tabs.find(tab => !tab.split && tab.kind === 'history');
      if (held) return normalize({ ...state, activeId: held.id, recentIds: [held.id, ...state.recentIds.filter(id => id !== held.id)] });
      const tab = newTab({ kind: 'history', title: historyTitle, titleSource: 'engine' });
      return { ...state, tabs: [...state.tabs, tab], activeId: tab.id, recentIds: [tab.id, ...state.recentIds] };
    }
    case 'history-replace': {
      const at = state.tabs.findIndex(tab => tab.id === a.id);
      if (at < 0) return state;
      const old = state.tabs[at];
      const tab: Tab = { ...a.tab, pinned: old.pinned, groupId: old.groupId };
      const tabs = state.tabs.map((one, index) => (index === at ? tab : one));
      return normalize({ ...state, tabs, activeId: state.activeId === old.id ? tab.id : state.activeId, recentIds: [tab.id, ...state.recentIds.filter(id => id !== old.id && id !== tab.id)] });
    }
    case 'history-archive': {
      const gone = new Set(a.ids);
      const tabs = state.tabs.filter(tab => !gone.has(tab.id));
      if (tabs.length === state.tabs.length || !tabs.length) return state;
      const order = visibleTabs(state);
      const activeId = tabs.some(tab => tab.id === state.activeId) ? state.activeId : (order.find(tab => !gone.has(tab.id)) ?? tabs[0]).id;
      return normalize({ ...state, tabs, activeId });
    }
    default:
      return undefined;
  }
}
