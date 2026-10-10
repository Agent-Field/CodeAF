// Grouping selection is transient state in this window, separate from the active pane.
export type SelectionPress = { button: number; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean };

/** Only the platform's unmodified primary click toggles a grouping selection. */
export function isSelectionPress(event: SelectionPress, mac: boolean): boolean {
  return event.button === 0 && (mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey) && !event.altKey && !event.shiftKey;
}

/** Toggle without mutating the previous render's selection. */
export function toggleSelection(selected: readonly string[], id: string): string[] {
  return selected.includes(id) ? selected.filter(value => value !== id) : [...selected, id];
}

/** A remote workspace update keeps this window's picks only while their tabs remain open. */
export function retainSelection(selected: readonly string[], open: readonly { id: string }[]): string[] {
  const alive = new Set(open.map(tab => tab.id));
  return selected.filter(id => alive.has(id));
}

/** The tabs a group gesture covers: the whole selection when the pressed tab is in it, otherwise that tab alone. */
export function groupTargets(state: { picked?: readonly string[]; activeId: string }, pressedId: string): string[] {
  const picked = state.picked ?? [];
  return picked.length && (picked.includes(pressedId) || pressedId === state.activeId) ? [...picked] : [pressedId];
}
