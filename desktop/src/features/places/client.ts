/**
 * Typed client for the engine's Places routes (internal/desktopbridge/places*.go).
 *
 * It owns no place state and no policy: every call is one request, every answer
 * is validated before it is returned, and a refusal arrives as a PlacesError
 * carrying the engine's own sentence. Nothing here guesses a status; a number
 * the engine did not send is not invented.
 *
 * The transport is injectable so tests (and the browser dev proxy) can stand in
 * for the native connection. The default asks Tauri for the loopback URL and
 * bearer token exactly as the conversation client does.
 */

export type Tint = 'tide' | 'iris' | 'rose' | 'sand' | 'sage' | 'graphite';
export type AddedBy = 'you' | 'ai';
export type SourceKind = 'folder' | 'repo' | 'file' | 'url' | 'chat';

/** What a place says about the work in it; counts of chats the engine could read. */
export type StatusRollup = { chats: number; running: number; needsYou: number; incomplete: number; failedTasks: number };
export type PlaceCounts = { children: number; descendants: number; chats: number; chatsInclusive: number };
export type PlaceRef = { id: string; name: string };

export type PlaceView = {
  id: string; name: string; parents: string[];
  /** The place's own choice; '' when it inherits. */
  tint: Tint | '';
  effectiveTint: Tint;
  archived: boolean; pinned: boolean;
  createdAt?: string; lastOpenedAt?: string; archivedAt?: string;
  counts: PlaceCounts;
  /** Chats filed directly here. */
  status: StatusRollup;
  /** This place and everything below it, each chat once. */
  statusInclusive: StatusRollup;
  hasInstructions: boolean; sourceCount: number;
  /** Parents after the first: "Release · also in Software". */
  alsoIn: PlaceRef[];
};

export type SourceCheck = { state: 'ok' | 'missing' | 'unreadable' | 'unknown'; note?: string; title?: string };
export type SourceView = { id: string; kind: SourceKind; ref: string; label?: string; addedBy: AddedBy; at?: string; check: SourceCheck };
export type PlacePolicy = { model?: string; permissions?: string };
export type PlaceDetail = PlaceView & { instructions: string; sources: SourceView[]; policy: PlacePolicy };

export type RailView = { pinned: PlaceView[]; open: PlaceView[]; openWindowHours: number };
export type NowView = { chats: number; status: StatusRollup };
export type PlacesTotals = { places: number; placed: number; unplaced: number; running: number; needsYou: number; missingChats: number };
export type PlacesRecovery = { kind: 'quarantined' | 'repaired'; reason: string; movedTo: string; repairs?: string[]; at: string };

export type PlacesGraph = {
  revision: number; places: PlaceView[]; rail: RailView; now: NowView; totals: PlacesTotals; readAt: string;
  /** Set when the store found a damaged file and kept a copy aside. */
  recovery?: PlacesRecovery;
};
export type PlacesStatus = {
  revision: number; readAt: string;
  places: Record<string, { status: StatusRollup; statusInclusive: StatusRollup }>;
  now: NowView; totals: PlacesTotals;
};

export type ChatPlace = { id: string; name: string; tint: Tint; addedBy: AddedBy };
export type ChatRow = {
  id: string; title: string; project: string; workspace: string;
  at?: string; created?: string; model?: string;
  archived: boolean; live: boolean; doing: string; needsYou: boolean; reason?: string;
  tasks: { running: number; incomplete: number; done: number; failed: number };
  places: ChatPlace[];
  /** Who filed it in the place this Home is about. */
  addedBy?: AddedBy;
};
export type Attention = {
  kind: 'needsYou' | 'running'; chatId: string; chatTitle: string; placeId: string; placeName: string;
  text: string; taskId?: string; since?: string;
};
export type Crumb = { id: string; name: string; tint: Tint };
export type HomeDigest = {
  kind: 'place' | 'root' | 'now'; title: string;
  /** Present only for a real place. */
  place?: PlaceDetail;
  breadcrumb: Crumb[]; children: PlaceView[]; chats: ChatRow[]; chatsTruncated: boolean;
  attention: Attention[]; status: StatusRollup; counts: PlaceCounts; missingChats: number;
  revision: number; readAt: string;
};

export type Receipt = { id: string; action: string; subject?: string; beforeRevision: number; afterRevision: number; at: string };
export type Membership = { chatId: string; placeId: string; addedBy: AddedBy; at?: string };
/** The receipt of one write. 2xx means the store committed. */
export type Mutation = {
  revision: number; receipts: Receipt[]; noop: boolean; undo: string[];
  place?: PlaceDetail; result?: Record<string, unknown>; memberships?: Membership[];
};
export type DeleteImpact = { children: number; chatsHere: number; wouldBeUnplaced: string[] };
export type ChatPlacesAnswer = {
  chatId: string; known: boolean;
  places: { id: string; name: string; tint: Tint; addedBy: AddedBy; at?: string; archived: boolean }[];
};
export type UndoAnswer = { revision: number; undone: number };

