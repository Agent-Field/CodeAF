// Whether a key event belongs to something the person is editing: a terminal, an editable region, or a text field
// they have typed into since it took focus. Shell shortcuts that mean something inside an editor (⌘Z above all) step
// aside for these, so the editor's own meaning wins: a text field's Undo, a terminal's Ctrl+Z.
//
// A FIELD THAT ONLY HAS FOCUS IS NOT BEING EDITED. The composer takes focus by itself whenever a tab is shown, so
// "focused" cannot mean "editing": a person who reopens a tab and presses ⌘Z again means the tabs, not an empty field.
// Typing (an `input` event) marks the field until it loses focus; focusing it again starts unmarked.

type Target = EventTarget | null | undefined;

const textInputs = new Set(['text', 'search', 'url', 'email', 'tel', 'password', 'number']);
const typed = new WeakSet<EventTarget>();
let watching = false;

function watch() {
  if (watching || typeof document === 'undefined') return;
  watching = true;
  document.addEventListener('input', event => { if (event.target) typed.add(event.target); }, true);
  document.addEventListener('focusout', event => { if (event.target) typed.delete(event.target); }, true);
}

/** A text field or text area that takes typing (not read-only, not disabled). */
function isTextField(element: Element & { type?: string; readOnly?: boolean; disabled?: boolean }): boolean {
  if (element.tagName === 'TEXTAREA') return !element.readOnly && !element.disabled;
  if (element.tagName === 'INPUT') return textInputs.has(element.type || 'text') && !element.readOnly && !element.disabled;
  return false;
}

export function isEditingTarget(target: Target): boolean {
  watch();
  const element = target as (Element & { isContentEditable?: boolean; type?: string; readOnly?: boolean; disabled?: boolean }) | null | undefined;
  if (!element || typeof element.closest !== 'function') return false;
  if (element.closest('.xterm')) return true;
  if (element.isContentEditable) return true;
  return isTextField(element) && typed.has(element);
}

/** Starts watching typing at once, so a field typed into before the first shortcut is already known. */
export const watchEditing = watch;
