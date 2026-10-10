// Pure readings of a place graph for the words and sections the desktop draws (Places 9d, 10a, 6c, 8a, 8c).
// React-free and clock-free so a node test can pin every sentence. The engine computes the same tint and the same
// roll-up on its side; this copy exists so a window can apply its own Open list, and so a fixture with no engine
// still says the design's words. A zero or an unknown is left out (the emptiness law). A place's manager is reserved
// and is not a field here, because it changes nothing a person reads.

import type { Membership, Tint } from './wire.ts';

/** The five hues a new top-level place may be given, in the palette's order. Graphite is "no tint chosen", not a pick. */
const PICKABLE_TINTS: readonly Tint[] = ['tide', 'iris', 'rose', 'sand', 'sage'];

/** How long an Open place may sit untouched before it leaves the rail (Places 10a). Pinned places are never on this clock. */
export const OPEN_IDLE_MS = 12 * 60 * 60 * 1000;

/** ⌃1–9. Places 9c numbers Pinned and then Open; the tenth place has no number. */
export const PLACE_NUMBER_MAX = 9;

/** The muted words on a place the person closed while it was still running or waiting (Places 10a). */
export const CLOSED_STILL_RUNNING = 'closed · still running';

/** One place as the graph stores it. `tint` empty means "inherit". Derived words are computed, never stored. */
export type PlaceRecord = {
  id: string;
  name: string;
  /** Ordered. The first parent decides an inherited tint. None means top-level. */
  parents: readonly string[];
  /** This place's own choice. Empty inherits. */
  tint?: Tint | '';
  archived?: boolean;
};

/** The membership edge the words need. `addedBy` is the store's, and no sentence here depends on it. */
export type PlaceMembership = Pick<Membership, 'chatId' | 'placeId'>;

export type PlaceStore = {
  places: readonly PlaceRecord[];
  memberships?: readonly PlaceMembership[];
  /** Pinned ids in the person's order. A pinned parent does not pull its children in. */
  pinned?: readonly string[];
};

/** What one chat is doing. Absent, zero and unknown count as nothing. */
export type ChatActivity = {
  /** Questions waiting on the person. */
  needsYou?: number;
  /** The chat has a failed task. */
  failed?: boolean;
  running?: boolean;
};

/** One place this window has gone to. Newest `touchedAt` is the top of Open. */
export type WindowVisit = {
  placeId: string;
  /** ISO instant. Unparseable is kept, and is not treated as twelve hours idle. */
  touchedAt: string;
  /** The person closed it in this window. */
  closed?: boolean;
};

export type WindowOpen = {
  visits: readonly WindowVisit[];
  /** ISO now, so the twelve-hour rule does not read the clock. */
  now: string;
};

export type Crumb = { id: string; name: string };

export type RollupWords = { status: 'waiting' | 'failed'; words: string };

export type RailRow = {
  id: string;
  name: string;
  tint: Tint;
  /** The first parent's name ("Config parser · codeaf"). Absent at the top level. */
  parentName?: string;
  status?: 'waiting' | 'failed';
  /** "2 need you in Config parser", or the failed sentence. */
  statusLabel?: string;
  /** Closed here, but work is still running or waiting, so the row stays. */
  closedButRunning?: true;
  closedLabel?: typeof CLOSED_STILL_RUNNING;
  /** 1–9 across Pinned then Open. Absent past the ninth. */
  number?: number;
};

export type RailSections = { pinned: RailRow[]; open: RailRow[] };

type Index = {
  byId: Map<string, PlaceRecord>;
  /** Child ids in store order, archived included, so a walk can pass through an archived place. */
  childrenOf: Map<string, string[]>;
  /** Chat ids filed directly in a place, each once, in membership order. */
  chatsOf: Map<string, string[]>;
};

