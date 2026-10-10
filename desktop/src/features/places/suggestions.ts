// The place suggestions a person can act on (Places 6d, 6e, 8c, Components "Suggested place",
// Interactions "Suggestion (place / filing)"). Two different sources, and they do not mix:
//
//   - An engine proposal (move chats into a place, or create one) is drawn only when the
//     engine sent it. This file never groups chats and never calls a model.
//   - A place left untouched is a date rule. The figures are the design's own (60 days,
//     30 days of Not now) and live in tokens so the sentence and the snooze cannot drift.
//     The clock is injected: a test pins the boundary to the millisecond, and a render
//     does not read the wall clock itself.
//
// Not now is not a change to the place graph. It is a record in this origin's storage,
// so a reload keeps it, and it ends on its boundary: hidden while now is before `until`,
// offered again when now reaches `until`. A damaged record reads as no snoozes, which
// at worst repeats a suggestion.

import tokens from '../../design/tokens.json' with { type: 'json' };

const DAY_MS = 86_400_000;
const rules = tokens.interaction;

/** Design 6d: untouched for 60 days, counted in 24-hour blocks, inclusive on the boundary. */
export const idleAfterDays = rules.placeIdleDays;
/** Interactions: Not now hides a suggestion for 30 days, and it returns on that boundary. */
export const snoozeDays = rules.placeSnoozeDays;
/** How many idle places one answer keeps. The screens draw the first; the rest wait behind Not now. */
export const idleListCap = rules.placeIdleListCap;
/** Places 6e: a new place is suggested only when at least this many chats cluster. */
export const clusterMinimum = rules.placeClusterMinimum;

/** One "Not now". `until` is the instant it stops hiding the suggestion. */
export type Snooze = { id: string; at: number; until: number };

/** A place the idle rule can judge. Absent dates stay unknown; they are never guessed. */
export type IdlePlace = {
  id: string;
  name: string;
  archived?: boolean;
  pinned?: boolean;
  /** Work running, or a chat waiting, in this place or under it. The caller rolls that up. */
  busy?: boolean;
  parents?: readonly string[];
  createdAt?: string;
  openedAt?: string;
  /** Newest time someone spoke in a chat filed here. The engine's rolled `touchedAt` may be passed as this. */
  activityAt?: string;
};

export type IdleOffer = { id: string; name: string; touchedAt: number; days: number };

/** What the engine already decided to offer. A missing name or count is not filled in here. */
export type SuppliedSuggestion = {
  id: string;
  kind: 'move' | 'create' | 'file' | 'merge';
  name?: string;
  placeName?: string;
  reason?: string;
  chatCount?: number;
};

export type SuggestionView =
  | { layout: 'line'; id: string; label: string; text: string; actions: { id: string; label: string }[] }
  | { layout: 'card'; id: string; label: string; kicker: string; title: string; detail: string; actions: { id: string; label: string }[] };

const snoozeKey = 'codeaf.desktop.placeSuggestions.v1';
/** A hostile or corrupt record is dropped whole. Past this, the file is not a person's snoozes. */
const maxSnoozeRecords = 200;

export type SnoozeStore = { getItem(key: string): string | null; setItem(key: string, value: string): void };

