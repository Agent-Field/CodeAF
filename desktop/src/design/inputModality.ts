// Focus rings are for the keyboard only (conversation 1f). A text field matches :focus-visible on every click,
// so the stylesheet cannot tell a click from a Tab by itself. A mouse press marks the root as pointer-driven and
// the next key press that is not typing into a field clears the mark; ui.css draws the shared ring unless the
// root carries data-input="pointer". Typing after a click therefore never makes the field's ring appear.
const TEXT_FIELD = 'input, textarea, select, [contenteditable="true"]';

if (typeof document !== 'undefined') {
  const root = document.documentElement;
  document.addEventListener('pointerdown', () => { root.dataset.input = 'pointer'; }, true);
  document.addEventListener('keydown', (event) => {
    const typing = event.key !== 'Tab' && (event.target as Element | null)?.closest?.(TEXT_FIELD);
    if (!typing) delete root.dataset.input;
  }, true);
}
