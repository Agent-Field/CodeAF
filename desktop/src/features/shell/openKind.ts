// Opening a tab of a given kind from the shell (the rail's Settings item, ⌘Y History): one tab per kind, so
// a second request focuses the tab that is already open. Pure: the workspace hook feeds it the state.
import { SETTINGS_TAB_TITLE } from '../settings/summary.ts';
import { newTab, panesOf } from '../tabs/helpers.ts';
import type { TabKind } from '../tabs/kinds/types.ts';
import type { Tab, TitleSource, WorkspaceAction, WorkspaceState } from '../tabs/model.ts';

/** The kinds the shell opens by name. Each is a singleton tab. */
export type ShellKind = Extract<TabKind, 'settings' | 'history'>;

const titles: Record<ShellKind, string> = { settings: SETTINGS_TAB_TITLE, history: 'History' };
/** History names its own tab while searching ("History · lexer"), so its opening title must not outrank the pane's. */
const titleSources: Record<ShellKind, TitleSource> = { settings: 'manual', history: 'engine' };

const holdsKind = (tab: Tab, kind: ShellKind) => panesOf(tab).some(pane => pane.kind === kind);

/** The action that leaves the tab of `kind`: the most recent tab that is not one, or a new tab when there is none. */
export function leaveKindAction(state: WorkspaceState, kind: ShellKind): WorkspaceAction {
  const other = state.recentIds.map(id => state.tabs.find(tab => tab.id === id)).find(tab => tab && !holdsKind(tab, kind));
  return other ? { type: 'select', id: other.id } : { type: 'new' };
}

/** The action that shows the tab of `kind`: select it when it exists, open it (and focus it) when it does not. */
export function openKindAction(state: WorkspaceState, kind: ShellKind): WorkspaceAction {
  const existing = state.tabs.find(tab => holdsKind(tab, kind));
  if (existing) return { type: 'select', id: panesOf(existing).find(pane => pane.kind === kind)!.id };
  return { type: 'open', background: false, tab: newTab({ kind, title: titles[kind], titleSource: titleSources[kind] }) };
}
