// The window's ⌘Z, as a seam around the workspace's dispatch (Workspace wires it in two lines; it renders nothing).
//
// Every structural change this window makes passes through `dispatch` here: it is predicted once with the pure
// reducer, pinned to the ids that prediction minted (so the real apply, and any replay over another window's tab set,
// lands on exactly the predicted tabs), recorded as an inverse step and then dispatched. ⌘Z takes the newest step.
//
// ⌘Z NEVER STEALS ANOTHER OWNER'S UNDO. A toast on screen that carries Undo (closing a running tab, a Places receipt)
// is what ⌘Z undoes first, through that toast, exactly as clicking its Undo would. In a text field, an editable region
// or a terminal being typed in, ⌘Z is left to that editor (design/editing.ts). With nothing to undo, the key does nothing at all.
import { useCallback, useRef, useState, type Dispatch } from 'react';
import { isEditingTarget, watchEditing } from '../../../design/editing';
import { shortcutLayer } from '../../../design/keyboard';
import { toasts } from '../../../design/toasts';
import { useShortcuts } from '../../../design/useShortcuts';
import { mintWith } from '../helpers';
import { workspaceReducer, type WorkspaceAction, type WorkspaceState } from '../model';
import { createStructuralUndo, isUndoable } from './structuralUndo';

type Options = { state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>; enabled: boolean };

/** The words when the newest step's tabs changed since (in another window, usually), so nothing was undone. */
export const undoRefusedMessage = 'Could not undo. These tabs have changed since.';

/** Only the overview may be open over the strip for ⌘Z to act; a rename or any other dialog keeps the key. */
const blockedByDialog = () => !!document.querySelector('dialog[open]:not(.tab-overview)');

export function useStructuralUndo({ state, dispatch, enabled }: Options) {
  const [stack] = useState(() => { watchEditing(); return createStructuralUndo(); });
  // The tab set as this window's own dispatches have left it, ahead of React's next render.
  const shadow = useRef(state);
  const rendered = useRef(state);
  if (rendered.current !== state) { rendered.current = state; shadow.current = state; }

  const recording = useCallback((action: WorkspaceAction) => {
    if (!isUndoable(action.type)) { dispatch(action); return; }
    const before = shadow.current;
    const { result: after, ids } = mintWith(action.mint, () => workspaceReducer(before, action));
    const pinned: WorkspaceAction = ids.length && !action.mint ? { ...action, mint: ids } : action;
    stack.record(before, pinned, after);
    shadow.current = after;
    dispatch(pinned);
  }, [dispatch, stack]);

  const undo = useCallback((): boolean => {
    const toast = toasts.getToast();
    if (toast?.undo) { toasts.undo(toast.id); return true; }
    const plan = stack.take(shadow.current);
    if (plan.kind === 'empty') return false;
    if (plan.kind === 'refused') { toasts.show({ message: [undoRefusedMessage] }); return true; }
    for (const action of plan.actions) { shadow.current = workspaceReducer(shadow.current, action); dispatch(action); }
    return true;
  }, [dispatch, stack]);

  useShortcuts(shortcutLayer.workspace, (shortcut, event) => {
    if (shortcut.id !== 'undo' || isEditingTarget(event.target) || blockedByDialog()) return false;
    return undo();
  }, enabled);

  return { dispatch: recording, undo, get steps() { return stack.size; } };
}
