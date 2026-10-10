// ⌘Z against the undo stack on the strip's state (reducers/undo.ts `withUndo`): the registry's 'undo' shortcut becomes
// the one `{ type: 'undo' }` action, the same action the closing toast's Undo dispatches. When it fires is undoKey.ts.
import type { Dispatch } from 'react';
import { watchEditing } from '../../../design/editing';
import { shortcutLayer } from '../../../design/keyboard';
import { useShortcuts } from '../../../design/useShortcuts';
import { undoAction, type UndoStackAction } from '../reducers/undo';
import { shouldUndo } from './undoKey';

export function useUndoKeys(dispatch: Dispatch<UndoStackAction>, enabled = true) {
  watchEditing();
  useShortcuts(shortcutLayer.workspace, (shortcut, event) => {
    if (!shouldUndo(shortcut, event.target)) return false;
    dispatch(undoAction);
    return true;
  }, enabled);
}
