// Where each pane's scrollers were left. Pure (no React, no DOM) so a node test can pin the bounds and the validation.
// A pane id is stable across tab switches, splits, reloads and Reopen, so it is the key; the saved tab list (plus the closed ring) decides which ids are alive.

/** One scroller's resting place: its offsets, and whether it was at its vertical end (a conversation following its tail has no meaningful offset). */
export type ScrollSpot = { top: number; left: number; end: boolean };

/** Scrollers remembered per pane, and panes remembered overall. The least recently touched is dropped first. */
export const MAX_SPOTS_PER_PANE = 16;
export const MAX_PANES = 128;
/** Panes kept after their tab closed, for Reopen: the closed ring holds 20 tabs and a split tab holds up to 4 panes. */
export const MAX_RETIRED_PANES = 80;

const isOffset = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0;

/** `left` was added after the first save format; a spot without one is a vertical-only spot. */
function readSpot(value: unknown): ScrollSpot | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const { top, left, end } = value as Record<string, unknown>;
  if (!isOffset(top) || typeof end !== 'boolean' || (left !== undefined && !isOffset(left))) return undefined;
  return { top, left: left ?? 0, end };
}

/** Keeps the newest `limit` entries of an insertion-ordered map. */
function trim<K, V>(map: Map<K, V>, limit: number) {
  while (map.size > limit) map.delete(map.keys().next().value as K);
}

export class ScrollMemory {
  private readonly panes = new Map<string, Map<string, ScrollSpot>>();
  /** Pane ids whose tab left the strip but may still come back, oldest first. */
  private retired: string[] = [];

  /** A copy, so a restore in progress never sees the memory move under it. */
  get(paneId: string): Map<string, ScrollSpot> {
    return new Map(this.panes.get(paneId) ?? []);
  }

  set(paneId: string, key: string, spot: ScrollSpot): void {
    const spots = this.panes.get(paneId) ?? new Map<string, ScrollSpot>();
    // Re-inserting moves the pane and the spot to the newest end of their maps.
    this.panes.delete(paneId);
    spots.delete(key);
    spots.set(key, { top: Math.max(0, Math.round(spot.top)), left: Math.max(0, Math.round(spot.left)), end: spot.end });
    trim(spots, MAX_SPOTS_PER_PANE);
    this.panes.set(paneId, spots);
    trim(this.panes, MAX_PANES);
  }

  /**
   * Reconciles the memory with the panes that exist. A pane in `alive` is kept (and is no longer retired). A pane that is gone is
   * dropped at once when `retained` is given and does not name it (the owner of the closed ring is authoritative); otherwise it
   * is retired, and only the oldest retired panes beyond MAX_RETIRED_PANES are dropped. Returns whether anything changed.
   */
  prune(alive: ReadonlySet<string>, retained?: ReadonlySet<string>): boolean {
    let changed = false;
    const retire = (id: string) => { this.retired = this.retired.filter(other => other !== id); };
    for (const id of alive) if (this.retired.includes(id)) { retire(id); changed = true; }
    for (const id of [...this.panes.keys()]) {
      if (alive.has(id)) continue;
      if (retained) {
        if (retained.has(id)) continue;
        this.panes.delete(id); retire(id); changed = true;
      } else if (!this.retired.includes(id)) { this.retired.push(id); changed = true; }
    }
    // A retired id with nothing remembered, or no longer retained, holds no place in the ring.
    const kept = this.retired.filter(id => this.panes.has(id) && (!retained || retained.has(id)));
    if (kept.length !== this.retired.length) { this.retired = kept; changed = true; }
    while (this.retired.length > MAX_RETIRED_PANES) { this.panes.delete(this.retired.shift()!); changed = true; }
    return changed;
  }

  get size(): number { return this.panes.size; }
  retiredIds(): string[] { return [...this.retired]; }

  serialize(): string {
    const panes: Record<string, Record<string, ScrollSpot>> = {};
    for (const [id, spots] of this.panes) panes[id] = Object.fromEntries(spots);
    return JSON.stringify({ v: 2, panes, retired: this.retired });
  }

  /** Anything malformed is dropped entry by entry: a bad save must cost one scroll position, never the workspace. */
  static parse(text: string | null | undefined): ScrollMemory {
    const memory = new ScrollMemory();
    if (!text) return memory;
    let raw: unknown;
    try { raw = JSON.parse(text); } catch { return memory; }
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return memory;
    const doc = raw as { v?: unknown; panes?: unknown; retired?: unknown };
    if (doc.v !== 2 || !doc.panes || typeof doc.panes !== 'object' || Array.isArray(doc.panes)) return memory;
    for (const [id, spots] of Object.entries(doc.panes as Record<string, unknown>)) {
      if (!spots || typeof spots !== 'object' || Array.isArray(spots)) continue;
      for (const [key, value] of Object.entries(spots as Record<string, unknown>)) { const spot = readSpot(value); if (spot) memory.set(id, key, spot); }
    }
    if (Array.isArray(doc.retired)) memory.retired = doc.retired.filter((id): id is string => typeof id === 'string' && memory.panes.has(id)).slice(-MAX_RETIRED_PANES);
    return memory;
  }
}
