// The place palette's arithmetic (Places 6c): Recent, the flattened All tree, fuzzy search over name and path, the
// "Create “name”" row when nothing matches, and the windowed slice a long list draws. Pure, React-free and clock-free
// (the caller injects `now`), so a node test pins every row. It reads the same `PlaceRowModel` and `childrenOf` the
// Go to chooser reads, and reuses its expansion rules and relative-time words so the two never disagree.

import type { PlaceRowModel } from '../shell/contracts.ts';
import { isExpanded, recentLimit, relativeWhen, rootKey, type Expansion } from '../shell/chooserModel.ts';

export type PaletteRow =
  | {
    kind: 'place';
    /** Unique across the list: a place under two parents is two tree rows with two keys. */
    key: string;
    section: 'recent' | 'all' | 'results';
    place: PlaceRowModel;
    /** Tree depth; Recent and results are flat and always 0. */
    depth: number;
    /** The tree path ("pl_a/pl_b"), which keys expansion. */
    path: string;
    hasChildren: boolean;
    expanded: boolean;
    /** The muted trail after the name ("codeaf › Software"), empty at the top level and inside the tree. */
    context: string;
    /** Right-hand words: a relative time in Recent, "4 inside" or "12 chats" elsewhere. Empty draws nothing. */
    meta: string;
    /** Character offsets in `place.name` the query matched, for the bold run. Empty when it matched the path only. */
    matched: readonly number[];
  }
  | { kind: 'create'; key: 'create'; name: string; label: string };

export type PaletteView = {
  searching: boolean;
  rows: PaletteRow[];
};

export type PaletteData = {
  /** Every non-archived place; archived ones are dropped here too. */
  places: readonly PlaceRowModel[];
  childrenOf: ReadonlyMap<string, readonly string[]>;
  now: Date;
};

/** The ancestors' separator in a result's trail. */
export const TRAIL_SEPARATOR = ' › ';

/** First-parent chain of names, outermost first, ending before the place itself. A cycle in a damaged graph ends it. */
function trailOf(id: string, parentOf: ReadonlyMap<string, string>, byId: ReadonlyMap<string, PlaceRowModel>): string[] {
  const names: string[] = [];
  const seen = new Set<string>([id]);
  for (let at = parentOf.get(id); at && !seen.has(at); at = parentOf.get(at)) {
    seen.add(at);
    const name = byId.get(at)?.name;
    if (name) names.unshift(name);
  }
  return names;
}

type Match = { score: number; offsets: number[] };

/**
 * Subsequence match of `needle` in `text` (both already lower-cased). Consecutive runs and word starts score higher,
 * a gap costs a little, and an earlier start wins a tie. Null when the letters do not all appear in order.
 */
export function fuzzy(needle: string, text: string): Match | null {
  const offsets: number[] = [];
  let score = 0;
  let from = 0;
  let run = 0;
  for (const ch of needle) {
    const at = text.indexOf(ch, from);
    if (at === -1) return null;
    const consecutive = offsets.length > 0 && at === from;
    const wordStart = at === 0 || /[\s\-_/›.]/.test(text[at - 1]);
    run = consecutive ? run + 1 : 0;
    score += 1 + run * 2 + (wordStart ? 3 : 0) - Math.min(at - from, 4) * 0.25;
    offsets.push(at);
    from = at + 1;
  }
  if (text.startsWith(needle)) score += 10;
  return { score: score - offsets[0] * 0.1, offsets };
}

/** A name match always outranks a path-only match, however good the path's letters line up. */
const NAME_BAND = 1000;

function search(needle: string, byId: Map<string, PlaceRowModel>, parentOf: Map<string, string>, live: PlaceRowModel[]): PaletteRow[] {
  const found: { row: PaletteRow; score: number; order: number }[] = [];
  live.forEach((place, order) => {
    const trail = trailOf(place.id, parentOf, byId);
    const byName = fuzzy(needle, place.name.toLowerCase());
    // The haystack for a path match is the whole trail plus the name, so "cod soft" finds Software inside codeaf.
    const byPath = byName ? null : fuzzy(needle, [...trail, place.name].join(TRAIL_SEPARATOR).toLowerCase());
    if (!byName && !byPath) return;
    found.push({
      order,
      score: byName ? NAME_BAND + byName.score : byPath!.score,
      row: {
        kind: 'place', key: `found:${place.id}`, section: 'results', place, depth: 0, path: place.id,
        hasChildren: false, expanded: false, context: trail.join(TRAIL_SEPARATOR), meta: place.meta ?? '',
        matched: byName?.offsets ?? [],
      },
    });
  });
  // Array.sort is stable, but the explicit order tie-break keeps the graph's order a stated rule rather than an accident.
  found.sort((a, b) => b.score - a.score || a.order - b.order);
  return found.map(entry => entry.row);
}

