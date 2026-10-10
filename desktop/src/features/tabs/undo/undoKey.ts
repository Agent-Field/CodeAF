// The rule for when ⌘Z reaches the undo stack, apart from the hook so it can be proved without React.
//
// THE KEY BELONGS TO THE EDITOR WHILE ONE IS BEING TYPED IN. The recognizer already declines ⌘Z in a writing field;
// this repeats that for an editable region or a terminal being typed in, and leaves the key alone while a dialog
// other than the overview is open, so text undo and a dialog's own keys are never taken.
import { isEditingTarget } from '../../../design/editing.ts';
import type { Shortcut } from '../../../design/keyboard.ts';

/** True when an 'undo' shortcut is outside any editor and no dialog but the overview is open. */
export function shouldUndo(shortcut: Shortcut, target: EventTarget | null): boolean {
  if (shortcut.id !== 'undo' || isEditingTarget(target)) return false;
  return !document.querySelector('dialog[open]:not(.tab-overview)');
}
