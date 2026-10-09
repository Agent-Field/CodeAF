// The Go to chooser's arithmetic (Places 6c, "All places (⌘P)"): which rows it draws, in what order, at what depth,
// and which places a mode refuses to offer. Pure and React-free, so node tests read it without a browser; the dialog in
// GoToChooser.tsx only draws what this returns and keeps the person's expansion and query.

import type { ChooserMode, PlaceRowModel } from './contracts.ts';

/** The tree's top level sits under this key of `childrenOf`. */
export const rootKey = 'root';
/** Recent shows at most this many places (6c draws three). */
export const recentLimit = 3;

/** One drawn row. `key` is unique across the whole list, so a place under two parents is two rows with two keys. */
export type ChooserRow = {
  key: string;
  section: 'recent' | 'all' | 'results';
  place: PlaceRowModel;
  /** Tree depth: 0 at the top level. Recent and search results are flat and always 0. */
  depth: number;
  /** The tree path ("pl_a/pl_b"), which keys expansion: the same place under two parents opens independently. */
  path: string;
  hasChildren: boolean;
  expanded: boolean;
  /** The muted words after the name (" · codeaf"), without the separator. Empty when nothing is drawn there. */
  context: string;
  /** The right-hand words: a relative time in Recent, "4 inside" or "12 chats" elsewhere. Empty draws nothing. */
  meta: string;
  /** Drawn so its offered children stay reachable, but not itself a choice (an excluded place in `file` mode). */
  disabled: boolean;
};

export type ChooserView = {
  /** True while the person has typed something: Recent and the tree give way to one flat list of results. */
  searching: boolean;
  recent: ChooserRow[];
  tree: ChooserRow[];
  results: ChooserRow[];
  /** Every row in screen order, for ↑ and ↓. */
  rows: ChooserRow[];
};

export type ChooserData = {
  places: readonly PlaceRowModel[];
  childrenOf: ReadonlyMap<string, readonly string[]>;
  now: Date;
};

/** The person's own toggles, by tree path. A path not in the map takes the default: the top level open, deeper closed. */
export type Expansion = ReadonlyMap<string, boolean>;

export function isExpanded(expansion: Expansion, path: string, depth: number): boolean {
  return expansion.get(path) ?? depth === 0;
}

/** Every parent id of every place, read back from `childrenOf`. `root` is not a parent. */
export function parentsOf(childrenOf: ReadonlyMap<string, readonly string[]>): Map<string, string[]> {
  const parents = new Map<string, string[]>();
  for (const [parent, children] of childrenOf) {
    if (parent === rootKey) continue;
    for (const child of children) {
      const list = parents.get(child) ?? [];
      if (!list.includes(parent)) list.push(parent);
      parents.set(child, list);
    }
  }
  return parents;
}

/** A place and everything under it, at any depth. A cycle in a damaged graph ends the walk rather than looping. */
export function descendantsOf(id: string, childrenOf: ReadonlyMap<string, readonly string[]>): Set<string> {
  const seen = new Set<string>();
  const stack = [...(childrenOf.get(id) ?? [])];
  while (stack.length) {
    const next = stack.pop()!;
    if (seen.has(next) || next === id) continue;
    seen.add(next);
    stack.push(...(childrenOf.get(next) ?? []));
  }
  return seen;
}

/** The places a mode does not offer (contracts.ts ChooserMode). `go` offers everything. */
export function excludedIds(mode: ChooserMode, data: Pick<ChooserData, 'places' | 'childrenOf'>): Set<string> {
  switch (mode.kind) {
    case 'go': return new Set();
    case 'file': return new Set(mode.exclude);
    case 'merge': return new Set([mode.placeId, ...descendantsOf(mode.placeId, data.childrenOf)]);
    case 'parent': {
      const own = data.places.find(place => place.id === mode.placeId)?.parents ?? [];
      const read = parentsOf(data.childrenOf).get(mode.placeId) ?? [];
      return new Set([mode.placeId, ...descendantsOf(mode.placeId, data.childrenOf), ...own, ...read]);
    }
  }
}

