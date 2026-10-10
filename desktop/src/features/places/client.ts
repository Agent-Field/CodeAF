import { engineFetch } from '../../design/engineFetch.ts';
/**
 * Typed client for the engine's Places routes (internal/desktopbridge/places*.go).
 *
 * It owns no place state and no policy: writes without an explicit generation first read the graph, every answer
 * is validated before it is returned, and a refusal arrives as a PlacesError
 * carrying the engine's own sentence. Nothing here guesses a status; a number
 * the engine did not send is not invented.
 *
 * The transport is injectable so tests (and the browser dev proxy) can stand in
 * for the native connection. The default asks Tauri for the loopback URL and
 * bearer token exactly as the conversation client does.
 */

export type { Tint, AddedBy, SourceKind, StatusRollup, PlaceCounts, PlaceRef, PlaceView, SourceCheck, SourceView, PlacePolicy, PlaceDetail, RailView, NowView, PlacesTotals, PlacesRecovery, PlacesGraph, PlacesStatus, ChatPlace, ChatRow, Attention, RecapItem, HomeRecap, Crumb, HomeDigest, Receipt, Membership, Mutation, DeleteImpact, ChatPlacesAnswer, UndoAnswer, EffectiveModelPlace, EffectiveModel, CreatePlaceAsk, UpdatePlaceAsk, ParentsAsk } from './wire.ts';
import type { AddedBy, SourceKind, StatusRollup, PlaceCounts, PlaceView, PlaceDetail, RailView, NowView, PlacesTotals, PlacesGraph, PlacesStatus, ChatRow, HomeDigest, Receipt, Mutation, DeleteImpact, ChatPlacesAnswer, UndoAnswer, EffectiveModel, CreatePlaceAsk, UpdatePlaceAsk, ParentsAsk } from './wire.ts';
import type { ChatSource, PlaceSession, HomeView, PolicyField } from './wire.ts';
import { createUsingClient } from './using-client.ts';
export { applyPlacesRecord } from './wire.ts';
/** A refusal or failure, in the engine's own words. */
export class PlacesError extends Error {
  readonly status: number;
  /** Machine slug (name_taken, cycle, stale, unknown_chat, cannot_undo, ...); '' when the engine sent none. */
  readonly code: string;
  /** Receipts already committed when a multi-step write stopped part-way. */
  readonly applied: Receipt[];
  /** How many receipts an undo had already taken back when it stopped. */
  readonly undone?: number;
  /** True when nothing answered: the engine is not running or the proxy cannot reach it. */
  readonly unreachable: boolean;
  constructor(message: string, status = 0, code = '', applied: Receipt[] = [], undone?: number, unreachable = false) {
    super(message);
    this.name = 'PlacesError';
    this.status = status; this.code = code; this.applied = applied; this.undone = undone; this.unreachable = unreachable;
  }
}

export type PlacesRequest = { method: 'GET' | 'POST'; body?: unknown; signal?: AbortSignal };
/** Sends one request to /api/engine{path} and returns the parsed JSON, or throws PlacesError. */
export type PlacesTransport = (path: string, request: PlacesRequest) => Promise<unknown>;

export const PLACES_REQUEST_TIMEOUT_MS = 30_000;

/** The chat id of a conversation: the folder holding its transcript (session.SessionRow.ID). */
export function chatIdFromSessionFile(sessionFile: string): string {
  const parts = sessionFile.split(/[\\/]/).filter(Boolean);
  return parts.length >= 2 ? parts[parts.length - 2] : '';
}

// ---- validation ------------------------------------------------------------

const isObject = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const isInt = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
const bad = (what: string): never => { throw new PlacesError(`The engine returned an invalid ${what}.`); };

