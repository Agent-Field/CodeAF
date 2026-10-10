/** Which on-screen web pane a focus-address chord belongs to. */
export function addressPane(panes: readonly string[], requested?: string): string | undefined {
  if (requested) return panes.includes(requested) ? requested : undefined;
  return panes[0];
}

/**
 * Moves the keyboard to that pane's address. A named pane that is not on
 * screen is left alone, so a chord for a hidden page never edits another one.
 * The field at rest is a button; clicking it is how it becomes the input.
 */
export function focusAddress(pane?: string) {
  const panes = [...document.querySelectorAll<HTMLElement>('.web-pane[data-pane]')];
  const id = addressPane(panes.map(el => el.dataset.pane ?? '').filter(Boolean), pane);
  const root = panes.find(el => el.dataset.pane === id);
  if (!root) return;
  const input = root.querySelector<HTMLInputElement>('.web-address-input');
  if (input) {
    input.focus();
    input.select();
    return;
  }
  root.querySelector<HTMLElement>('.web-address')?.click();
}
