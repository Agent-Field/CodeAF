import { useCallback, useState } from 'react';

// A text field matches :focus-visible on every click, so the ring needs the real input modality:
// keyboard focus is a focus that follows a key press with no pointer press since.
let byPointer = false;
if (typeof document !== 'undefined') {
  document.addEventListener('pointerdown', () => (byPointer = true), true);
  document.addEventListener('keydown', () => (byPointer = false), true);
}

/** `keyboard` is true only while the field holds focus that the keyboard put there. */
export function useKeyboardFocus() {
  const [keyboard, setKeyboard] = useState(false);
  const onFocus = useCallback(() => setKeyboard(!byPointer), []);
  const onBlur = useCallback(() => setKeyboard(false), []);
  return { keyboard, onFocus, onBlur };
}