function rollup(v: unknown, what: string): StatusRollup {
  if (!isObject(v) || !(['chats', 'running', 'needsYou', 'incomplete', 'failedTasks'] as const).every(k => isInt(v[k]))) return bad(what);
  return v as StatusRollup;
}
function counts(v: unknown, what: string): PlaceCounts {
  if (!isObject(v) || !(['children', 'descendants', 'chats', 'chatsInclusive'] as const).every(k => isInt(v[k]))) return bad(what);
  return v as PlaceCounts;
}
function placeView(v: unknown): PlaceView {
  if (!isObject(v) || typeof v.id !== 'string' || !v.id || typeof v.name !== 'string' || !Array.isArray(v.parents) || typeof v.effectiveTint !== 'string'
    || typeof v.archived !== 'boolean' || typeof v.pinned !== 'boolean' || typeof v.hasInstructions !== 'boolean' || !isInt(v.sourceCount) || !Array.isArray(v.alsoIn)) return bad('place');
  counts(v.counts, 'place'); rollup(v.status, 'place'); rollup(v.statusInclusive, 'place');
  return v as unknown as PlaceView;
}
function placeDetail(v: unknown): PlaceDetail {
  placeView(v);
  const d = v as Record<string, unknown>;
  if (typeof d.instructions !== 'string' || !Array.isArray(d.sources) || !isObject(d.policy)) return bad('place');
  for (const s of d.sources) if (!isObject(s) || typeof s.id !== 'string' || typeof s.kind !== 'string' || typeof s.ref !== 'string' || !isObject(s.check) || typeof s.check.state !== 'string') bad('source');
  return v as PlaceDetail;
}
function rail(v: unknown): RailView {
  if (!isObject(v) || !Array.isArray(v.pinned) || !Array.isArray(v.open) || !isInt(v.openWindowHours)) return bad('rail');
  v.pinned.forEach(placeView); v.open.forEach(placeView);
  return v as RailView;
}
function nowView(v: unknown): NowView {
  if (!isObject(v) || !isInt(v.chats)) return bad('Now summary');
  rollup(v.status, 'Now summary');
  return v as NowView;
}
function totals(v: unknown): PlacesTotals {
  if (!isObject(v) || !(['places', 'placed', 'unplaced', 'running', 'needsYou', 'missingChats'] as const).every(k => isInt(v[k]))) return bad('summary');
  return v as PlacesTotals;
}
function graph(v: unknown): PlacesGraph {
  if (!isObject(v) || !isInt(v.revision) || !Array.isArray(v.places) || typeof v.readAt !== 'string') return bad('place list');
  v.places.forEach(placeView); rail(v.rail); nowView(v.now); totals(v.totals);
  if (v.recovery !== undefined && (!isObject(v.recovery) || typeof v.recovery.kind !== 'string')) bad('recovery note');
  return v as PlacesGraph;
}
function status(v: unknown): PlacesStatus {
  if (!isObject(v) || !isInt(v.revision) || !isObject(v.places) || typeof v.readAt !== 'string') return bad('status');
  for (const entry of Object.values(v.places)) { if (!isObject(entry)) bad('status'); rollup((entry as Record<string, unknown>).status, 'status'); rollup((entry as Record<string, unknown>).statusInclusive, 'status'); }
  nowView(v.now); totals(v.totals);
  return v as PlacesStatus;
}
function chatRow(v: unknown): ChatRow {
  if (!isObject(v) || typeof v.id !== 'string' || !v.id || typeof v.title !== 'string' || typeof v.archived !== 'boolean' || typeof v.live !== 'boolean' || typeof v.needsYou !== 'boolean'
    || typeof v.doing !== 'string' || !isObject(v.tasks) || !Array.isArray(v.places)) return bad('chat');
  return v as unknown as ChatRow;
}
function home(v: unknown): HomeDigest {
  if (!isObject(v) || !['place', 'root', 'now'].includes(v.kind as string) || typeof v.title !== 'string' || !Array.isArray(v.breadcrumb) || !Array.isArray(v.children) || !Array.isArray(v.chats)
    || typeof v.chatsTruncated !== 'boolean' || !Array.isArray(v.attention) || !isInt(v.missingChats) || !isInt(v.revision)) return bad('Home');
  v.children.forEach(placeView); v.chats.forEach(chatRow); rollup(v.status, 'Home'); counts(v.counts, 'Home');
  for (const a of v.attention) if (!isObject(a) || (a.kind !== 'needsYou' && a.kind !== 'running') || typeof a.chatId !== 'string' || typeof a.text !== 'string') bad('attention item');
  if (v.kind === 'place' ? v.place === undefined : v.place !== undefined) bad('Home');
  if (v.place !== undefined) placeDetail(v.place);
  if (v.contextLine !== undefined && typeof v.contextLine !== 'string') bad('Home');
  if (v.recap !== undefined) {
    const r = v.recap;
    if (!isObject(r) || typeof r.label !== 'string' || typeof r.text !== 'string' || !r.text || !isInt(r.chats) || !isInt(r.unsummarised) || !Array.isArray(r.items)
      || !r.items.every(i => isObject(i) && typeof i.chatId === 'string' && typeof i.line === 'string')) bad('recap');
  }
  return v as unknown as HomeDigest;
}
export function validatePlacesMutation(v: unknown): Mutation {
  if (!isObject(v) || !isInt(v.revision) || !Array.isArray(v.receipts) || typeof v.noop !== 'boolean' || !Array.isArray(v.undo) || !v.undo.every(id => typeof id === 'string')) return bad('receipt');
  for (const r of v.receipts) if (!isObject(r) || typeof r.id !== 'string' || !r.id || typeof r.action !== 'string' || !isInt(r.beforeRevision) || !isInt(r.afterRevision)) bad('receipt');
  if (v.noop !== (v.receipts.length === 0) || v.undo.length !== v.receipts.length) bad('receipt');
  if (v.place !== undefined) placeDetail(v.place);
  return v as Mutation;
}

