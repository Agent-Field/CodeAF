// ⌘K goes to the New-tab field (Shell 3f/3k: the new tab is one field, not a page; SH-OQ5). Pure: the workspace
// hook feeds it the state and dispatches what it returns, so a second ⌘K never piles up empty tabs.
import { focusedPane, type WorkspaceAction, type WorkspaceState } from '../tabs/model.ts';
import { panesOf } from '../tabs/helpers.ts';

export const newTabFieldEvent = 'codeaf:new-tab-field';
/** Ask the workspace to show the New-tab field and put the caret in it. */
export function requestNewTabField() {
  window.dispatchEvent(new CustomEvent(newTabFieldEvent));
}

/** A New tab is empty until words are typed into it; a draft means somebody is already using it. */
const isEmptyNewTab = (pane: { kind: string; draft?: string }) => pane.kind === 'newtab' && !pane.draft;

/** Nothing to do when the active tab already is the empty field; select the empty one that exists; otherwise open one. */
export function newTabFieldAction(state: WorkspaceState): WorkspaceAction | undefined {
  const active = state.tabs.find(tab => tab.id === state.activeId);
  if (active && isEmptyNewTab(focusedPane(active))) return undefined;
  const existing = state.tabs.map(tab => panesOf(tab).find(isEmptyNewTab)).find(Boolean);
  return existing ? { type: 'select', id: existing.id } : { type: 'new' };
}
