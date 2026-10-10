// ⌘G and the tab menu's "New group…" share one path (Shell 2h; Interactions Shortcuts ⌘G): the selected tabs, or the
// active tab alone, become a new group, the selection clears, and rename opens on the new group's label.
import { useCallback, useEffect, useRef, type Dispatch } from 'react';
import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import type { WorkspaceAction, WorkspaceState } from './model';
import { groupTargets } from './selection';

type Options = {
  enabled: boolean; state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>;
  startRename: (id: string, group?: boolean) => void;
};

/** Registers ⌘G and returns the function the menu calls, so both go through the same reducer action and rename. */
export function useGroupKeys({ enabled, state, dispatch, startRename }: Options) {
  // The reducer mints the group's id, so the rename waits for the group to appear in state rather than guessing it.
  const awaiting = useRef<Set<string> | null>(null);
  const groupSelected = useCallback((pressedId?: string) => {
    const id = pressedId ?? state.activeId;
    const [first, ...rest] = groupTargets(state, id);
    awaiting.current = new Set(state.groups.map(group => group.id));
    dispatch({ type: 'group', id: first, ids: rest });
  }, [state, dispatch]);

  useEffect(() => {
    const before = awaiting.current;
    if (!before) return;
    const made = state.groups.find(group => !before.has(group.id));
    // The Inbox cannot be grouped, so a press on it makes nothing and must not leave the wait armed for a later group.
    awaiting.current = null;
    if (made) startRename(made.id, true);
  }, [state.groups]);

  useShortcuts(shortcutLayer.workspace, shortcut => {
    if (shortcut.id !== 'group' || document.querySelector('dialog[open]')) return false;
    groupSelected();
    return true;
  }, enabled);

  return groupSelected;
}