function prepare(store: PlaceStore): Index {
  const byId = new Map<string, PlaceRecord>();
  for (const place of store.places) {
    // A repeated id is damage. The first row keeps the name the list was built with.
    if (place.id && !byId.has(place.id)) byId.set(place.id, place);
  }
  const childrenOf = new Map<string, string[]>();
  for (const place of byId.values()) {
    for (const parent of place.parents) {
      if (!parent || parent === place.id) continue;
      const list = childrenOf.get(parent) ?? [];
      list.push(place.id);
      childrenOf.set(parent, list);
    }
  }
  const chatsOf = new Map<string, string[]>();
  for (const membership of store.memberships ?? []) {
    if (!membership.chatId || !membership.placeId || !byId.has(membership.placeId)) continue;
    const list = chatsOf.get(membership.placeId) ?? [];
    if (!list.includes(membership.chatId)) list.push(membership.chatId);
    chatsOf.set(membership.placeId, list);
  }
  return { byId, childrenOf, chatsOf };
}

function positive(value: number | undefined): number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? value : 0;
}

const plural = (count: number, one: string, many = `${one}s`) => `${count} ${count === 1 ? one : many}`;

/**
 * The tint a place shows. Its own choice wins. Otherwise the first parent's, and that parent's own choice wins over
 * the grandparent, so an override is followed by the children under it. A top-level place that chose nothing is
 * graphite. Several parents never blend: only the first parent counts. An unknown id is nothing.
 */
export function effectiveTint(id: string, store: PlaceStore): Tint | undefined {
  const index = prepare(store);
  if (!index.byId.has(id)) return undefined;
  return tintOf(id, index);
}

function tintOf(id: string, index: Index): Tint {
  const seen = new Set<string>();
  let current: string | undefined = id;
  // Bounded by the places already seen, so a cycle of empty tints cannot spin.
  while (current && !seen.has(current)) {
    seen.add(current);
    const place = index.byId.get(current);
    if (!place) return 'graphite';
    if (place.tint) return place.tint;
    current = place.parents[0];
    if (!current) return 'graphite';
  }
  return 'graphite';
}

/**
 * The hue for a new top-level place: the least-used of the five among top-level places, ties broken by palette order.
 * Children do not count, archived places do not count, and graphite is never assigned. An empty store suggests tide,
 * the first unused colour, which is a choice rather than a count on screen.
 */
export function leastUsedTint(store: PlaceStore): Tint {
  const index = prepare(store);
  const use = new Map<Tint, number>(PICKABLE_TINTS.map(tint => [tint, 0]));
  for (const place of index.byId.values()) {
    if (place.archived || place.parents.length > 0) continue;
    const tint = tintOf(place.id, index);
    if (use.has(tint)) use.set(tint, (use.get(tint) ?? 0) + 1);
  }
  let best: Tint = PICKABLE_TINTS[0];
  for (const tint of PICKABLE_TINTS) {
    if ((use.get(tint) ?? 0) < (use.get(best) ?? 0)) best = tint;
  }
  return best;
}

/** The first parent's name, the muted word beside a rail row. Top-level, missing and nameless are nothing. */
export function primaryParentLabel(id: string, store: PlaceStore): string | undefined {
  const index = prepare(store);
  const parentId = index.byId.get(id)?.parents[0];
  const name = parentId ? index.byId.get(parentId)?.name : undefined;
  return name || undefined;
}

/** The first-parent chain, outermost first, without the place itself. A cycle cannot be walked twice. */
export function breadcrumb(id: string, store: PlaceStore): Crumb[] {
  const index = prepare(store);
  if (!index.byId.has(id)) return [];
  const chain: Crumb[] = [];
  const seen = new Set<string>([id]);
  let parentId = index.byId.get(id)?.parents[0];
  while (parentId && !seen.has(parentId)) {
    const parent = index.byId.get(parentId);
    if (!parent) break;
    seen.add(parentId);
    if (parent.name) chain.unshift({ id: parent.id, name: parent.name });
    parentId = parent.parents[0];
  }
  return chain;
}

function childrenOf(id: string, index: Index): PlaceRecord[] {
  const out: PlaceRecord[] = [];
  const seen = new Set<string>();
  for (const childId of index.childrenOf.get(id) ?? []) {
    if (seen.has(childId)) continue;
    seen.add(childId);
    const child = index.byId.get(childId);
    if (child && !child.archived) out.push(child);
  }
  return out;
}

