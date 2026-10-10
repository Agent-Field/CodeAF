export type RailKey = 'ArrowUp' | 'ArrowDown';

/**
 * ↑↓ through the rail list (PL-045). The design names the keys and not wrapping,
 * so the move stops on the first and last row instead of cycling.
 * An index outside the list is left alone; the caller still swallows the key.
 */
export function stepRail(length: number, index: number, key: RailKey): number {
  if (length <= 0 || index < 0 || index >= length) return index;
  const next = index + (key === 'ArrowDown' ? 1 : -1);
  if (next < 0 || next >= length) return index;
  return next;
}

/**
 * Places 8e draws All places with no shortcut when nothing has been created yet.
 * A graph that has not been read leaves the shortcut up: unknown is not zero.
 */
export function railShortcutVisible(livePlaces: number | undefined): boolean {
  return livePlaces === undefined || livePlaces > 0;
}

/** Archived places are absent from the rail (PL-046). A section of only those draws no header. */
export function shownPlaces<T extends { archived: boolean }>(places: readonly T[]): T[] {
  return places.filter(place => !place.archived);
}
