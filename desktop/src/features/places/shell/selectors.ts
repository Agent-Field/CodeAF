// Pure selectors from the engine's Places answers (client.ts) to what the shell draws. React-free and clock-free so
// node tests can pin every rule. Nothing here invents a status, a count or a sentence the engine did not send: a
// number that is zero or unknown is left out (the emptiness law), and a recap the engine did not write is absent.

import type { Attention, ChatRow, HomeDigest, PlaceView, PlacesGraph, StatusRollup } from '../client.ts';
import type { HomeChat, HomeChild, HomeSource, HomeView } from '../home-model.ts';
import type { PlaceRowModel } from './contracts.ts';

const plural = (count: number, one: string, many = `${one}s`) => `${count} ${count === 1 ? one : many}`;

/** Amber when something needs the person, red when a task failed; running never draws a dot (Places 10a). */
export function rollupStatus(rollup: StatusRollup | undefined): 'waiting' | 'failed' | undefined {
  if (!rollup) return undefined;
  if (rollup.needsYou > 0) return 'waiting';
  if (rollup.failedTasks > 0) return 'failed';
  return undefined;
}

export type PlaceIndex = { byId: ReadonlyMap<string, PlaceView>; childrenOf: ReadonlyMap<string, readonly string[]> };

/** Every place by id, and children by parent id (`root` for the top level), each list in the graph's order. */
export function indexPlaces(places: readonly PlaceView[]): PlaceIndex {
  const byId = new Map(places.map(place => [place.id, place] as const));
  const childrenOf = new Map<string, string[]>();
  for (const place of places) {
    const parents = place.parents.filter(parent => byId.has(parent));
    for (const parent of parents.length ? parents : ['root']) {
      const list = childrenOf.get(parent) ?? [];
      list.push(place.id);
      childrenOf.set(parent, list);
    }
  }
  return { byId, childrenOf };
}

/** The first-parent chain, outermost first, without the place itself. A cycle the store refused can never be walked twice. */
export function ancestry(id: string, byId: ReadonlyMap<string, PlaceView>): PlaceView[] {
  const chain: PlaceView[] = [];
  const seen = new Set([id]);
  let parent = byId.get(id)?.parents[0];
  while (parent && !seen.has(parent)) {
    const place = byId.get(parent);
    if (!place) break;
    chain.unshift(place);
    seen.add(parent);
    parent = place.parents[0];
  }
  return chain;
}

/** The words a status dot says, to a tooltip and a screen reader. A parent's dot names the child it comes from when exactly one does. */
export function statusWords(place: PlaceView, index: PlaceIndex): string | undefined {
  const rollup = place.statusInclusive;
  const status = rollupStatus(rollup);
  if (!status) return undefined;
  const own = status === 'waiting' ? place.status.needsYou > 0 : place.status.failedTasks > 0;
  let where = place.name;
  if (!own) {
    const sources = [...index.byId.values()].filter(other => other.id !== place.id && (status === 'waiting' ? other.status.needsYou > 0 : other.status.failedTasks > 0)
      && ancestry(other.id, index.byId).some(ancestor => ancestor.id === place.id));
    if (sources.length === 1) where = sources[0].name;
  }
  return status === 'waiting'
    ? `${rollup.needsYou} ${rollup.needsYou === 1 ? 'needs' : 'need'} you in ${where}`
    : `${plural(rollup.failedTasks, 'failed task')} in ${where}`;
}

/** "4 inside" for a place with children, otherwise "12 chats"; nothing when both are zero. */
export function placeMeta(place: PlaceView): string | undefined {
  if (place.counts.descendants > 0) return `${place.counts.descendants} inside`;
  if (place.counts.chatsInclusive > 0) return plural(place.counts.chatsInclusive, 'chat');
  return undefined;
}

export function placeRow(place: PlaceView, index: PlaceIndex, closedButBusy = false): PlaceRowModel {
  const parent = place.parents[0] ? index.byId.get(place.parents[0]) : undefined;
  return {
    id: place.id, name: place.name, tint: place.effectiveTint, parentName: parent?.name, parents: place.parents,
    status: rollupStatus(place.statusInclusive), statusLabel: statusWords(place, index),
    closedButBusy: closedButBusy || undefined, pinned: place.pinned, archived: place.archived,
    lastOpenedAt: place.lastOpenedAt, meta: placeMeta(place),
  };
}

/** True while a place (or anything under it) is running or waiting: a closed place with this stays on the rail, muted. */
export const isBusy = (place: PlaceView) => place.statusInclusive.running > 0 || place.statusInclusive.needsYou > 0;

export type RailSections = { pinned: PlaceRowModel[]; open: PlaceRowModel[] };

/**
 * The rail's two sections (Places 10a). Pinned is the engine's order. Open is the engine's list (visited in the last
 * 12h, or with work inside, newest first) minus the places the person closed in this window since they last went
 * there; a closed place that is still running or waiting stays, muted, until the work settles. The window's own place
 * is always in Open while it is not pinned, because going to a place is what puts it there.
 */