/** Active places in the subtree, this place included. An archived place contributes no chats, but the walk passes through it. */
function subtree(id: string, index: Index): string[] {
  const seen = new Set<string>();
  const active: string[] = [];
  const stack = [id];
  while (stack.length) {
    const current = stack.pop();
    if (!current || seen.has(current)) continue;
    seen.add(current);
    const place = index.byId.get(current);
    if (place && !place.archived) active.push(current);
    for (const child of index.childrenOf.get(current) ?? []) stack.push(child);
  }
  return active;
}

/** Chats in this place and under it, each chat once. A chat filed in two children counts once for the parent. */
function chatsUnder(id: string, index: Index): string[] {
  const seen = new Set<string>();
  const chats: string[] = [];
  for (const placeId of subtree(id, index)) {
    for (const chatId of index.chatsOf.get(placeId) ?? []) {
      if (seen.has(chatId)) continue;
      seen.add(chatId);
      chats.push(chatId);
    }
  }
  return chats;
}

/**
 * The count a tree row or a tile draws. `tree` is "4 inside" or, for a leaf, "28 chats". `tile` is "28 chats",
 * "6 chats · also in Software" or "47 places · 212 chats". "Inside" and "places" are the places directly in it
 * (a grandchild is counted on its own parent). Chats are this place and everything under it, each chat once.
 * Zero of both is nothing.
 */
export function countsText(id: string, store: PlaceStore, surface: 'tree' | 'tile'): string | undefined {
  const index = prepare(store);
  const place = index.byId.get(id);
  if (!place || place.archived) return undefined;
  const places = childrenOf(id, index).length;
  const chats = chatsUnder(id, index).length;
  if (surface === 'tree') {
    if (places > 0) return `${places} inside`;
    if (chats > 0) return plural(chats, 'chat');
    return undefined;
  }
  const parts: string[] = [];
  if (places > 0) parts.push(plural(places, 'place'));
  if (chats > 0) parts.push(plural(chats, 'chat'));
  const extra: string[] = [];
  for (const parentId of place.parents.slice(1)) {
    const parent = index.byId.get(parentId);
    if (parent && !parent.archived && parent.name) extra.push(parent.name);
  }
  if (extra.length) parts.push(`also in ${extra.join(', ')}`);
  return parts.length ? parts.join(' · ') : undefined;
}

function originName(place: PlaceRecord, origins: readonly string[], index: Index): string {
  if (origins.length === 1) {
    const named = index.byId.get(origins[0])?.name;
    if (named) return named;
  }
  return place.name;
}

/**
 * The hover on a status dot. Amber wins over red. Running alone says nothing, so the rail draws no dot for it.
 * One source is named ("2 need you in Config parser"); two or more name the place the dot sits on.
 */
export function rollupWords(id: string, store: PlaceStore, activity: Readonly<Record<string, ChatActivity>> = {}): RollupWords | undefined {
  const index = prepare(store);
  const place = index.byId.get(id);
  if (!place || place.archived) return undefined;
  return wordsOf(place, index, activity);
}

function wordsOf(place: PlaceRecord, index: Index, activity: Readonly<Record<string, ChatActivity>>): RollupWords | undefined {
  // The number counts a chat once. The origin list still names every place that holds it, so two parents are not collapsed into one.
  const counted = new Set<string>();
  let needsYou = 0;
  let failed = 0;
  const needOrigins: string[] = [];
  const failedOrigins: string[] = [];
  for (const placeId of subtree(place.id, index)) {
    let placeNeeds = 0;
    let placeFailed = 0;
    for (const chatId of index.chatsOf.get(placeId) ?? []) {
      const state = activity[chatId] ?? {};
      const waiting = positive(state.needsYou);
      const didFail = state.failed === true;
      if (!counted.has(chatId)) {
        counted.add(chatId);
        needsYou += waiting;
        if (didFail) failed += 1;
      }
      if (waiting > 0) placeNeeds += waiting;
      if (didFail) placeFailed += 1;
    }
    if (placeNeeds > 0) needOrigins.push(placeId);
    if (placeFailed > 0) failedOrigins.push(placeId);
  }
  if (needsYou > 0) {
    const verb = needsYou === 1 ? 'needs' : 'need';
    return { status: 'waiting', words: `${needsYou} ${verb} you in ${originName(place, needOrigins, index)}` };
  }
  if (failed > 0) return { status: 'failed', words: `${plural(failed, 'failed task')} in ${originName(place, failedOrigins, index)}` };
  return undefined;
}