/** The label of the row that makes a place from the typed words. */
export function createLabel(name: string): string {
  return `Create “${name}”`;
}

/**
 * What the palette draws. No query: Recent (newest `lastOpenedAt` first, at most `recentLimit`) then the All tree
 * flattened to the rows visible under `expansion`. A query: one flat, ranked list of fuzzy matches on name and path,
 * and when nothing matches, a single Create row for the trimmed words. A place absent from the live list is never drawn.
 */
export function paletteView(data: PaletteData, query: string, expansion: Expansion = new Map()): PaletteView {
  const live = data.places.filter(place => !place.archived);
  const byId = new Map(live.map(place => [place.id, place]));
  const parentOf = new Map<string, string>();
  for (const [parent, children] of data.childrenOf) {
    if (parent === rootKey) continue;
    // The first parent in the graph's order decides the trail; a later one is "also in", not a path.
    for (const child of children) if (!parentOf.has(child)) parentOf.set(child, parent);
  }

  const typed = query.trim();
  if (typed) {
    const rows = search(typed.toLowerCase(), byId, parentOf, live);
    return { searching: true, rows: rows.length ? rows : [{ kind: 'create', key: 'create', name: typed, label: createLabel(typed) }] };
  }

  const recent = live
    .filter(place => place.lastOpenedAt && !Number.isNaN(Date.parse(place.lastOpenedAt)))
    .sort((a, b) => Date.parse(b.lastOpenedAt!) - Date.parse(a.lastOpenedAt!))
    .slice(0, recentLimit)
    .map((place): PaletteRow => ({
      kind: 'place', key: `recent:${place.id}`, section: 'recent', place, depth: 0, path: place.id, hasChildren: false,
      expanded: false, context: trailOf(place.id, parentOf, byId).join(TRAIL_SEPARATOR), meta: relativeWhen(place.lastOpenedAt, data.now), matched: [],
    }));

  const tree: PaletteRow[] = [];
  const visit = (parent: string, depth: number, prefix: string, trail: ReadonlySet<string>) => {
    for (const id of data.childrenOf.get(parent) ?? []) {
      const place = byId.get(id);
      if (!place || trail.has(id)) continue;
      const path = prefix ? `${prefix}/${id}` : id;
      const next = new Set([...trail, id]);
      const hasChildren = (data.childrenOf.get(id) ?? []).some(child => byId.has(child) && !next.has(child));
      const expanded = hasChildren && isExpanded(expansion, path, depth);
      tree.push({ kind: 'place', key: `all:${path}`, section: 'all', place, depth, path, hasChildren, expanded, context: '', meta: place.meta ?? '', matched: [] });
      if (expanded) visit(id, depth + 1, path, next);
    }
  };
  visit(rootKey, 0, '', new Set());
  return { searching: false, rows: [...recent, ...tree] };
}

export type WindowSlice = {
  /** Index of the first row to draw. */
  start: number;
  /** One past the last row to draw. */
  end: number;
  /** Space above the first drawn row, so the scrollbar keeps the whole list's length. */
  padTop: number;
  /** Space below the last drawn row. */
  padBottom: number;
};

/**
 * Which rows of a long list to draw for a scroll position, plus `overscan` rows either side so a fast scroll never
 * shows a blank edge. Fixed row height, because the palette's rows are all one height (6c).
 */
export function windowSlice(total: number, scrollTop: number, viewport: number, rowHeight: number, overscan = 6): WindowSlice {
  if (!(total > 0) || !(rowHeight > 0)) return { start: 0, end: 0, padTop: 0, padBottom: 0 };
  const first = Math.floor(Math.max(0, scrollTop) / rowHeight);
  const visible = Math.ceil(Math.max(0, viewport) / rowHeight);
  const start = Math.min(total, Math.max(0, first - overscan));
  const end = Math.min(total, first + visible + overscan);
  return { start, end, padTop: start * rowHeight, padBottom: (total - end) * rowHeight };
}