// ---- the default transport -------------------------------------------------

type Connection = { url: string; token: string };

async function defaultTransport(path: string, request: PlacesRequest): Promise<unknown> {
  const { invoke, isTauri } = await import('@tauri-apps/api/core');
  const headers = new Headers({ Accept: 'application/json' });
  let url = `/api/engine${path}`;
  if (isTauri()) {
    const connection = await invoke<Connection>('engine_connection');
    const base = new URL(connection.url);
    if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname)) throw new PlacesError('The local engine announced an invalid connection.');
    headers.set('Authorization', `Bearer ${connection.token}`);
    url = `${base.origin}/api/engine${path}`;
  }
  const init: RequestInit = { method: request.method, headers, cache: 'no-store' };
  if (request.body !== undefined) { init.body = JSON.stringify(request.body); headers.set('Content-Type', 'application/json'); }
  const clock = new AbortController();
  const timer = setTimeout(() => clock.abort(), PLACES_REQUEST_TIMEOUT_MS);
  const abort = () => clock.abort();
  if (request.signal?.aborted) abort();
  else request.signal?.addEventListener('abort', abort, { once: true });
  init.signal = clock.signal;
  let response: Response;
  try { response = await engineFetch(url, init); }
  catch (error) {
    if (request.signal?.aborted) throw error;
    throw new PlacesError('codeaf engine is not running', 0, '', [], undefined, true);
  }
  finally { clearTimeout(timer); request.signal?.removeEventListener('abort', abort); }
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const e = isObject(body) ? body : {};
    if (typeof e.error !== 'string' && response.status >= 500) throw new PlacesError('codeaf engine is not running', response.status, '', [], undefined, true);
    throw new PlacesError(
      typeof e.error === 'string' ? e.error : `The engine request failed (${response.status}).`,
      response.status, typeof e.code === 'string' ? e.code : '',
      Array.isArray(e.applied) ? e.applied as Receipt[] : [], isInt(e.undone) ? e.undone : undefined,
    );
  }
  return body;
}

/** The loopback transport every Places door shares (using-client.ts reuses it). */
export const placesTransport: PlacesTransport = defaultTransport;

// ---- the client ------------------------------------------------------------

const enc = encodeURIComponent;

export type PlacesClient = ReturnType<typeof createPlacesClient>;

