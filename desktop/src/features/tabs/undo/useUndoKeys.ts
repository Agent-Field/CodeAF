// The window's undo shortcut reaches its structural inverse stack. Snapshot consumers may still pass a
// dispatch for the older undo action. The shared key policy leaves editors and other dialogs their own undo.
import type { Dispatch } from 'react';
import { watchEditing } from '../../../design/editing';
import { shortcutLayer } from '../../../design/keyboard';
import { useShortcuts } from '../../../design/useShortcuts';
import { undoAction, type UndoStackAction } from '../reducers/undo';
import { shouldUndo } from './undoKey';

export function useUndoKeys(dispatch: Dispatch<UndoStackAction> | { undo: () => boolean }, enabled = true) {
  watchEditing();
  useShortcuts(shortcutLayer.workspace, (shortcut, event) => {
    if (!shouldUndo(shortcut, event.target)) return false;
    // The live workspace uses guarded inverses; snapshot consumers keep their existing action seam.
    if (typeof dispatch !== 'function') return dispatch.undo();
    dispatch(undoAction);
    return true;
  }, enabled);
}