function isBusy(id: string, index: Index, activity: Readonly<Record<string, ChatActivity>>): boolean {
  for (const chatId of chatsUnder(id, index)) {
    const state = activity[chatId] ?? {};
    if (state.running || positive(state.needsYou) > 0) return true;
  }
  return false;
}

function stamp(iso: string): number {
  const value = Date.parse(iso);
  return Number.isNaN(value) ? Number.NEGATIVE_INFINITY : value;
}

/** One row per place: the newest visit wins, and the list comes back newest first. */
function latestVisits(visits: readonly WindowVisit[]): WindowVisit[] {
  const best = new Map<string, { visit: WindowVisit; index: number }>();
  visits.forEach((visit, index) => {
    if (!visit.placeId) return;
    const previous = best.get(visit.placeId);
    const at = stamp(visit.touchedAt);
    if (!previous || at > stamp(previous.visit.touchedAt) || (at === stamp(previous.visit.touchedAt) && index >= previous.index)) {
      best.set(visit.placeId, { visit, index });
    }
  });
  return [...best.values()]
    .sort((a, b) => stamp(b.visit.touchedAt) - stamp(a.visit.touchedAt) || b.index - a.index)
    .map(entry => entry.visit);
}

function idleSince(touchedAt: string, now: string): boolean {
  const at = Date.parse(touchedAt);
  const clock = Date.parse(now);
  if (Number.isNaN(at) || Number.isNaN(clock)) return false;
  return clock - at >= OPEN_IDLE_MS;
}

/** ⌃1–9 in rail order: every pinned place, then Open. A duplicate is not numbered twice, and the tenth place is absent. */
export function placeNumbers(pinnedIds: readonly string[], openIds: readonly string[]): ReadonlyMap<string, number> {
  const numbers = new Map<string, number>();
  for (const id of [...pinnedIds, ...openIds]) {
    if (!id || numbers.has(id)) continue;
    if (numbers.size >= PLACE_NUMBER_MAX) break;
    numbers.set(id, numbers.size + 1);
  }
  return numbers;
}

/**
 * Pinned is the person's order, and it never auto-closes. Open is this window's visits, newest first. A place the
 * person closed leaves unless something under it is still running or waiting; then it stays, muted, until that
 * settles. An Open place untouched for twelve hours leaves too, unless that work is still going. A pinned parent
 * does not add its children.
 */
export function railSections(store: PlaceStore, window: WindowOpen, activity: Readonly<Record<string, ChatActivity>> = {}): RailSections {
  const index = prepare(store);
  const pinnedIds: string[] = [];
  const seen = new Set<string>();
  for (const id of store.pinned ?? []) {
    const place = index.byId.get(id);
    if (!place || place.archived || seen.has(id)) continue;
    seen.add(id);
    pinnedIds.push(id);
  }
  const openIds: string[] = [];
  const closedBusy = new Set<string>();
  for (const visit of latestVisits(window.visits)) {
    if (seen.has(visit.placeId)) continue;
    const place = index.byId.get(visit.placeId);
    if (!place || place.archived) continue;
    const busy = isBusy(visit.placeId, index, activity);
    if (visit.closed) {
      if (!busy) continue;
      closedBusy.add(visit.placeId);
    } else if (idleSince(visit.touchedAt, window.now) && !busy) continue;
    seen.add(visit.placeId);
    openIds.push(visit.placeId);
  }
  const numbers = placeNumbers(pinnedIds, openIds);
  const build = (id: string): RailRow => {
    const place = index.byId.get(id)!;
    const words = wordsOf(place, index, activity);
    const parent = primaryParentLabel(id, store);
    const number = numbers.get(id);
    const row: RailRow = { id: place.id, name: place.name, tint: tintOf(id, index) };
    if (parent) row.parentName = parent;
    if (words) {
      row.status = words.status;
      row.statusLabel = words.words;
    }
    if (closedBusy.has(id)) {
      row.closedButRunning = true;
      row.closedLabel = CLOSED_STILL_RUNNING;
    }
    if (number) row.number = number;
    return row;
  };
  return { pinned: pinnedIds.map(build), open: openIds.map(build) };
}