export function createPlacesClient(transport: PlacesTransport = defaultTransport) {
  // The caller may supply the generation it rendered. Otherwise read it before
  // the write; never guess zero or silently retry a conflict against newer data.
  const get = (path: string, signal?: AbortSignal) => transport(path, { method: 'GET', signal });
  const post = async (path: string, body: unknown = {}, signal?: AbortSignal) => {
    const ask = isObject(body) ? body : {};
    const { ifRevision, ifGeneration, ...fields } = ask;
    const snapshot = ifGeneration === undefined && ifRevision === undefined ? await get('/places', signal) : undefined;
    const generation = ifGeneration ?? ifRevision ?? (isObject(snapshot) ? snapshot.generation ?? snapshot.revision : undefined);
    if (!isInt(generation)) return bad('place generation');
    return transport(path, { method: 'POST', body: { ...fields, ifGeneration: generation }, signal });
  };
  const usingClient = (ifGeneration?: number) => createUsingClient(async (path, request) => request.method === 'GET'
    ? get(path, request.signal) : post(path, { ...(isObject(request.body) ? request.body : {}), ifGeneration }, request.signal));
  const write = async (path: string, body?: unknown) => validatePlacesMutation(await post(path, body));
  return {
    /** The whole graph with status roll-ups, the rail and totals. */
    graph: async (opts: { archived?: boolean; signal?: AbortSignal } = {}) => graph(await get(opts.archived ? '/places?archived=1' : '/places', opts.signal)),
    /** Roll-ups only: cheap enough to poll for rail dots. */
    status: async (signal?: AbortSignal) => status(await get('/places/status', signal)),
    rail: async (signal?: AbortSignal) => rail(await get('/places/rail', signal)),
    /** A place's Home; pass 'root' for All places and 'now' for the unplaced chats. */
    home: async (id: string, signal?: AbortSignal) => home(await get(`/places/${enc(id)}`, signal)),
    homeView: async (id: string, signal?: AbortSignal): Promise<HomeView> => {
      const v = await get(`/places/${enc(id)}/home`, signal);
      if (!isObject(v) || !Array.isArray(v.breadcrumb) || !Array.isArray(v.attention) || !Array.isArray(v.children)) return bad('Home');
      if (v.place !== undefined) placeDetail(v.place);
      return v as HomeView;
    },
    deletePreview: async (id: string): Promise<DeleteImpact> => {
      const v = await get(`/places/${enc(id)}/delete-preview`);
      if (!isObject(v) || !isInt(v.children) || !isInt(v.chatsHere) || !Array.isArray(v.wouldBeUnplaced)) return bad('delete preview');
      return v as DeleteImpact;
    },
    /** What a new chat started on this place's Home would run on (read-only; no session is made). */
    effectiveModel: async (id: string, signal?: AbortSignal): Promise<EffectiveModel> => {
      const v = await get(`/places/${enc(id)}/effective-model`, signal);
      if (!isObject(v) || typeof v.placeId !== 'string' || !isInt(v.revision) || !['none', 'applies', 'needsPick', 'unavailable'].includes(v.state as string)) return bad('model answer');
      if (v.state === 'applies' && (typeof v.model !== 'string' || !v.model)) return bad('model answer');
      return v as unknown as EffectiveModel;
    },
    chatPlaces: async (chatId: string): Promise<ChatPlacesAnswer> => {
      const v = await get(`/chats/${enc(chatId)}/places`);
      if (!isObject(v) || typeof v.chatId !== 'string' || typeof v.known !== 'boolean' || (v.workspace !== undefined && typeof v.workspace !== 'string') || (v.sessionFile !== undefined && typeof v.sessionFile !== 'string') || !Array.isArray(v.places)) return bad('chat places answer');
      return v as ChatPlacesAnswer;
    },

    using: usingClient().using,
    choose: (token: string, field: PolicyField, placeId: string, ifGeneration?: number) => usingClient(ifGeneration).choose(token, field, placeId),
    apply: (token: string, field: PolicyField, ifGeneration?: number) => usingClient(ifGeneration).apply(token, field),
    fromFolder: (path: string, ifGeneration?: number) => write('/places/from-folder', { path, ifGeneration }),
    railOp: (ask: { op: 'visit' | 'close' | 'pin' | 'unpin' | 'reorder'; place?: string; index?: number; order?: string[]; ifGeneration?: number }) => write('/places/rail', ask),
    undoReceipt: async (token: string, ifGeneration?: number): Promise<{ revision: number }> => {
      const v = await post(`/places/undo/${enc(token)}`, { ifGeneration });
      if (!isObject(v) || !isInt(v.revision)) return bad('undo answer');
      return v as { revision: number };
    },
    impact: async (id: string): Promise<{ chats: number; children: number }> => {
      const v = await get(`/places/${enc(id)}/impact`);
      if (!isObject(v) || !isInt(v.chats) || !isInt(v.children)) return bad('place impact');
      return v as { chats: number; children: number };
    },
    addSessionSource: async (token: string, path: string, ifGeneration?: number): Promise<ChatSource> => {
      const v = await post(`/sessions/${enc(token)}/sources`, { path, ifGeneration });
      if (!isObject(v) || typeof v.path !== 'string' || !['said', 'kept'].includes(v.arrival as string)) return bad('chat source');
      return v as ChatSource;
    },
    createSession: async (placeId: string, ifGeneration?: number): Promise<PlaceSession> => {
      const v = await post('/sessions', { placeId, ifGeneration });
      if (!isObject(v) || typeof v.id !== 'string' || typeof v.sessionFile !== 'string' || !Array.isArray(v.entries)) return bad('place session');
      return v as PlaceSession;
    },
    createPlace: (ask: CreatePlaceAsk) => write('/places', ask),
    updatePlace: (id: string, ask: UpdatePlaceAsk) => write(`/places/${enc(id)}`, ask),
    setParents: (id: string, ask: ParentsAsk) => write(`/places/${enc(id)}/parents`, ask),
    archivePlace: (id: string, ifRevision?: number) => write(`/places/${enc(id)}/archive`, { ifRevision }),
    restorePlace: (id: string, ifRevision?: number) => write(`/places/${enc(id)}/restore`, { ifRevision }),
    deletePlace: (id: string, ifRevision?: number) => write(`/places/${enc(id)}/delete`, { ifRevision }),
    mergePlace: (id: string, into: string, ifRevision?: number) => write(`/places/${enc(id)}/merge`, { into, ifRevision }),
    pinPlace: (id: string, index?: number, ifGeneration?: number) => write(`/places/${enc(id)}/pin`, { index, ifGeneration }),
    unpinPlace: (id: string, ifGeneration?: number) => write(`/places/${enc(id)}/unpin`, { ifGeneration }),
    addSource: (id: string, ask: { kind: SourceKind; ref: string; label?: string; ifGeneration?: number; ifRevision?: number }) => write(`/places/${enc(id)}/sources`, ask),
    removeSource: (id: string, sourceId: string, ifRevision?: number) => write(`/places/${enc(id)}/sources/remove`, { sourceId, ifRevision }),
    /** Files chats in a place. With moveFrom ('now' allowed) each chat is moved instead. */
    addChats: (id: string, chats: string[], opts: { addedBy?: AddedBy; moveFrom?: string; ifGeneration?: number; ifRevision?: number } = {}) => write(`/places/${enc(id)}/members`, { chats, ...opts }),
    removeChats: (id: string, chats: string[], ifRevision?: number) => write(`/places/${enc(id)}/members/remove`, { chats, ifRevision }),
    /** Records that the person went to a place. Moves no revision and has no receipt. */
    visit: async (id: string, ifGeneration?: number): Promise<void> => {
      const v = await post(`/places/${enc(id)}/visit`, { ifGeneration });
      if (!isObject(v) || v.ok !== true) bad('visit answer');
    },
    /** Takes receipts back, newest first (pass them in the order they were made). */
    undo: async (receipts: string[], ifGeneration?: number): Promise<UndoAnswer> => {
      const v = await post('/places/undo', { receipts, ifGeneration });
      if (!isObject(v) || !isInt(v.revision) || !isInt(v.undone)) return bad('undo answer');
      return v as UndoAnswer;
    },
  };
}