/** The search field's placeholder and the dialog's name: the question being asked. */
export function chooserTitle(mode: ChooserMode): string {
  switch (mode.kind) {
    case 'go': return 'Go to a place, or create one';
    case 'merge': return `Merge “${mode.placeName}” into…`;
    case 'parent': return `Add “${mode.placeName}” to another place…`;
    case 'file': return `Add “${mode.chatTitle}” to a place…`;
  }
}

/** "58 places", or nothing when the count is unknown or zero (the emptiness law). */
export function countLabel(total: number): string {
  if (!Number.isFinite(total) || total <= 0) return '';
  return total === 1 ? '1 place' : `${total} places`;
}

const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const startOfDay = (at: Date) => new Date(at.getFullYear(), at.getMonth(), at.getDate()).getTime();

/** Recent's right-hand words: "just now", "2m ago", "1h ago", "yesterday", "3d ago", then "Sep 14". Unknown is ''. */
export function relativeWhen(iso: string | undefined, now: Date): string {
  if (!iso) return '';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime()) || Number.isNaN(now.getTime())) return '';
  const age = now.getTime() - at.getTime();
  if (age < 60_000) return 'just now';
  const minutes = Math.floor(age / 60_000);
  if (minutes < 60) return `${minutes}m ago`;
  // Calendar days, not 24-hour blocks: last night at eleven is "yesterday" at nine this morning.
  const days = Math.round((startOfDay(now) - startOfDay(at)) / 86_400_000);
  if (days <= 0) return `${Math.floor(minutes / 60)}h ago`;
  if (days === 1) return 'yesterday';
  if (days < 7) return `${days}d ago`;
  return `${months[at.getMonth()]} ${at.getDate()}`;
}

function rank(place: PlaceRowModel, ancestors: readonly string[], needle: string): number {
  const name = place.name.toLowerCase();
  if (name.startsWith(needle)) return 0;
  if (name.includes(needle)) return 1;
  return ancestors.some(ancestor => ancestor.toLowerCase().includes(needle)) ? 2 : -1;
}

/** Every ancestor name of a place, nearest first, each once. */
function ancestorNames(id: string, parents: ReadonlyMap<string, readonly string[]>, byId: ReadonlyMap<string, PlaceRowModel>): string[] {
  const names: string[] = [];
  const seen = new Set<string>([id]);
  let frontier = [...(parents.get(id) ?? [])];
  while (frontier.length) {
    const next: string[] = [];
    for (const parent of frontier) {
      if (seen.has(parent)) continue;
      seen.add(parent);
      const name = byId.get(parent)?.name;
      if (name) names.push(name);
      next.push(...(parents.get(parent) ?? []));
    }
    frontier = next;
  }
  return names;
}

/**
 * What the chooser draws for `query` in `mode`. With no query: Recent (up to three, newest first) then the All tree,
 * flattened to the rows that are visible under `expansion`. With a query: one flat list matched case-insensitively on
 * the place's name and its ancestors' names, name-prefix matches first, then other name matches, then ancestor-only
 * matches, each group in the graph's order. Excluded places are never offered; in the tree an excluded place is drawn,
 * disabled, only when something under it is still offered.
 */