/** Go's zero time and anything that does not parse are unknown, not old. */
function instant(value?: string): number | undefined {
  if (!value || value.startsWith('0001-01-01')) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function ownTouched(place: IdlePlace): number | undefined {
  const times = [place.createdAt, place.openedAt, place.activityAt].map(instant).filter((time): time is number => time !== undefined);
  return times.length ? Math.max(...times) : undefined;
}

function childrenOf(places: readonly IdlePlace[]): Map<string, string[]> {
  const children = new Map<string, string[]>();
  for (const place of places) {
    for (const parent of place.parents ?? []) {
      const list = children.get(parent);
      if (list) list.push(place.id);
      else children.set(parent, [place.id]);
    }
  }
  return children;
}

/**
 * The newest sign of life in this place or under it. An archived place contributes
 * none of its own dates, but a live grandchild below it still does: archiving the
 * middle place does not erase the child. Cycles stop at the first repeat.
 */
function rolledTouched(id: string, byId: ReadonlyMap<string, IdlePlace>, children: ReadonlyMap<string, readonly string[]>, seen: Set<string>): number | undefined {
  if (seen.has(id)) return undefined;
  seen.add(id);
  const place = byId.get(id);
  if (!place) return undefined;
  let best = place.archived ? undefined : ownTouched(place);
  for (const child of children.get(id) ?? []) {
    const next = rolledTouched(child, byId, children, seen);
    if (next !== undefined && (best === undefined || next > best)) best = next;
  }
  return best;
}

function hidden(snoozes: readonly Snooze[], id: string, now: number): boolean {
  return snoozes.some(snooze => snooze.id === id && now < snooze.until);
}

/** Places to offer for merging or archiving, longest untouched first. An unusable clock offers nothing. */
export function idlePlaces(places: readonly IdlePlace[], now: Date, snoozes: readonly Snooze[] = []): IdleOffer[] {
  const at = now.getTime();
  if (!Number.isFinite(at)) return [];
  const byId = new Map<string, IdlePlace>();
  for (const place of places) if (place.id && !byId.has(place.id)) byId.set(place.id, place);
  const children = childrenOf(places);
  const limit = idleAfterDays * DAY_MS;
  const due: IdleOffer[] = [];
  for (const place of byId.values()) {
    if (!place.name.trim() || place.archived || place.pinned || place.busy || hidden(snoozes, place.id, at)) continue;
    const touched = rolledTouched(place.id, byId, children, new Set());
    if (touched === undefined || at - touched < limit) continue;
    due.push({ id: place.id, name: place.name, touchedAt: touched, days: Math.floor((at - touched) / DAY_MS) });
  }
  due.sort((a, b) => a.touchedAt - b.touchedAt || a.name.localeCompare(b.name) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  return due.slice(0, idleListCap);
}

/** “Launch week” hasn’t been touched in 75 days. The count is whole days from the injected clock. */
export function idleText(place: Pick<IdleOffer, 'name' | 'days'>): string {
  return `“${place.name}” hasn’t been touched in ${place.days} ${place.days === 1 ? 'day' : 'days'}`;
}

/**
 * The one idle line for a candidate the engine already named. `touchedAt` is that
 * candidate's newest sign of life, so chat speech the desktop graph does not carry
 * still counts. A candidate this clock would not call idle is left out.
 */
export function idleLine(place: { id: string; name: string; touchedAt: string }, now: Date, snoozes: readonly Snooze[] = []): { placeId: string; text: string; days: number } | undefined {
  const [due] = idlePlaces([{ id: place.id, name: place.name, activityAt: place.touchedAt }], now, snoozes);
  return due ? { placeId: due.id, text: idleText(due), days: due.days } : undefined;
}

/** Not now for `id`, ending `snoozeDays` after `now`. Expired rows are dropped so the record cannot grow without bound. */
export function withSnooze(records: readonly Snooze[], id: string, now: Date): Snooze[] {
  const at = now.getTime();
  if (!id || !Number.isFinite(at)) return records.filter(snooze => Number.isFinite(snooze.until));
  const until = at + snoozeDays * DAY_MS;
  const kept = records.filter(snooze => snooze.id !== id && at < snooze.until);
  return [...kept, { id, at, until }];
}

function snoozeRow(value: unknown): Snooze | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const row = value as { id?: unknown; at?: unknown; until?: unknown };
  if (typeof row.id !== 'string' || !row.id || typeof row.at !== 'number' || typeof row.until !== 'number') return undefined;
  if (!Number.isFinite(row.at) || !Number.isFinite(row.until) || row.until < row.at) return undefined;
  return { id: row.id, at: row.at, until: row.until };
}

/** Snoozes still hiding something at `now`. A damaged record is no snoozes: the suggestion may repeat, and nothing else changes. */
export function readSnoozes(store: SnoozeStore | undefined, now: Date): Snooze[] {
  if (!store || !Number.isFinite(now.getTime())) return [];
  try {
    const parsed = JSON.parse(store.getItem(snoozeKey) ?? 'null') as { version?: unknown; snoozes?: unknown } | null;
    if (!parsed || parsed.version !== 1 || !Array.isArray(parsed.snoozes) || parsed.snoozes.length > maxSnoozeRecords) return [];
    const rows: Snooze[] = [];
    for (const value of parsed.snoozes) {
      const row = snoozeRow(value);
      if (!row || rows.some(kept => kept.id === row.id)) return [];
      rows.push(row);
    }
    return rows.filter(snooze => now.getTime() < snooze.until);
  } catch {
    return [];
  }
}

/** Writes one Not now and returns the rows still in force. A store that refuses the write still returns the row, so this call can hide the card. */
export function writeSnooze(store: SnoozeStore | undefined, id: string, now: Date): Snooze[] {
  const next = withSnooze(readSnoozes(store, now), id, now);
  try { store?.setItem(snoozeKey, JSON.stringify({ version: 1, snoozes: next })); } catch { /* the caller still hides it for this turn */ }
  return next;
}

/** The origin's snooze record, or none where storage is missing or blocked. */
export function browserSnoozes(now: Date): Snooze[] {
  try {
    return readSnoozes(typeof localStorage === 'undefined' ? undefined : localStorage, now);
  } catch {
    return [];
  }
}

function chats(count: number | undefined): string {
  if (!count || count < 1) return '';
  return `${count} ${count === 1 ? 'chat' : 'chats'}`;
}

/**
 * One engine proposal, or nothing. A create below the cluster minimum, a create
 * with no name, and a snoozed id are absent: the screen draws nothing for them.
 */
export function viewForSupplied(offer: SuppliedSuggestion, now: Date, snoozes: readonly Snooze[] = []): SuggestionView | undefined {
  if (!offer.id || !Number.isFinite(now.getTime()) || hidden(snoozes, offer.id, now.getTime())) return undefined;
  if (offer.kind === 'create') {
    const count = offer.chatCount ?? 0;
    const title = offer.name?.trim();
    if (count < clusterMinimum || !title) return undefined;
    const detail = offer.reason?.trim() || chats(count);
    if (!detail) return undefined;
    return {
      layout: 'card', id: offer.id, label: 'Suggested place', kicker: 'Suggested place', title, detail,
      actions: [{ id: 'create', label: 'Create' }, { id: 'not-now', label: 'Not now' }],
    };
  }
  if (offer.kind === 'move' || offer.kind === 'file') {
    const place = offer.placeName?.trim();
    const count = offer.chatCount ?? 0;
    if (!place || count < 1) return undefined;
    const text = offer.kind === 'file'
      ? (offer.reason?.trim() || '')
      : `${count} of these look like they belong in ${place}`;
    if (!text) return undefined;
    const accept = offer.kind === 'file' ? `Add to ${place}` : 'Move them';
    return {
      layout: 'line', id: offer.id, label: 'Place suggestion', text,
      actions: [{ id: 'accept', label: accept }, { id: 'not-now', label: 'Not now' }],
    };
  }
  return undefined;
}

/** Engine proposals in the order they arrived, minus the ones this clock is still hiding. */
export function suppliedSuggestions(offers: readonly SuppliedSuggestion[], now: Date, snoozes: readonly Snooze[] = []): SuggestionView[] {
  return offers.flatMap(offer => {
    const view = viewForSupplied(offer, now, snoozes);
    return view ? [view] : [];
  });
}
