// Where ⌘O (registry open-file) lands, and which field should ask for a file name. Pure, so a node test covers it
// without mounting the workspace. Shell 3f draws the chord on the row and does not say what another tab does with it.
import { focusedPane, panesOf, type WorkspaceState } from '../../model.ts';

export type OpenFilePlan = { kind: 'focus'; paneId: string } | { kind: 'open' };

/**
 * A New tab already in front is focused. Otherwise one that is open (the unfocused pane of the active split, else
 * the most recently used) is focused. With none open, a New tab is created.
 */
export function planOpenFile(state: WorkspaceState): OpenFilePlan {
  const active = state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0];
  if (!active) return { kind: 'open' };
  const here = focusedPane(active);
  if (here.kind === 'newtab') return { kind: 'focus', paneId: here.id };
  const beside = active.split?.panes.find(pane => pane.kind === 'newtab');
  if (beside) return { kind: 'focus', paneId: beside.id };
  const open = state.tabs.flatMap(tab => panesOf(tab).filter(pane => pane.kind === 'newtab').map(pane => ({ pane, tabId: tab.id })));
  if (!open.length) return { kind: 'open' };
  const recent = state.recentIds.map(id => open.find(item => item.tabId === id)).find(item => item !== undefined) ?? open[0];
  return { kind: 'focus', paneId: recent.pane.id };
}

/** Which field should show "Type part of a file name." No pane id means the next New tab that mounts. */
let armed: { paneId?: string } | null = null;

export function armFilePrompt(paneId?: string) {
  armed = { paneId };
}

export function filePromptArmed(paneId: string): boolean {
  if (!armed) return false;
  return armed.paneId === undefined || armed.paneId === paneId;
}

/** Drops the prompt once the field that was asked has taken it. A different pane leaves it for its owner. */
export function settleFilePrompt(paneId: string) {
  if (filePromptArmed(paneId)) armed = null;
}
