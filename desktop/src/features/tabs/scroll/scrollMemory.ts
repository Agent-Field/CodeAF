// Where each pane's scrollers were left. Pure (no React, no DOM) so a node test can pin the bounds and the validation.
// A pane id is stable across tab switches, splits and reloads, so it is the key; the saved tab list is what decides which ids are alive.

/** One scroller's resting place: its offset, and whether it was at its end (a conversation following its tail has no meaningful offset). */
export type ScrollSpot = { top: number; end: boolean };

/** Scrollers remembered per pane, and panes remembered overall. The least recently touched is dropped first. */
export const MAX_SPOTS_PER_PANE = 16;
export const MAX_PANES = 64;

export const scrollStorageKey = 'codeaf.desktop.tabScroll.v1';

const isSpot = (value: unknown): value is ScrollSpot => {
  if (!value || typeof value !== 'object') return false;
  const { top, end } = value as Record<string, unknown>;
  return typeof top === 'number' && Number.isFinite(top) && top >= 0 && typeof end === 'boolean';
};

/** Keeps the newest `limit` entries of an insertion-ordered map. */
function trim<K, V>(map: Map<K, V>, limit: number) {
  while (map.size > limit) map.delete(map.keys().next().value as K);
}

export class ScrollMemory {
  private readonly panes = new Map<string, Map<string, ScrollSpot>>();

  /** A copy, so a restore in progress never sees the memory move under it. */
  get(paneId: string): Map<string, ScrollSpot> {
    return new Map(this.panes.get(paneId) ?? []);
  }

  set(paneId: string, key: string, spot: ScrollSpot): void {
    const spots = this.panes.get(paneId) ?? new Map<string, ScrollSpot>();
    // Re-inserting moves the pane and the spot to the newest end of their maps.
    this.panes.delete(paneId);
    spots.delete(key);
    spots.set(key, { top: Math.max(0, Math.round(spot.top)), end: spot.end });
    trim(spots, MAX_SPOTS_PER_PANE);
    this.panes.set(paneId, spots);
    trim(this.panes, MAX_PANES);
  }

  /** Forgets every pane whose id is not in `alive`; returns whether anything was dropped. */
  prune(alive: ReadonlySet<string>): boolean {
    let dropped = false;
    for (const id of [...this.panes.keys()]) if (!alive.has(id)) { this.panes.delete(id); dropped = true; }
    return dropped;
  }

  get size(): number { return this.panes.size; }

  serialize(): string {
    const out: Record<string, Record<string, ScrollSpot>> = {};
    for (const [id, spots] of this.panes) out[id] = Object.fromEntries(spots);
    return JSON.stringify(out);
  }

  /** Anything malformed is dropped entry by entry: a bad save must cost one scroll position, never the workspace. */
  static parse(text: string | null | undefined): ScrollMemory {
    const memory = new ScrollMemory();
    if (!text) return memory;
    let raw: unknown;
    try { raw = JSON.parse(text); } catch { return memory; }
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return memory;
    for (const [id, spots] of Object.entries(raw as Record<string, unknown>)) {
      if (!spots || typeof spots !== 'object' || Array.isArray(spots)) continue;
      for (const [key, spot] of Object.entries(spots as Record<string, unknown>)) if (isSpot(spot)) memory.set(id, key, spot);
    }
    return memory;
  }
}