export function railSections(graph: PlacesGraph, closed: ReadonlyMap<string, string>, current?: string): RailSections {
  const index = indexPlaces(graph.places);
  const pinned = graph.rail.pinned.map(place => placeRow(place, index));
  const seen = new Set(pinned.map(place => place.id));
  const open: PlaceRowModel[] = [];
  const consider = (place: PlaceView | undefined) => {
    if (!place || place.archived || seen.has(place.id)) return;
    const closedAt = closed.get(place.id);
    const reopened = closedAt !== undefined && !!place.lastOpenedAt && place.lastOpenedAt > closedAt;
    const isClosed = closedAt !== undefined && !reopened && place.id !== current;
    if (isClosed && !isBusy(place)) return;
    seen.add(place.id);
    open.push(placeRow(place, index, isClosed));
  };
  if (current && current.startsWith('pl_')) consider(index.byId.get(current));
  for (const place of graph.rail.open) consider(index.byId.get(place.id) ?? place);
  return { pinned, open };
}

/** The ⌃1–9 order: pinned first, then open, in rail order (DESIGN-QUESTIONS Q-P3's shipped assumption). */
export const railOrder = (sections: RailSections) => [...sections.pinned, ...sections.open].map(place => place.id);

// ---- Home ------------------------------------------------------------------------------------------------------------

function child(place: PlaceView, path?: readonly string[]): HomeChild {
  return {
    id: place.id, name: place.name, tint: place.effectiveTint, tintSource: place.tint ? 'own' : 'inherited',
    places: place.counts.descendants, chats: place.counts.chatsInclusive,
    alsoIn: place.alsoIn.length ? place.alsoIn.map(ref => ref.name) : undefined,
    status: rollupStatus(place.statusInclusive), pinned: place.pinned, decide: place.decide, archived: place.archived, path,
  };
}

function chat(row: ChatRow, digest: HomeDigest): HomeChat {
  // Only the live presence says a chat is running or waiting; an all-time failure count is not "failed now".
  const status = row.needsYou ? 'waiting' : row.live && (row.tasks.running > 0 || row.doing === 'working') ? 'running' : undefined;
  // A waiting reason takes priority; otherwise the engine’s recap supplies the digest without a renderer model call.
  const line = digest.recap?.items.find(item => item.chatId === row.id)?.line;
  return { id: row.id, title: row.title, excerpt: row.needsYou && row.reason ? row.reason : line || undefined, status, at: row.at, model: row.model };
}

function attention(item: Attention, viewing: string): HomeView['attention'][number] {
  return {
    id: item.chatId, title: item.chatTitle || 'Untitled chat',
    placeName: item.placeId && item.placeId !== viewing ? item.placeName : undefined,
    status: item.kind === 'needsYou' ? 'waiting' : 'running', detail: item.text && item.text !== item.chatTitle ? item.text : undefined,
  };
}

/** A context line is the engine's words only; this one is assembled from the place's real sources and nothing else. */
function sources(digest: HomeDigest): HomeSource[] | undefined {
  const list = digest.place?.sources ?? [];
  if (!list.length) return undefined;
  return list.map(source => ({ id: source.id, kind: source.kind, label: source.label || source.check.title || source.ref, state: source.check.state }));
}

/** The engine's "Since …" words, or nothing: a blank label or text draws no block (PL-133, no model call here). */
function sinceOf(digest: HomeDigest): HomeView['recap'] {
  const label = digest.recap?.label.trim(), text = digest.recap?.text.trim();
  return label && text ? { label, text } : undefined;
}

/**
 * The Home view model (home-model.ts) from the engine's digest. For the root, `graph` (with archived places) supplies
 * the count line, the search pool with each place's path, and the archived toggle; the digest's own children stay
 * the top level, and `unplaced` (Now's digest) supplies "Not in any place". Nothing is written that the engine did not send: no recap, no context line, no suggestion.
 */
export function homeViewFromDigest(digest: HomeDigest, graph?: PlacesGraph, unplaced?: HomeDigest): HomeView {
  const id = digest.kind === 'place' ? digest.place!.id : digest.kind;
  const view: HomeView = {
    kind: digest.kind, id, title: digest.title,
    tint: digest.place?.effectiveTint ?? 'graphite',
    tintSource: digest.place ? (digest.place.tint ? 'own' : 'inherited') : undefined,
    breadcrumb: digest.breadcrumb.map(crumb => ({ id: crumb.id, name: crumb.name })),
    attention: digest.attention.map(item => attention(item, id)),
    children: digest.children.map(place => child(place)),
    chats: digest.chats.map(row => chat(row, digest)),
    chatsTruncated: digest.chatsTruncated,
    sources: digest.kind === 'place' ? sources(digest) : undefined,
    pinned: digest.place?.pinned,
    decide: digest.place?.decide,
    recap: sinceOf(digest),
  };
  if (digest.kind === 'root' && graph) {
    const index = indexPlaces(graph.places);
    const active = graph.places.filter(place => !place.archived);
    view.totals = { topLevel: digest.children.length, all: active.length };
    view.allPlaces = active.map(place => child(place, ancestry(place.id, index.byId).map(ancestor => ancestor.name)));
    view.archivedChildren = graph.places.filter(place => place.archived).map(place => child(place));
    view.unplaced = { total: graph.totals.unplaced };
  }
  // The root's own chats are every placed chat; the page lists the ones in NO place, which is Now's digest.
  if (digest.kind === 'root') { view.chats = unplaced ? unplaced.chats.map(row => chat(row, unplaced)) : []; view.chatsTruncated = unplaced?.chatsTruncated; }
  return view;
}

/** The session file a Home row opens, when the engine said it. */
export function sessionFileOf(digests: readonly (HomeDigest | undefined)[], chatId: string): string | undefined {
  for (const digest of digests) {
    const file = digest?.chats.find(row => row.id === chatId)?.sessionFile;
    if (file) return file;
  }
  return undefined;
}
