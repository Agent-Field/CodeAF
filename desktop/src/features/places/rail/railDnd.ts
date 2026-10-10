/**
 * Index math for the rail's pin, unpin and reorder gestures (Places 10a, P-IX-9). Pure so the
 * pointer drop and the Alt+arrow chord land on the same slot and say the same sentence.
 */

/**
 * Where a place dragged inside Pinned must be inserted. The slot is read against the list as drawn,
 * but the dragged row leaves it first, so a drop below its own old position shifts up by one.
 */
export function pinnedDropIndex(pinned: readonly string[], dragged: string, slot: number): number {
  const from = pinned.indexOf(dragged);
  const clamped = Math.max(0, Math.min(slot, pinned.length));
  return from >= 0 && from < clamped ? clamped - 1 : clamped;
}

/** The slot an Alt+arrow moves a pinned row to; undefined at the ends or for a row that is not pinned. */
export function keyboardMoveTarget(pinned: readonly string[], id: string, key: 'ArrowUp' | 'ArrowDown'): number | undefined {
  const from = pinned.indexOf(id);
  if (from < 0) return undefined;
  const next = from + (key === 'ArrowUp' ? -1 : 1);
  return next >= 0 && next < pinned.length ? next : undefined;
}

/** What a screen reader hears after a reorder. Positions are one-based. */
export function movedAnnouncement(index: number, total: number): string {
  return `Moved to position ${index + 1} of ${total}`;
}