export function chooserView(data: ChooserData, query: string, mode: ChooserMode, expansion: Expansion = new Map()): ChooserView {
  const live = data.places.filter(place => !place.archived);
  const byId = new Map(live.map(place => [place.id, place]));
  const excluded = excludedIds(mode, data);
  const parents = parentsOf(data.childrenOf);
  const parentName = (place: PlaceRowModel) => place.parentName ?? byId.get(parents.get(place.id)?.[0] ?? '')?.name ?? '';
  const needle = query.trim().toLowerCase();

  if (needle) {
    // Graph order: the tree's walk first, then any place the tree cannot reach, so results never depend on the list's shuffle.
    const order: string[] = [];
    const placed = new Set<string>();
    const walk = (id: string, trail: Set<string>) => {
      for (const child of data.childrenOf.get(id) ?? []) {
        if (trail.has(child)) continue;
        if (!placed.has(child)) { placed.add(child); order.push(child); }
        walk(child, new Set([...trail, child]));
      }
    };
    walk(rootKey, new Set());
    for (const place of live) if (!placed.has(place.id)) { placed.add(place.id); order.push(place.id); }
    const ranked = order.flatMap(id => {
      const place = byId.get(id);
      if (!place || excluded.has(id)) return [];
      const score = rank(place, ancestorNames(id, parents, byId), needle);
      return score < 0 ? [] : [{ place, score }];
    });
    ranked.sort((a, b) => a.score - b.score);
    const results = ranked.map(({ place }): ChooserRow => ({
      key: `found:${place.id}`, section: 'results', place, depth: 0, path: place.id, hasChildren: false, expanded: false,
      context: parentName(place), meta: place.meta ?? '', disabled: false,
    }));
    return { searching: true, recent: [], tree: [], results, rows: results };
  }

  const recent = live
    .filter(place => !excluded.has(place.id) && place.lastOpenedAt && !Number.isNaN(Date.parse(place.lastOpenedAt)))
    .sort((a, b) => Date.parse(b.lastOpenedAt!) - Date.parse(a.lastOpenedAt!))
    .slice(0, recentLimit)
    .map((place): ChooserRow => ({
      key: `recent:${place.id}`, section: 'recent', place, depth: 0, path: place.id, hasChildren: false, expanded: false,
      context: parentName(place), meta: relativeWhen(place.lastOpenedAt, data.now), disabled: false,
    }));

  // Whether anything at or under a place is offered; memoised, and a cycle counts as nothing offered.
  const offeredMemo = new Map<string, boolean>();
  const offered = (id: string, trail: ReadonlySet<string>): boolean => {
    const known = offeredMemo.get(id);
    if (known !== undefined) return known;
    if (!byId.has(id) || trail.has(id)) return false;
    const next = new Set([...trail, id]);
    const answer = !excluded.has(id) || (data.childrenOf.get(id) ?? []).some(child => offered(child, next));
    offeredMemo.set(id, answer);
    return answer;
  };

  const tree: ChooserRow[] = [];
  const visit = (parent: string, depth: number, prefix: string, trail: ReadonlySet<string>) => {
    for (const id of data.childrenOf.get(parent) ?? []) {
      const place = byId.get(id);
      if (!place || trail.has(id) || !offered(id, trail)) continue;
      const path = prefix ? `${prefix}/${id}` : id;
      const next = new Set([...trail, id]);
      const hasChildren = (data.childrenOf.get(id) ?? []).some(child => !next.has(child) && offered(child, next));
      const expanded = hasChildren && isExpanded(expansion, path, depth);
      tree.push({ key: `all:${path}`, section: 'all', place, depth, path, hasChildren, expanded, context: '', meta: place.meta ?? '', disabled: excluded.has(id) });
      if (expanded) visit(id, depth + 1, path, next);
    }
  };
  visit(rootKey, 0, '', new Set());

  return { searching: false, recent, tree, results: [], rows: [...recent, ...tree] };
}

/** The row ↑/↓ lands on from `from` (a row key), skipping disabled rows. With no current row, ↓ takes the first and ↑ the last. */
export function step(rows: readonly ChooserRow[], from: string | undefined, by: 1 | -1): string | undefined {
  const usable = rows.filter(row => !row.disabled || row.hasChildren);
  if (!usable.length) return undefined;
  const at = usable.findIndex(row => row.key === from);
  if (at === -1) return (by === 1 ? usable[0] : usable[usable.length - 1]).key;
  return usable[Math.min(usable.length - 1, Math.max(0, at + by))].key;
}

/** The first row ↵ would take: the first offered row. */
export function firstChoosable(rows: readonly ChooserRow[]): string | undefined {
  return rows.find(row => !row.disabled)?.key;
}

/** ← on a row inside the tree goes to its parent row; this finds that row's key. */
export function parentRowKey(row: ChooserRow): string | undefined {
  if (row.section !== 'all' || row.depth === 0) return undefined;
  return `all:${row.path.slice(0, row.path.lastIndexOf('/'))}`;
}
