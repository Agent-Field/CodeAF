// SPECIMEN FIXTURE — test data only, never shown in the live app.
//
// A stateful, in-memory stand-in for the engine's Places routes (internal/desktopbridge/places*.go) and the
// engine-wide world feed (GET /world, GET /events?after=N). Its answers follow the shapes of the Go bridge's real
// answers in src/features/places/fixtures/*.json and pass every validator in src/features/places/client.ts.
//
// Install it AFTER installMockEngine: Playwright gives the last registered route precedence, and this one answers only
// /api/engine/(places|chats|world|events) and hands every other request back (route.fallback) to the conversation mock.
//
// The world stream: route.fulfill can only send one finite body, so /events behaves like the conversation mock's
// session stream. A request from behind the current world sequence gets one `reset` record and the body ends; the
// world store then reconnects from that sequence (its own one-second backoff), and a request that is already current is
// held open until something changes (`setStatus`, `nudge`) or the page closes. A change therefore arrives as one
// `world` record, which is what makes the places store read the graph again, exactly as the live window does.

import type { Page, Route } from '@playwright/test';
import type { PlaceProposal } from '../../../src/features/places/proposals-client';
import type {
  AddedBy, ChatRow, Crumb, HomeDigest, Mutation, PlaceCounts, PlaceDetail, PlaceView, PlacesGraph, Receipt, SourceKind, SourceView, StatusRollup, Tint,
} from '../../../src/features/places/client';

export type SeedPlace = {
  /** Fixed id; otherwise one is made (pl_ and 16 hex digits). */
  id?: string;
  name: string;
  /** Parent names or ids, first parent first. */
  parents?: string[];
  /** The place's own tint; '' (or absent below the top level) inherits. At the top level absent means the store picks. */
  tint?: Tint | '';
  pinned?: boolean;
  archived?: boolean;
  /** 'now' stamps the current instant, so the place is in the rail's Open list. */
  lastOpenedAt?: string | 'now';
  /** Created and last opened this many days ago, so the untouched-place rule (60 days) can be met or missed exactly. */
  untouchedDays?: number;
  instructions?: string;
  /** The place's own default model (Policy.Model). Fixture ids only: no test here reaches a real model. */
  model?: string;
  sources?: { kind: SourceKind; ref: string; label?: string }[];
};

export type SeedChat = {
  id: string;
  title?: string;
  /** Place names or ids it is filed in. */
  places?: string[];
  live?: boolean;
  doing?: string;
  needsYou?: boolean;
  reason?: string;
  tasks?: Partial<{ running: number; incomplete: number; done: number; failed: number }>;
  /** ISO instant of the last word; defaults to minutes ago, newest first in seed order. */
  at?: string;
};

export type PlacesSeed = {
  /** A fixed engine clock keeps fixture timestamps and idle rules reproducible. */
  now?: string;
  /** Closed places remain visible only while their work needs attention. */
  closed?: string[];
  proposals?: PlaceProposal[];
  places?: SeedPlace[];
  chats?: SeedChat[];
  /** Chat ids a live bridge conversation holds though the world has not saved them yet: filing them is allowed. */
  live?: string[];
  /** Absolute paths that "exist" for folder/file/repo sources. Anything else is refused as the bridge refuses it. */
  disk?: string[];
};

/** A forced answer for one route: `abort` drops the connection (the engine is unreachable). */
export type Forced = 'abort' | { status: number; error: string; code?: string };
export type RouteKey =
  | 'graph' | 'status' | 'rail' | 'home' | 'delete-preview' | 'effective-model' | 'chat-places' | 'create' | 'update' | 'parents' | 'archive' | 'restore' | 'delete'
  | 'merge' | 'pin' | 'unpin' | 'sources' | 'sources/remove' | 'members' | 'members/remove' | 'visit' | 'undo' | 'world' | 'events' | 'stale' | 'stale-snooze';

export type PlacesCall = { method: string; path: string; body: Record<string, unknown> };

type Place = {
  id: string; name: string; parents: string[]; tint: Tint | ''; archived: boolean; createdAt: string; lastOpenedAt?: string; archivedAt?: string;
  instructions: string; sources: SourceView[]; policy: { model?: string; permissions?: string };
};
type Chat = {
  id: string; title: string; sessionFile: string; at: string; live: boolean; doing: string; needsYou: boolean; reason?: string; archived: boolean;
  tasks: { running: number; incomplete: number; done: number; failed: number };
};
type Member = { chatId: string; placeId: string; addedBy: AddedBy; at: string };
type Store = { places: Place[]; pins: string[]; members: Member[]; chats: Chat[] };

