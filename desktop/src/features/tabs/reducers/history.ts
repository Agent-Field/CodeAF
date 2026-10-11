// Reducer slice for the History tab (design 4a-4d): replacing it with the conversation a person continues, and
// archiving idle tabs. Opening it is the shell's one singleton door, openKindAction (shell/openKind.ts), which ⌘Y
// and the archive toast's Review both go through. A slice returns undefined for an action it does not own.
import { normalize, visibleTabs } from '../helpers.ts';
import { closedRecord, placeOf } from './tabs.ts';
import type { Tab, WorkspaceState } from '../types.ts';

export type HistoryAction =
  /** Puts `tab` where tab `id` sits (position, pin and group stay) and selects it: "Continue" replaces History. */
  | { type: 'history-replace'; id: string; tab: Tab }
  /** Takes tabs off the strip. They live on in History and also join the Reopen list, so the rail's Recent shows them (owner decision 2026-10-11). */
  | { type: 'history-archive'; ids: string[] };

/** How many closed tabs the Reopen list keeps; the close reducer holds the same bound. */
const closedLimit = 20;

export function reduceHistory(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as HistoryAction;
  switch (a.type) {
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
      const archived = state.tabs.filter(tab => gone.has(tab.id)).map(tab => closedRecord(tab, placeOf(state, tab)));
      return normalize({ ...state, tabs, activeId, closed: [...state.closed.filter(tab => !gone.has(tab.id)), ...archived].slice(-closedLimit) });
    }
    default:
      return undefined;
  }
}