export type CreatePlaceAsk = { name: string; parent?: string; parents?: string[]; tint?: Tint; instructions?: string; ifRevision?: number };
export type UpdatePlaceAsk = { name?: string; tint?: Tint | ''; instructions?: string; policy?: PlacePolicy; ifRevision?: number };
export type ParentsAsk = ({ add: string } | { remove: string } | { set: string[] }) & { ifRevision?: number };

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
  return v as unknown as HomeDigest;
}
function mutation(v: unknown): Mutation {
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
  request.signal?.addEventListener('abort', () => clock.abort(), { once: true });
  init.signal = clock.signal;
  let response: Response;
  try { response = await fetch(url, init); }
  catch (error) {
    if (request.signal?.aborted) throw error;
    throw new PlacesError('codeaf engine is not running', 0, '', [], undefined, true);
  }
  finally { clearTimeout(timer); }
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
  const get = (path: string, signal?: AbortSignal) => transport(path, { method: 'GET', signal });
  const post = (path: string, body: unknown = {}, signal?: AbortSignal) => transport(path, { method: 'POST', body, signal });
  const write = async (path: string, body?: unknown) => mutation(await post(path, body));
  return {
    /** The whole graph with status roll-ups, the rail and totals. */
    graph: async (opts: { archived?: boolean; signal?: AbortSignal } = {}) => graph(await get(opts.archived ? '/places?archived=1' : '/places', opts.signal)),
    /** Roll-ups only: cheap enough to poll for rail dots. */
    status: async (signal?: AbortSignal) => status(await get('/places/status', signal)),
    rail: async (signal?: AbortSignal) => rail(await get('/places/rail', signal)),
    /** A place's Home; pass 'root' for All places and 'now' for the unplaced chats. */
    home: async (id: string, signal?: AbortSignal) => home(await get(`/places/${enc(id)}`, signal)),
    deletePreview: async (id: string): Promise<DeleteImpact> => {
      const v = await get(`/places/${enc(id)}/delete-preview`);
      if (!isObject(v) || !isInt(v.children) || !isInt(v.chatsHere) || !Array.isArray(v.wouldBeUnplaced)) return bad('delete preview');
      return v as DeleteImpact;
    },
    chatPlaces: async (chatId: string): Promise<ChatPlacesAnswer> => {
      const v = await get(`/chats/${enc(chatId)}/places`);
      if (!isObject(v) || typeof v.chatId !== 'string' || typeof v.known !== 'boolean' || !Array.isArray(v.places)) return bad('chat places answer');
      return v as ChatPlacesAnswer;
    },

    createPlace: (ask: CreatePlaceAsk) => write('/places', ask),
    updatePlace: (id: string, ask: UpdatePlaceAsk) => write(`/places/${enc(id)}`, ask),
    setParents: (id: string, ask: ParentsAsk) => write(`/places/${enc(id)}/parents`, ask),
    archivePlace: (id: string, ifRevision?: number) => write(`/places/${enc(id)}/archive`, { ifRevision }),
    restorePlace: (id: string, ifRevision?: number) => write(`/places/${enc(id)}/restore`, { ifRevision }),
    deletePlace: (id: string, ifRevision?: number) => write(`/places/${enc(id)}/delete`, { ifRevision }),
    mergePlace: (id: string, into: string, ifRevision?: number) => write(`/places/${enc(id)}/merge`, { into, ifRevision }),
    pinPlace: (id: string, index?: number) => write(`/places/${enc(id)}/pin`, { index }),
    unpinPlace: (id: string) => write(`/places/${enc(id)}/unpin`),
    addSource: (id: string, ask: { kind: SourceKind; ref: string; label?: string; ifRevision?: number }) => write(`/places/${enc(id)}/sources`, ask),
    removeSource: (id: string, sourceId: string, ifRevision?: number) => write(`/places/${enc(id)}/sources/remove`, { sourceId, ifRevision }),
    /** Files chats in a place. With moveFrom ('now' allowed) each chat is moved instead. */
    addChats: (id: string, chats: string[], opts: { addedBy?: AddedBy; moveFrom?: string; ifRevision?: number } = {}) => write(`/places/${enc(id)}/members`, { chats, ...opts }),
    removeChats: (id: string, chats: string[], ifRevision?: number) => write(`/places/${enc(id)}/members/remove`, { chats, ifRevision }),
    /** Records that the person went to a place. Moves no revision and has no receipt. */
    visit: async (id: string): Promise<void> => {
      const v = await post(`/places/${enc(id)}/visit`);
      if (!isObject(v) || v.ok !== true) bad('visit answer');
    },
    /** Takes receipts back, newest first (pass them in the order they were made). */
    undo: async (receipts: string[]): Promise<UndoAnswer> => {
      const v = await post('/places/undo', { receipts });
      if (!isObject(v) || !isInt(v.revision) || !isInt(v.undone)) return bad('undo answer');
      return v as UndoAnswer;
    },
  };
}