const TINTS: Tint[] = ['tide', 'iris', 'rose', 'sand', 'sage'];
const ZERO: StatusRollup = { chats: 0, running: 0, needsYou: 0, incomplete: 0, failedTasks: 0 };
const json = (route: Route, value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
const hex = (n: number) => n.toString(16).padStart(16, '0');
/** Where a chat's journal lives: the folder holding it is the chat id (chatIdFromSessionFile). */
export const sessionFileFor = (chatId: string) => `/home/u/.codeaf/v3/projects/p/${chatId}/session.jsonl`;

class Refusal extends Error {
  constructor(readonly status: number, readonly code: string, message: string, readonly extra: Record<string, unknown> = {}) { super(message); }
}

export type MockPlaces = {
  /** Every Places/world request this mock answered, in order. */
  calls: PlacesCall[];
  /** Every /api/engine request the page made (both mocks), in the order the browser issued them. */
  traffic: PlacesCall[];
  /** A copy of the store: places, pins, memberships, chats, and the revision. */
  state: () => Store & { revision: number };
  /** The id of a place by name (the first one with that name). */
  id: (name: string) => string;
  /** Overrides a place's DIRECT roll-up (its inclusive one follows) and nudges the world stream so the window reads again. */
  setStatus: (placeId: string, rollup: Partial<StatusRollup>) => void;
  /** Updates real fixture work, so closed rows can leave when their last run ends. */
  setChat: (chatId: string, patch: Partial<SeedChat>) => void;
  /** Forces an answer on one route until cleared with `undefined`. */
  fail: (key: RouteKey, forced: Forced | undefined) => void;
  /** Moves the world sequence, so the window reads the graph again. */
  nudge: () => void;
  /** A delayed ledger answer, without a world event, as actual advice finishes asynchronously. */
  propose: (next: PlaceProposal[]) => void;
  /** POSTs whose path ends with the suffix. */
  posts: (suffix: string) => PlacesCall[];
  /** The "Not now" snoozes the engine holds: place id to the ISO instant each ends. Survives a page reload, as the engine's file does. */
  snoozes: () => Record<string, string>;
};

/** `alsoServe` are more windows on the same engine: they answer from this same state, as two app windows share one engine. */
export async function installMockPlaces(page: Page, seed: PlacesSeed = {}, alsoServe: Page[] = []): Promise<MockPlaces> {
  let counter = 0;
  const nextId = (prefix: string) => `${prefix}_${hex(++counter)}`;
  const clock = () => seed.now === undefined ? Date.now() : Date.parse(seed.now);
  const now = () => new Date(clock()).toISOString();
  const DAY = 86_400_000;
  const daysAgo = (days: number) => new Date(clock() - days * DAY).toISOString();
  /** Place id to the instant its "Not now" ends; lives in this mock, so it outlasts a page reload like the engine's snooze file. */
  const snoozed = new Map<string, string>();
  let revision = 0;
  const offers = structuredClone(seed.proposals ?? []);
  const offersView = () => ({ proposals: offers, organizing: false });
  let store: Store = { places: [], pins: [], members: [], chats: [] };
  const overrides = new Map<string, Partial<StatusRollup>>();
  const closedPlaces = new Set(seed.closed ?? []);
  const live = new Set(seed.live ?? []);
  const disk = new Set(seed.disk ?? []);
  const forced = new Map<RouteKey, Forced>();
  const calls: PlacesCall[] = [];
  const traffic: PlacesCall[] = [];
  /** Receipts the store still holds, newest last; an undo may only take back the newest live one (the bridge's rule). */
  const history: { receipt: Receipt; before: Store }[] = [];
  let worldSeq = 1;
  let closed = false;
  page.on('close', () => { closed = true; });

  // ---- seed ---------------------------------------------------------------------------------------------------------
  const byName = (ref: string) => store.places.find(place => place.id === ref || place.name === ref);
  for (const s of seed.places ?? []) {
    const id = s.id ?? nextId('pl');
    store.places.push({
      id, name: s.name, parents: (s.parents ?? []).map(ref => byName(ref)?.id ?? ref), tint: s.tint ?? '', archived: !!s.archived,
      createdAt: s.untouchedDays !== undefined ? daysAgo(s.untouchedDays) : '2026-10-01T09:00:00Z',
      lastOpenedAt: s.untouchedDays !== undefined ? daysAgo(s.untouchedDays) : s.lastOpenedAt === 'now' ? now() : s.lastOpenedAt, archivedAt: s.archived ? '2026-10-05T09:00:00Z' : undefined,
      instructions: s.instructions ?? '', policy: s.model ? { model: s.model } : {},
      sources: (s.sources ?? []).map(source => ({ id: nextId('src'), kind: source.kind, ref: source.ref, label: source.label ?? source.ref.split('/').filter(Boolean).pop(), addedBy: 'you', at: '2026-10-01T09:00:00Z', check: { state: 'ok' } })),
    });
    if (s.pinned) store.pins.push(id);
  }
  // A top-level place with no tint gets the least-used one, as the store does.
  for (const place of store.places) if (!place.tint && !place.parents.length) place.tint = leastUsed();
  const base = clock();
  (seed.chats ?? []).forEach((c, index) => {
    store.chats.push({
      id: c.id, title: c.title ?? '', sessionFile: sessionFileFor(c.id), at: c.at ?? new Date(base - (index + 1) * 7 * 60_000).toISOString(),
      live: !!c.live || !!c.needsYou, doing: c.doing ?? (c.needsYou ? 'waiting on you' : c.live ? 'working' : ''), needsYou: !!c.needsYou, reason: c.reason, archived: false,
      tasks: { running: 0, incomplete: 0, done: 0, failed: 0, ...c.tasks },
    });
    for (const ref of c.places ?? []) store.members.push({ chatId: c.id, placeId: byName(ref)!.id, addedBy: 'you', at: '2026-10-01T09:00:00Z' });
  });

  function leastUsed(): Tint {
    const use = new Map(TINTS.map(tint => [tint, 0]));
    for (const place of store.places) if (place.tint) use.set(place.tint, (use.get(place.tint) ?? 0) + 1);
    return TINTS.reduce((best, tint) => ((use.get(tint) ?? 0) < (use.get(best) ?? 0) ? tint : best), TINTS[0]);
  }

  // ---- reads ----------------------------------------------------------------------------------------------------------
  const get = (id: string) => store.places.find(place => place.id === id);
  const must = (id: string) => { const place = get(id); if (!place) throw new Refusal(404, 'not_found', 'That place doesn\'t exist any more.'); return place; };
  const active = () => store.places.filter(place => !place.archived);
  const childrenOf = (id: string) => active().filter(place => place.parents.includes(id));
  const descendants = (id: string): Set<string> => {
    const out = new Set<string>();
    const walk = (at: string) => { for (const child of childrenOf(at)) if (!out.has(child.id)) { out.add(child.id); walk(child.id); } };
    walk(id);
    return out;
  };
  const effectiveTint = (place: Place, seen = new Set<string>()): Tint => {
    if (place.tint) return place.tint;
    const parent = place.parents[0] ? get(place.parents[0]) : undefined;
    if (!parent || seen.has(parent.id)) return 'graphite';
    seen.add(place.id);
    return effectiveTint(parent, seen);
  };
  const chatsIn = (placeId: string) => store.members.filter(m => m.placeId === placeId).map(m => store.chats.find(c => c.id === m.chatId)).filter((c): c is Chat => !!c && !c.archived);
  const isRunning = (chat: Chat) => chat.live && (chat.tasks.running > 0 || chat.doing === 'working');
  const rollupOf = (chats: Chat[]): StatusRollup => ({
    chats: chats.length, running: chats.filter(isRunning).length, needsYou: chats.filter(c => c.needsYou).length,
    incomplete: chats.filter(c => c.tasks.incomplete > 0).length, failedTasks: chats.reduce((n, c) => n + c.tasks.failed, 0),
  });
  const add = (a: StatusRollup, b: Partial<StatusRollup> | undefined): StatusRollup => ({
    chats: a.chats + (b?.chats ?? 0), running: a.running + (b?.running ?? 0), needsYou: a.needsYou + (b?.needsYou ?? 0),
    incomplete: a.incomplete + (b?.incomplete ?? 0), failedTasks: a.failedTasks + (b?.failedTasks ?? 0),
  });
  const statusOf = (id: string) => add(rollupOf(chatsIn(id)), overrides.get(id));
  const inclusiveChats = (id: string) => { const ids = new Set(chatsIn(id).map(c => c.id)); for (const d of descendants(id)) for (const c of chatsIn(d)) ids.add(c.id); return store.chats.filter(c => ids.has(c.id)); };
  const statusInclusiveOf = (id: string) => {
    let total = rollupOf(inclusiveChats(id));
    for (const placeId of [id, ...descendants(id)]) total = add(total, overrides.get(placeId));
    return total;
  };
  const countsOf = (id: string): PlaceCounts => ({ children: childrenOf(id).length, descendants: descendants(id).size, chats: chatsIn(id).length, chatsInclusive: inclusiveChats(id).length });
  const view = (place: Place): PlaceView => ({
    id: place.id, name: place.name, parents: [...place.parents], tint: place.tint, effectiveTint: effectiveTint(place), archived: place.archived,
    pinned: store.pins.includes(place.id), createdAt: place.createdAt, lastOpenedAt: place.lastOpenedAt, archivedAt: place.archivedAt,
    counts: countsOf(place.id), status: statusOf(place.id), statusInclusive: statusInclusiveOf(place.id),
    hasInstructions: place.instructions.trim().length > 0, sourceCount: place.sources.length,
    alsoIn: place.parents.slice(1).map(parentId => ({ id: parentId, name: get(parentId)?.name ?? '' })),
  });
  const detail = (place: Place): PlaceDetail => ({ ...view(place), instructions: place.instructions, sources: place.sources.map(s => ({ ...s, check: { ...s.check } })), policy: { ...place.policy } });
  const unplaced = () => store.chats.filter(c => !c.archived && !store.members.some(m => m.chatId === c.id && get(m.placeId)));
  const rail = () => {
    const pinned = store.pins.map(get).filter((p): p is Place => !!p && !p.archived).map(view);
    const window = clock() - 12 * 3_600_000;
    const open = active().filter(p => !store.pins.includes(p.id) && p.lastOpenedAt).map(view)
      .filter(v => (!closedPlaces.has(v.id) && Date.parse(v.lastOpenedAt!) >= window) || v.statusInclusive.running > 0 || v.statusInclusive.needsYou > 0)
      .map(v => ({ ...v, ...(closedPlaces.has(v.id) ? { closed: true } : {}) }))
      .sort((a, b) => (b.lastOpenedAt ?? '').localeCompare(a.lastOpenedAt ?? '')).slice(0, 12);
    return { pinned, open, openWindowHours: 12 };
  };
  const totals = () => {
    const placed = store.chats.filter(c => !c.archived && store.members.some(m => m.chatId === c.id && get(m.placeId)));
    const all = store.chats.filter(c => !c.archived);
    return { places: store.places.length, placed: placed.length, unplaced: unplaced().length, running: all.filter(isRunning).length, needsYou: all.filter(c => c.needsYou).length, missingChats: 0 };
  };
  const nowView = () => ({ chats: unplaced().length, status: rollupOf(unplaced()) });
  const graph = (archived: boolean): PlacesGraph => ({
    revision, generation: revision, nodes: (archived ? store.places : active()).map(view), unplaced: unplaced().map(c => c.id), places: (archived ? store.places : active()).map(view), rail: rail(), now: nowView(), totals: totals(), readAt: now(),
  });
  const chatRow = (chat: Chat, inPlace?: string): ChatRow => ({
    id: chat.id, title: chat.title, project: 'app', workspace: '/w', sessionFile: chat.sessionFile, at: chat.at, archived: chat.archived,
    live: chat.live, doing: chat.live ? chat.doing : '', needsYou: chat.needsYou, reason: chat.reason, tasks: { ...chat.tasks },
    places: store.members.filter(m => m.chatId === chat.id).map(m => ({ m, p: get(m.placeId) })).filter(x => x.p && !x.p.archived)
      .map(({ m, p }) => ({ id: p!.id, name: p!.name, tint: effectiveTint(p!), addedBy: m.addedBy })),
    addedBy: inPlace ? store.members.find(m => m.chatId === chat.id && m.placeId === inPlace)?.addedBy : undefined,
  });
  const newest = (chats: Chat[]) => [...chats].sort((a, b) => b.at.localeCompare(a.at));
  const attention = (placeIds: string[]) => {
    const seen = new Set<string>();
    const needs: HomeDigest['attention'] = [], running: HomeDigest['attention'] = [];
    for (const placeId of placeIds) {
      const place = get(placeId)!;
      for (const chat of chatsIn(placeId)) {
        if (seen.has(chat.id)) continue;
        if (chat.needsYou) { seen.add(chat.id); needs.push({ kind: 'needsYou', chatId: chat.id, chatTitle: chat.title, placeId, placeName: place.name, text: chat.reason ?? '' }); }
        else if (isRunning(chat)) { seen.add(chat.id); running.push({ kind: 'running', chatId: chat.id, chatTitle: chat.title, placeId, placeName: place.name, text: chat.title }); }
      }
    }
    return [...needs, ...running];
  };
  const ancestry = (place: Place): Crumb[] => {
    const chain: Crumb[] = [];
    const seen = new Set([place.id]);
    let parent = place.parents[0] ? get(place.parents[0]) : undefined;
    while (parent && !seen.has(parent.id)) { chain.unshift({ id: parent.id, name: parent.name, tint: effectiveTint(parent) }); seen.add(parent.id); parent = parent.parents[0] ? get(parent.parents[0]) : undefined; }
    return chain;
  };
  function home(id: string): HomeDigest {
    const base = { readAt: now(), revision, missingChats: 0, chatsTruncated: false };
    if (id === 'root') {
      const placed = newest(store.chats.filter(c => !c.archived && store.members.some(m => m.chatId === c.id && get(m.placeId) && !get(m.placeId)!.archived)));
      const top = active().filter(p => !p.parents.some(parent => get(parent) && !get(parent)!.archived));
      return { ...base, kind: 'root', title: 'All places', breadcrumb: [], children: top.map(view), chats: placed.map(c => chatRow(c)),
        attention: attention(active().map(p => p.id)), status: rollupOf(placed), counts: { children: top.length, descendants: active().length, chats: 0, chatsInclusive: placed.length } };
    }
    if (id === 'now') {
      const loose = newest(unplaced());
      return { ...base, kind: 'now', title: 'Now', breadcrumb: [], children: [], chats: loose.map(c => chatRow(c)), attention: [], status: rollupOf(loose),
        counts: { children: 0, descendants: 0, chats: loose.length, chatsInclusive: loose.length } };
    }
    const place = must(id);
    return { ...base, kind: 'place', title: place.name, place: detail(place), breadcrumb: ancestry(place), children: childrenOf(id).map(view),
      chats: newest(chatsIn(id)).map(c => chatRow(c, id)), attention: attention([id, ...descendants(id)]), status: statusOf(id), counts: countsOf(id) };
  }

  // ---- the untouched-place suggestion (internal/placegraph/stale.go: 60 days untouched, 30 days of "Not now") ------------
  const STALE_AFTER = 60, STALE_SNOOZE = 30;
  const touchedAt = (place: Place) => {
    const stamps = [place, ...[...descendants(place.id)].map(get).filter((p): p is Place => !!p)].flatMap(p => [p.createdAt, p.lastOpenedAt, ...chatsIn(p.id).map(c => c.at)]);
    return Math.max(...stamps.filter((t): t is string => !!t).map(t => Date.parse(t)).filter(t => !Number.isNaN(t)));
  };
  const staleAnswer = () => {
    const at = clock();
    const places = active().filter(place => {
      if (store.pins.includes(place.id)) return false;
      const busy = statusInclusiveOf(place.id);
      if (busy.running > 0 || busy.needsYou > 0) return false;
      const until = snoozed.get(place.id);
      if (until && at < Date.parse(until)) return false;
      return at - touchedAt(place) >= STALE_AFTER * DAY;
    }).map(place => ({ id: place.id, name: place.name, touchedAt: new Date(touchedAt(place)).toISOString(), daysUntouched: Math.floor((at - touchedAt(place)) / DAY) }))
      .sort((a, b) => a.touchedAt.localeCompare(b.touchedAt) || a.name.localeCompare(b.name));
    return { revision, readAt: now(), afterDays: STALE_AFTER, snoozeDays: STALE_SNOOZE, places };
  };
  function staleSnooze(id: string) {
    must(id);
    const until = new Date(clock() + STALE_SNOOZE * DAY).toISOString();
    snoozed.set(id, until);
    return { ok: true, placeId: id, until };
  }

  // ---- writes ---------------------------------------------------------------------------------------------------------
  /** One store call: checks ifRevision, keeps the state before, applies, and returns its receipt (or none for a no-op). */
  const snapshot = (): Store => structuredClone(store);
  function commit(action: string, subject: string | undefined, apply: () => boolean | void): Receipt | undefined {
    const before = snapshot();
    const changed = apply();
    if (changed === false) return undefined;
    const receipt: Receipt = { id: nextId('rc'), action, subject, beforeRevision: revision, afterRevision: revision + 1, at: now() };
    revision += 1;
    worldSeq += 1;
    history.push({ receipt, before });
    return receipt;
  }
  const checkRevision = (body: Record<string, unknown>) => {
    if ((body.ifGeneration ?? body.ifRevision) !== undefined && (body.ifGeneration ?? body.ifRevision) !== revision) throw new Refusal(409, 'stale', 'The places changed in another window. Look again and retry.');
  };
  const mutation = (receipts: (Receipt | undefined)[], place?: Place, extra: Record<string, unknown> = {}): Mutation & Record<string, unknown> => {
    const list = receipts.filter((r): r is Receipt => !!r);
    return { revision, receipts: list, noop: list.length === 0, undo: list.map(r => r.id), ...(place && get(place.id) ? { place: detail(get(place.id)!) } : {}), ...extra };
  };
  const siblingsTaken = (name: string, parents: string[], except?: string) => {
    const lower = name.trim().toLowerCase();
    return store.places.some(p => p.id !== except && p.name.toLowerCase() === lower
      && (parents.length ? p.parents.some(parent => parents.includes(parent)) : p.parents.length === 0));
  };
  const nameCheck = (name: unknown) => {
    if (typeof name !== 'string' || !name.trim()) throw new Refusal(400, 'invalid', 'A place needs a name.');
    if (name.trim().length > 120) throw new Refusal(400, 'invalid', 'Names are at most 120 characters.');
    return name.trim();
  };
  const tintCheck = (tint: unknown): Tint | '' => {
    if (tint === '' || (typeof tint === 'string' && (TINTS as string[]).includes(tint))) return tint as Tint | '';
    throw new Refusal(400, 'invalid', 'Tint must be one of tide, iris, rose, sand or sage.');
  };

  function create(body: Record<string, unknown>) {
    checkRevision(body);
    const name = nameCheck(body.name);
    const parents = Array.isArray(body.parents) ? (body.parents as string[]) : typeof body.parent === 'string' ? [body.parent] : [];
    for (const parent of parents) must(parent);
    if (siblingsTaken(name, parents)) throw new Refusal(409, 'name_taken', 'Another place here already has that name.');
    const id = nextId('pl');
    const receipt = commit('place.create', id, () => {
      store.places.push({ id, name, parents: [...parents], tint: body.tint !== undefined ? tintCheck(body.tint) : parents.length ? '' : leastUsed(), archived: false,
        createdAt: now(), instructions: typeof body.instructions === 'string' ? body.instructions : '', sources: [], policy: {} });
    });
    return mutation([receipt], get(id));
  }

  function update(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const place = must(id);
    const receipts: Receipt[] = [];
    const step = (action: string, apply: () => boolean | void) => {
      try { const r = commit(action, id, apply); if (r) receipts.push(r); }
      catch (failure) { if (failure instanceof Refusal) throw new Refusal(failure.status, failure.code, failure.message, { applied: receipts }); throw failure; }
    };
    if (body.name !== undefined) step('place.rename', () => {
      const name = nameCheck(body.name);
      if (name === place.name) return false;
      if (siblingsTaken(name, place.parents, id)) throw new Refusal(409, 'name_taken', 'Another place here already has that name.');
      place.name = name;
    });
    if (body.tint !== undefined) step('place.tint', () => { const tint = tintCheck(body.tint); if (tint === place.tint) return false; place.tint = tint; });
    if (body.instructions !== undefined) step('place.context', () => { const text = String(body.instructions); if (text === place.instructions) return false; place.instructions = text; });
    if (body.policy !== undefined) step('place.policy', () => { place.policy = { ...(body.policy as object) }; });
    // The receipts' changes apply to the live object; `place` above is a reference into the store, so re-read it.
    return mutation(receipts, get(id));
  }

  function parents(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const place = must(id);
    const verbs = ['add', 'remove', 'set'].filter(key => body[key] !== undefined);
    if (verbs.length !== 1) throw new Refusal(400, 'invalid', 'Say exactly one of add, remove or set.');
    const cycle = (parent: string) => {
      if (parent === id) throw new Refusal(409, 'cycle', 'That would put a place inside itself.');
      if (descendants(id).has(parent)) throw new Refusal(409, 'cycle', `That would put “${place.name}” inside “${get(parent)?.name ?? ''}”.`);
    };
    const receipt = commit('place.parents', id, () => {
      if (body.add !== undefined) {
        const parent = String(body.add); must(parent); cycle(parent);
        if (place.parents.includes(parent)) return false;
        place.parents.push(parent);
      } else if (body.remove !== undefined) {
        const parent = String(body.remove);
        if (!place.parents.includes(parent)) return false;
        place.parents = place.parents.filter(p => p !== parent);
      } else {
        const next = (body.set as string[]).map(String);
        for (const parent of next) { must(parent); cycle(parent); }
        if (JSON.stringify(next) === JSON.stringify(place.parents)) return false;
        place.parents = next;
      }
    });
    return mutation([receipt], get(id));
  }

  function archive(id: string, body: Record<string, unknown>, archived: boolean) {
    checkRevision(body);
    const place = must(id);
    if (place.archived === archived) throw archived ? new Refusal(409, 'archived', 'That place is archived. Restore it first.') : new Refusal(409, 'not_archived', 'That place isn\'t archived.');
    const receipt = commit(archived ? 'place.archive' : 'place.restore', id, () => { place.archived = archived; place.archivedAt = archived ? now() : undefined; });
    return mutation([receipt], get(id));
  }

  const impact = (id: string) => {
    const here = chatsIn(id).map(c => c.id);
    return { children: childrenOf(id).length, chatsHere: here.length, wouldBeUnplaced: here.filter(chat => !store.members.some(m => m.chatId === chat && m.placeId !== id && get(m.placeId))) };
  };

  function remove(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const place = must(id);
    const preview = impact(id);
    const receipt = commit('place.delete', id, () => {
      for (const child of store.places.filter(p => p.parents.includes(id))) child.parents = [...new Set(child.parents.flatMap(p => (p === id ? place.parents : [p])))];
      store.members = store.members.filter(m => m.placeId !== id);
      store.pins = store.pins.filter(p => p !== id);
      store.places = store.places.filter(p => p.id !== id);
      overrides.delete(id);
    });
    return mutation([receipt], undefined, { result: { childrenMoved: preview.children, unfiled: preview.chatsHere, nowUnplaced: preview.wouldBeUnplaced } });
  }

  function merge(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const place = must(id);
    const into = String(body.into ?? '');
    const target = must(into);
    if (into === id || descendants(id).has(into)) throw new Refusal(409, 'cycle', `That would put “${place.name}” inside “${target.name}”.`);
    const moved = chatsIn(id).length;
    const receipt = commit('place.merge', id, () => {
      for (const m of store.members.filter(m => m.placeId === id)) if (!store.members.some(o => o.chatId === m.chatId && o.placeId === into)) store.members.push({ ...m, placeId: into });
      store.members = store.members.filter(m => m.placeId !== id);
      for (const child of store.places.filter(p => p.parents.includes(id))) child.parents = [...new Set(child.parents.map(p => (p === id ? into : p)))];
      target.sources.push(...place.sources);
      store.pins = store.pins.filter(p => p !== id);
      store.places = store.places.filter(p => p.id !== id);
    });
    return mutation([receipt], get(into), { result: { childrenMoved: childrenOf(into).length, unfiled: 0, nowUnplaced: [], chatsMoved: moved } });
  }

  function pin(id: string, body: Record<string, unknown>, on: boolean) {
    checkRevision(body);
    must(id);
    const receipt = commit(on ? 'place.pin' : 'place.unpin', id, () => {
      const before = JSON.stringify(store.pins);
      const rest = store.pins.filter(p => p !== id);
      if (on) {
        const index = typeof body.index === 'number' && body.index >= 0 ? Math.min(body.index, rest.length) : rest.length;
        rest.splice(index, 0, id);
      }
      store.pins = rest;
      return JSON.stringify(store.pins) !== before;
    });
    return mutation([receipt], get(id));
  }

  function addSource(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const place = must(id);
    const kind = String(body.kind ?? '') as SourceKind;
    let ref = String(body.ref ?? '').trim();
    if (!ref) throw new Refusal(400, 'invalid_source', 'Say what to add.');
    let label = typeof body.label === 'string' && body.label ? body.label : undefined;
    if (kind === 'folder' || kind === 'file' || kind === 'repo') {
      if (!ref.startsWith('/')) throw new Refusal(400, 'invalid_source', 'A folder or file needs its full path.');
      ref = ref.replace(/\/+$/, '') || '/';
      if (!disk.has(ref)) throw new Refusal(404, 'invalid_source', 'That path doesn\'t exist.');
      label ??= ref.split('/').filter(Boolean).pop();
    } else if (kind === 'url') {
      if (!/^https?:\/\//i.test(ref)) throw new Refusal(400, 'invalid_source', 'Only web addresses starting with http or https can be added.');
      label ??= new URL(ref).host;
    } else if (kind === 'chat') {
      if (!store.chats.some(c => c.id === ref)) throw new Refusal(404, 'invalid_source', 'That conversation isn\'t saved on this machine.');
    } else throw new Refusal(400, 'invalid_source', 'That kind of source isn\'t one of folder, repo, file, link or chat.');
    if (place.sources.some(s => s.kind === kind && s.ref === ref)) throw new Refusal(409, 'duplicate_source', 'That is already in this place.');
    const receipt = commit('place.context', id, () => {
      place.sources.push({ id: nextId('src'), kind, ref, label, addedBy: 'you', at: now(), check: kind === 'url' ? { state: 'unknown', note: 'Links aren\'t fetched until a chat uses them.' } : { state: 'ok' } });
    });
    return mutation([receipt], get(id));
  }

  function removeSource(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const place = must(id);
    const sourceId = String(body.sourceId ?? '');
    if (!sourceId) throw new Refusal(400, 'invalid', 'Say which source to remove.');
    if (!place.sources.some(s => s.id === sourceId)) throw new Refusal(404, 'not_found', 'That source isn\'t in this place any more.');
    const receipt = commit('place.context', id, () => { place.sources = place.sources.filter(s => s.id !== sourceId); });
    return mutation([receipt], get(id));
  }

  /** The chat a filing names: one the world saved, or one a live bridge conversation holds (it joins the world as it is filed). */
  const knownChat = (chatId: string) => {
    const saved = store.chats.find(c => c.id === chatId);
    if (saved) return saved;
    if (!live.has(chatId)) return undefined;
    const fresh: Chat = { id: chatId, title: '', sessionFile: sessionFileFor(chatId), at: now(), live: true, doing: 'working', needsYou: false, archived: false, tasks: { running: 0, incomplete: 0, done: 0, failed: 0 } };
    return fresh;
  };

  function members(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    const chats = Array.isArray(body.chats) ? (body.chats as unknown[]).map(String) : [];
    if (!chats.length) throw new Refusal(400, 'invalid', 'Say which chats to file.');
    const moveFrom = typeof body.moveFrom === 'string' ? body.moveFrom : undefined;
    if (id === 'root') throw new Refusal(400, 'reserved', 'All places and Now are built in; they can\'t be changed this way.');
    if (id === 'now' && !moveFrom) throw new Refusal(400, 'reserved', 'All places and Now are built in; they can\'t be changed this way.');
    if (id !== 'now') must(id);
    if (moveFrom && moveFrom !== 'now') must(moveFrom);
    const addedBy: AddedBy = body.addedBy === 'ai' ? 'ai' : 'you';
    const found = chats.map(knownChat);
    if (found.some(c => !c)) throw new Refusal(404, 'unknown_chat', 'That conversation isn\'t saved on this machine, so it can\'t be filed yet.');
    for (const chat of found) if (chat && !store.chats.includes(chat)) store.chats.push(chat);
    const receipts: Receipt[] = [];
    for (const chatId of chats) {
      const r = commit(moveFrom ? 'chat.move' : 'chat.file', chatId, () => {
        const before = store.members.length;
        let changed = false;
        if (moveFrom && moveFrom !== 'now') { store.members = store.members.filter(m => !(m.chatId === chatId && m.placeId === moveFrom)); changed = store.members.length !== before; }
        if (id !== 'now' && !store.members.some(m => m.chatId === chatId && m.placeId === id)) { store.members.push({ chatId, placeId: id, addedBy, at: now() }); changed = true; }
        return changed;
      });
      if (r) receipts.push(r);
    }
    const memberships = id === 'now' ? [] : store.members.filter(m => m.placeId === id && chats.includes(m.chatId)).map(m => ({ ...m }));
    return mutation(receipts, id === 'now' ? undefined : get(id), { memberships });
  }

  function removeMembers(id: string, body: Record<string, unknown>) {
    checkRevision(body);
    must(id);
    const chats = Array.isArray(body.chats) ? (body.chats as unknown[]).map(String) : [];
    const receipts: Receipt[] = [];
    for (const chatId of chats) {
      const r = commit('chat.unfile', chatId, () => {
        const before = store.members.length;
        store.members = store.members.filter(m => !(m.chatId === chatId && m.placeId === id));
        return store.members.length !== before;
      });
      if (r) receipts.push(r);
    }
    return mutation(receipts, get(id));
  }

  function undo(body: Record<string, unknown>) {
    const ids = Array.isArray(body.receipts) ? (body.receipts as unknown[]).map(String) : [];
    if (!ids.length) throw new Refusal(400, 'invalid', 'Say which change to undo.');
    let undone = 0;
    for (const receiptId of [...ids].reverse()) {
      const at = history.findIndex(h => h.receipt.id === receiptId);
      if (at < 0) throw new Refusal(410, 'no_receipt', 'That change can no longer be undone.', { undone });
      // Only the newest change the store still holds can be taken back; anything newer means the places moved on.
      if (at !== history.length - 1) throw new Refusal(409, 'cannot_undo', 'That can\'t be undone now because the places changed afterwards.', { undone });
      store = history[at].before;
      history.splice(at, 1);
      revision += 1;
      undone += 1;
    }
    return { revision, undone };
  }

  function visit(id: string) {
    const place = must(id);
    place.lastOpenedAt = now();
    closedPlaces.delete(id);
    worldSeq += 1;
    return { ok: true };
  }

  // ---- the world feed -------------------------------------------------------------------------------------------------
  const worldRows = () => store.chats.map(chat => ({
    session: chat.id, title: chat.title, project: 'app', workspace: '/w', sessionFile: chat.sessionFile, sourceFolders: [], state: chat.live ? chat.doing || 'open' : 'closed',
    live: chat.live, open: chat.live, running: isRunning(chat), needsYou: chat.needsYou, failed: chat.tasks.failed,
    tasks: { ...chat.tasks, total: chat.tasks.running + chat.tasks.incomplete + chat.tasks.done + chat.tasks.failed }, at: chat.at,
  }));
  const worldItems = () => store.chats.filter(c => c.needsYou).map(chat => ({ key: `q:${chat.id}`, session: chat.id, kind: 'consent', text: chat.reason ?? '', sourceFolders: [], title: chat.title, answerable: false }));
  const sse = (record: unknown, seq: number) => `: connected\n\nid: ${seq}\ndata: ${JSON.stringify(record)}\n\n`;
  async function events(route: Route, after: number) {
    if (after < worldSeq) {
      const record = after === 0
        ? { seq: worldSeq, type: 'reset', at: now(), payload: { rows: worldRows(), items: worldItems() } }
        : { seq: worldSeq, type: 'world', at: now(), payload: { rows: worldRows(), removed: [] } };
      await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: sse(record, worldSeq) });
      return;
    }
    const deadline = Date.now() + 60_000;
    while (!closed && Date.now() < deadline && worldSeq <= after) await new Promise(resolve => setTimeout(resolve, 25));
    if (closed || worldSeq <= after) { await route.abort().catch(() => undefined); return; }
    await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: sse({ seq: worldSeq, type: 'world', at: now(), payload: { rows: worldRows(), removed: [] } }, worldSeq) }).catch(() => undefined);
  }

  // ---- routing --------------------------------------------------------------------------------------------------------
  page.on('request', request => {
    const url = new URL(request.url());
    if (!url.pathname.includes('/api/engine/')) return;
    let body: Record<string, unknown> = {};
    try { body = request.postData() ? JSON.parse(request.postData()!) as Record<string, unknown> : {}; } catch { body = {}; }
    traffic.push({ method: request.method(), path: url.pathname.replace(/^.*\/api\/engine/, ''), body });
  });

  const keyOf = (method: string, parts: string[]): RouteKey => {
    const [root, id, verb, sub] = parts;
    if (root === 'world') return 'world';
    if (root === 'events') return 'events';
    if (root === 'chats') return 'chat-places';
    if (method === 'GET') return !id ? 'graph' : id === 'status' ? 'status' : id === 'rail' ? 'rail' : id === 'stale' ? 'stale' : verb === 'delete-preview' ? 'delete-preview' : verb === 'effective-model' ? 'effective-model' : 'home';
    if (!id) return 'create';
    if (id === 'undo') return 'undo';
    if (!verb) return 'update';
    if (sub === 'remove') return `${verb}/remove` as RouteKey;
    return verb as RouteKey;
  };

  const serve = async (route: Route) => {
    const request = route.request();
    const url = new URL(request.url());
    const parts = url.pathname.replace(/^.*\/api\/engine/, '').split('/').filter(Boolean).map(decodeURIComponent);
    if (!['places', 'chats', 'world', 'events'].includes(parts[0] ?? '')) return route.fallback();
    const method = request.method();
    let body: Record<string, unknown> = {};
    if (method === 'POST' && request.postData()) {
      try { body = JSON.parse(request.postData()!) as Record<string, unknown>; } catch { return json(route, { error: 'That request was not JSON.', code: 'invalid' }, 400); }
    }
    calls.push({ method, path: url.pathname.replace(/^.*\/api\/engine/, ''), body });
    const key = keyOf(method, parts);
    const force = forced.get(key);
    if (force === 'abort') return route.abort('connectionrefused');
    if (force) return json(route, { error: force.error, code: force.code }, force.status);
    const [root, id, verb, sub] = parts;
    try {
      if (root === 'world') return json(route, { seq: worldSeq, rows: worldRows(), items: worldItems() });
      if (root === 'events') return events(route, Number(url.searchParams.get('after') ?? 0));
      if (root === 'chats') {
        if (verb !== 'places' || method !== 'GET') return json(route, { error: 'unknown route', code: 'not_found' }, 404);
        const known = store.chats.some(c => c.id === id);
        return json(route, { chatId: id, known, places: store.members.filter(m => m.chatId === id && get(m.placeId)).map(m => { const p = get(m.placeId)!; return { id: p.id, name: p.name, tint: effectiveTint(p), addedBy: m.addedBy, at: m.at, archived: p.archived }; }) });
      }
      if (root === 'places' && id === 'proposals') {
        if (method === 'GET' && !verb) return json(route, offersView());
        const index = offers.findIndex(offer => offer.id === verb);
        const offer = offers[index];
        if (method !== 'POST' || !offer) return json(route, { error: 'That offer is no longer open.', code: 'gone' }, 409);
        if (sub === 'decline') { offers.splice(index, 1); return json(route, offersView()); }
        if (sub !== 'accept') return json(route, { error: 'unknown route' }, 404);
        if (body.offerVersion !== offer.offerVersion) return json(route, { error: 'That offer changed since you saw it.', code: 'stale_offer' }, 409);
        const receipts: Receipt[] = [];
        let target = offer.placeId;
        if (offer.kind === 'create') { const made = create({ name: offer.name, parent: offer.parentId }); target = made.place?.id; receipts.push(...made.receipts); }
        if (!target) return json(route, { error: 'That place no longer exists.' }, 409);
        receipts.push(...members(target, { chats: offer.chatIds, addedBy: 'ai' }).receipts);
        offers.splice(index, 1);
        return json(route, { proposal: { ...offer, status: 'accepted' }, placeId: target, receipts, view: offersView() });
      }
      if (method === 'GET') {
        if (!id) return json(route, graph(url.searchParams.get('archived') === '1'));
        if (id === 'status') {
          const places = Object.fromEntries(store.places.map(p => [p.id, { status: statusOf(p.id), statusInclusive: statusInclusiveOf(p.id) }]));
          return json(route, { revision, readAt: now(), places, now: nowView(), totals: totals() });
        }
        if (id === 'rail') return json(route, rail());
        if (id === 'stale') return json(route, staleAnswer());
        if (verb === 'delete-preview') { must(id); return json(route, impact(id)); }
        if (verb === 'effective-model') {
          // The nearest place at or above this one that names a model decides, as the engine's rule does for a single parent line.
          must(id);
          for (let at: Place | undefined = get(id), hops = 0; at && hops < 3; at = get(at.parents[0] ?? ''), hops++) {
            if (at.policy.model) return json(route, { placeId: id, revision, state: 'applies', model: at.policy.model, outcome: 'agreed', decidedBy: { id: at.id, name: at.name, model: at.policy.model } });
          }
          return json(route, { placeId: id, revision, state: 'none' });
        }
        if (verb) return json(route, { error: 'unknown route', code: 'not_found' }, 404);
        return json(route, home(id));
      }
      if (method !== 'POST') return json(route, { error: 'method not allowed' }, 405);
      if (!id) return json(route, create(body));
      if (id === 'undo') return json(route, undo(body));
      if (id === 'rail') {
        checkRevision(body);
        const placeId = String(body.place ?? '');
        if (body.op === 'pin' || body.op === 'unpin') return json(route, pin(placeId, body, body.op === 'pin'));
        if (body.op === 'visit') visit(placeId);
        else if (body.op === 'close') { must(placeId); closedPlaces.add(placeId); worldSeq += 1; }
        else if (body.op === 'reorder') {
          const order = body.order;
          if (!Array.isArray(order) || order.length !== store.pins.length || new Set(order).size !== order.length || order.some(id => !store.pins.includes(id))) throw new Refusal(400, 'invalid', 'Name every pinned place once.');
          store.pins = [...order];
          worldSeq += 1;
        } else throw new Refusal(400, 'invalid', 'Unknown rail operation.');
        return json(route, { ...mutation([]), rail: rail() });
      }
      if (!verb) return json(route, update(id, body));
      if (sub === 'remove') return json(route, verb === 'sources' ? removeSource(id, body) : removeMembers(id, body));
      switch (verb) {
        case 'parents': return json(route, parents(id, body));
        case 'archive': return json(route, archive(id, body, true));
        case 'restore': return json(route, archive(id, body, false));
        case 'delete': return json(route, remove(id, body));
        case 'merge': return json(route, merge(id, body));
        case 'pin': return json(route, pin(id, body, true));
        case 'unpin': return json(route, pin(id, body, false));
        case 'sources': return json(route, addSource(id, body));
        case 'members': return json(route, members(id, body));
        case 'visit': return json(route, visit(id));
        case 'stale-snooze': return json(route, staleSnooze(id));
        default: return json(route, { error: 'unknown route', code: 'not_found' }, 404);
      }
    } catch (failure) {
      if (failure instanceof Refusal) return json(route, { error: failure.message, code: failure.code, ...failure.extra }, failure.status);
      throw failure;
    }
  };
  for (const window of [page, ...alsoServe]) await window.route('**/api/engine/**', serve);

  const nudge = () => { worldSeq += 1; };
  return {
    calls, traffic,
    state: () => ({ ...snapshot(), revision }),
    id: name => { const place = store.places.find(p => p.name === name); if (!place) throw new Error(`mock-places: no place called ${name}`); return place.id; },
    setStatus: (placeId, rollup) => { overrides.set(placeId, rollup); nudge(); },
    setChat: (chatId, patch) => {
      const chat = store.chats.find(chat => chat.id === chatId);
      if (!chat) throw new Error(`mock-places: no chat called ${chatId}`);
      const { places: _places, tasks, id: _id, ...fields } = patch;
      Object.assign(chat, fields);
      if (tasks) Object.assign(chat.tasks, tasks);
      nudge();
    },
    fail: (key, value) => { if (value) forced.set(key, value); else forced.delete(key); },
    nudge,
    propose: next => { offers.splice(0, offers.length, ...structuredClone(next)); },
    posts: suffix => calls.filter(call => call.method === 'POST' && call.path.endsWith(suffix)),
    snoozes: () => Object.fromEntries(snoozed),
  };
}
