// SPECIMEN FIXTURE — Playwright only. Nothing under src/ may import this module.
//
// In-memory stand-in for the engine routes a window needs before it has a
// conversation: the world stream (GET /events and GET /world), /places*, a
// conversation's Using list, workspace compare-and-swap, /settings/*, and
// background jobs. The conversation mock mounts `answerWorldRoute` inside its
// own /api/engine handler; a spec that only needs these routes calls
// `installWorldEngine` and never starts a model.
//
// The wire is the one the desktop clients validate (features/world/types.ts,
// places/client.ts, workspace-sync, settings, jobs). Rows use chatId and a
// needsYou count. A reset always carries rows, items, jobs, places and
// workspaces, because the world client clears any collection a reset omits.
//
// route.fulfill can send only one finite body, so /events behaves like the
// conversation stream: a reader behind the cursor gets the records and the body
// ends; a reader that is current is held until the fixture changes or the page
// closes. The fixed clock keeps every timestamp reproducible.

import type { Page, Route } from '@playwright/test';
import type { AttentionItem, EngineJob, JobsRollup, PlacesRecord, WorkspaceRecord, WorldFull, WorldRecord, WorldRow } from '../../../src/features/world/types';
import type { AddedBy, PlaceCounts, PlaceDetail, PlaceView, PlacesGraph, RailView, SourceKind, SourceView, StatusRollup, Tint } from '../../../src/features/places/wire';
import type { UsingView } from '../../../src/features/places/using-client';

export const WORLD_NOW = '2026-10-10T12:00:00.000Z';
export const WORLD_SCENARIOS = ['empty', 'typicalDay', 'twoHundredPlaces'] as const;
export type WorldScenarioName = (typeof WORLD_SCENARIOS)[number];

export type WorldCall = { method: string; path: string; body: Record<string, unknown> };
export type WorldCounts = { events: number; places: number; using: number; workspaces: number; settings: number; jobs: number };

export type WorldRequest = { method: string; path: string; search: URLSearchParams; body: Record<string, unknown> };
export type WorldResponse = { status: number; body?: unknown; sse?: string; abort?: boolean };

export type WorldEngine = {
  /** Every request this mock answered, in order. Paths are engine paths (`/places`), with no query. */
  calls: WorldCall[];
  /** How many requests each family answered, including its refusals. */
  counts: WorldCounts;
  scenario: WorldScenarioName;
  owns: (path: string) => boolean;
  /** Records one owned request. Invalid JSON is counted here, before dispatch. */
  hit: (method: string, path: string, body: Record<string, unknown>) => void;
  dispatch: (request: WorldRequest) => Promise<WorldResponse | undefined>;
  /** Publishes a reset of the current fixture and wakes held /events readers. */
  nudge: () => void;
  /** Stops held streams. installWorldEngine calls this when the last page closes. */
  close: () => void;
};

const EPOCH = 'mock-world';
const DAY = 86_400_000;
const RAIL_OPEN_MS = 12 * 60 * 60 * 1000;
const RAIL_OPEN_MAX = 12;
const HOME_CHATS_MAX = 100;
const ATTENTION_MAX = 50;
const JOB_LOG_CAP = 1 << 20;
const WORKSPACE_WAIT_MS = 250;
const TINTS: Tint[] = ['tide', 'iris', 'rose', 'sand', 'sage'];
const ZERO: StatusRollup = { chats: 0, running: 0, needsYou: 0, incomplete: 0, failedTasks: 0 };
const RESERVED = new Set(['now', 'root', 'undo', 'status', 'rail', 'stale', 'policy', 'proposals', 'from-folder']);
const KEY = /^(now|pl_[0-9a-f]{16})$/;
const placeKey = (n: number) => `pl_${n.toString(16).padStart(16, '0')}`;
const sessionFileFor = (chatId: string) => `/home/u/.codeaf/v3/projects/p/${chatId}/session.jsonl`;

const CODEAF = placeKey(1);
const PERSONAL = placeKey(2);
const SOFTWARE = placeKey(3);
const CONFIG = placeKey(4);
const MARKETING = placeKey(5);
const REPORTS = placeKey(6);
const Q3 = placeKey(7);
const READING = placeKey(8);
const RELEASE = placeKey(9);

/** The three organisation rows Settings already reads. A window with only this mock can still render them. */
const PLACES_POLICY = [
  { key: 'clusterOffers', group: 'offers', name: 'Offer new places', explain: 'When enough chats in no place belong together, offer to put them in a place. You approve every new place.', kind: 'switch' as const, default: true, design: true },
  { key: 'autoFile', group: 'offers', name: 'File chats without asking', explain: 'Put a chat in a place you already have when codeaf is very sure, and say so.', kind: 'switch' as const, default: false, design: false },
  { key: 'maxAiTopLevel', group: 'limits', name: 'Top-level places codeaf may create', explain: 'Places you make yourself are never limited.', kind: 'number' as const, default: 6, min: 0, max: 50, unit: 'places', design: false },
];

type StorePlace = {
  id: string; name: string; parents: string[]; tint: Tint | ''; pinned: boolean; archived: boolean;
  instructions: string; sources: SourceView[]; policy: { model?: string; permissions?: string };
  createdAt: string; lastOpenedAt?: string;
};
type StoreChat = {
  id: string; tokens: string[]; title: string; sessionFile: string; workspace?: string; project: string;
  places: string[]; running: boolean; needsYou: number; failed: number; tasksRunning: number; tasksTotal: number;
  incomplete: number; done: number; attached: boolean; archived: boolean; updatedAt?: string; doing: string;
  live: boolean; reason?: string;
};
type HeldJob = EngineJob & { log: string };
type HeldWorkspace = { key: string; revision: number; writer?: string; updatedAt?: string; workspace: unknown };
type Snap = { places: StorePlace[]; pins: string[]; chats: StoreChat[] };
type Receipt = { id: string; action: string; subject?: string; beforeRevision: number; afterRevision: number; at: string };

class Refusal extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const cleanPath = (path: string) => (path.length > 1 ? path.replace(/\/+$/, '') : path) || '/';

export function familyOf(path: string): keyof WorldCounts | undefined {
  const cleaned = cleanPath(path);
  if (cleaned === '/events' || cleaned === '/world') return 'events';
  if (cleaned === '/places' || cleaned.startsWith('/places/') || /^\/chats\/[^/]+\/places$/.test(cleaned)) return 'places';
  if (/^\/sessions\/[^/]+\/using(?:\/|$)/.test(cleaned)) return 'using';
  if (/^\/workspaces\/[^/]+$/.test(cleaned)) return 'workspaces';
  if (cleaned === '/settings' || cleaned.startsWith('/settings/')) return 'settings';
  if (/^\/sessions\/[^/]+\/jobs(?:\/|$)/.test(cleaned)) return 'jobs';
  return undefined;
}

const source = (id: string, kind: SourceKind, ref: string): SourceView => ({
  id, kind, ref, addedBy: 'you', at: WORLD_NOW, check: { state: 'ok' },
});

const chat = (id: string, title: string, extra: Partial<StoreChat> = {}): StoreChat => ({
  id, tokens: [id], title, sessionFile: sessionFileFor(id), project: '', places: [], running: false, needsYou: 0, failed: 0,
  tasksRunning: 0, tasksTotal: 0, incomplete: 0, done: 0, attached: false, archived: false, doing: '', live: false, ...extra,
});

function typicalPlaces(): StorePlace[] {
  const made = (id: string, name: string, extra: Partial<StorePlace> = {}): StorePlace => ({
    id, name, parents: [], tint: '', pinned: false, archived: false, instructions: '', sources: [], policy: {}, createdAt: '2026-10-09T12:01:00.000Z', ...extra,
  });
  return [
    made(CODEAF, 'codeaf', { tint: 'tide', pinned: true, instructions: 'Keep changes focused.', sources: [source('src_0000000000000001', 'folder', '/work/codeaf')] }),
    made(PERSONAL, 'Personal', { tint: 'sand', pinned: true }),
    made(SOFTWARE, 'Software', { parents: [CODEAF] }),
    made(CONFIG, 'Config parser', { parents: [CODEAF], lastOpenedAt: WORLD_NOW }),
    made(MARKETING, 'Marketing', { parents: [CODEAF], tint: 'rose', lastOpenedAt: '2026-10-10T11:00:00.000Z', sources: [source('src_0000000000000002', 'file', '/work/brand-voice.md')] }),
    made(REPORTS, 'Reports', { tint: 'sage' }),
    made(Q3, 'Q3 report', { parents: [REPORTS], lastOpenedAt: '2026-10-10T10:00:00.000Z' }),
    made(READING, 'Reading', { tint: 'iris' }),
    made(RELEASE, 'Release', { parents: [SOFTWARE, MARKETING] }),
  ];
}

function typicalChats(): StoreChat[] {
  return [
    chat('config-stack', 'Trailing commas across the config stack', {
      tokens: ['config-stack', 'mock-1'], places: [CONFIG, SOFTWARE], workspace: '/work/codeaf', project: 'codeaf',
      running: true, needsYou: 1, tasksRunning: 4, tasksTotal: 4, attached: true, live: true, doing: 'working',
      reason: 'Choose the rollout scope', updatedAt: '2026-10-10T11:59:00.000Z',
    }),
    chat('launch-copy', 'Launch copy', { places: [MARKETING], workspace: '/work/codeaf', project: 'codeaf', updatedAt: '2026-10-10T11:40:00.000Z' }),
    chat('q3-chat', 'Q3 report', { places: [Q3], workspace: '/work/codeaf', project: 'codeaf', updatedAt: '2026-10-10T11:20:00.000Z' }),
    ...['Pricing for teams', 'Explain Go generics constraints', 'Plan the week'].map((title, index) => chat(`loose-${index + 1}`, title, { updatedAt: new Date(Date.parse(WORLD_NOW) - (index + 1) * 60_000).toISOString() })),
  ];
}

const runningJob = (): HeldJob => ({
  id: 1, name: 'Tests', command: 'go test ./internal/placegraph', kind: 'command', state: 'running',
  startedAt: '2026-10-10T11:59:48.000Z', elapsedMs: 12_000, logPath: 'jobs/1.log', log: 'ok\tplaces\n',
});

const sharedTabs = () => ({
  schema: 1,
  tabs: [{
    id: 'tab-config', kind: 'conversation', title: 'Trailing commas across the config stack', titleSource: 'engine',
    draft: '', pinned: false, sessionFile: sessionFileFor('config-stack'),
  }],
  groups: [], closed: [], nextNumber: 2,
});

class Engine {
  readonly calls: WorldCall[] = [];
  readonly counts: WorldCounts = { events: 0, places: 0, using: 0, workspaces: 0, settings: 0, jobs: 0 };
  readonly scenario: WorldScenarioName;
  private readonly now = WORLD_NOW;
  private readonly epoch = EPOCH;
  private places: StorePlace[] = [];
  private pins: string[] = [];
  private chats: StoreChat[] = [];
  private jobs = new Map<string, HeldJob[]>();
  private workspaces = new Map<string, HeldWorkspace>();
  private revision = 0;
  private seq = 1;
  private ring: WorldRecord[] = [];
  private history: { receipt: Receipt; before: Snap }[] = [];
  private receiptN = 0;
  private nextPlace = 0x200;
  private usingOverrides = new Map<string, UsingView>();
  private policyChosen: Record<string, boolean | number> = {};
  private permissions = { mode: 'prompt', modes: ['prompt', 'allow', 'deny'] };
  private keyPresent = false;
  private model: string | undefined;
  private closed = false;
  private waiters: (() => void)[] = [];

  constructor(name: WorldScenarioName) {
    this.scenario = name;
    if (name !== 'empty') {
      this.places = typicalPlaces();
      this.pins = [CODEAF, PERSONAL];
      this.chats = typicalChats();
      this.jobs.set('config-stack', [runningJob()]);
      this.workspaces.set('now', { key: 'now', revision: 1, writer: 'win-a', updatedAt: WORLD_NOW, workspace: sharedTabs() });
      this.revision = 1;
      this.keyPresent = true;
      this.model = 'deepseek/deepseek-v4.1-flash';
    }
    if (name === 'twoHundredPlaces') this.addScale();
    this.ring = [{ epoch: this.epoch, seq: 1, type: 'reset', at: this.now, payload: structuredClone(this.full()) }];
  }

  owns(path: string) { return familyOf(path) !== undefined; }

  hit(method: string, path: string, body: Record<string, unknown>) {
    const family = familyOf(path);
    if (!family) return;
    this.counts[family] += 1;
    this.calls.push({ method, path: cleanPath(path), body });
  }

  close() { this.closed = true; this.wake(); }

  nudge() { this.push('reset', this.full()); }

  async dispatch(request: WorldRequest): Promise<WorldResponse | undefined> {
    const path = cleanPath(request.path);
    const family = familyOf(path);
    if (!family) return undefined;
    this.hit(request.method, path, request.body);
    if (family === 'events') return this.events(request);
    if (family === 'places') return this.placesRoute(request, path);
    if (family === 'using') return this.usingRoute(request, path);
    if (family === 'workspaces') return this.workspaceRoute(request, path);
    if (family === 'settings') return this.settingsRoute(request, path);
    return this.jobsRoute(request, path);
  }

  private addScale() {
    // Q3 is already a child of Reports. These 46 siblings make 47, then enough
    // projects under Software and Personal to reach 200 places and a depth of 3.
    const extras = Array.from({ length: 46 }, (_, index) => ({
      id: placeKey(0x20 + index),
      name: index === 0 ? 'Q2 report' : index === 1 ? 'Churn deep-dive' : `Report ${index + 1}`,
      parents: [REPORTS],
    }));
    const scaleCount = 200 - (this.places.length + extras.length);
    const scale = Array.from({ length: scaleCount }, (_, index) => ({
      id: placeKey(0x80 + index),
      name: `Project ${String(index + 1).padStart(3, '0')}`,
      parents: [index % 2 ? SOFTWARE : PERSONAL],
    }));
    for (const row of [...extras, ...scale]) {
      this.places.push({
        id: row.id, name: row.name, parents: row.parents, tint: '', pinned: false, archived: false,
        instructions: '', sources: [], policy: {}, createdAt: '2026-10-01T12:00:00.000Z',
      });
    }
  }

  private wake() {
    const pending = this.waiters;
    this.waiters = [];
    pending.forEach(fn => fn());
  }

  private pause(ms: number): Promise<void> {
    return new Promise(resolve => {
      let waiter: () => void = () => {};
      const finish = () => {
        clearTimeout(timer);
        this.waiters = this.waiters.filter(item => item !== waiter);
        resolve();
      };
      const timer = setTimeout(finish, ms);
      waiter = finish;
      this.waiters.push(waiter);
    });
  }

  private push(type: WorldRecord['type'], payload: WorldRecord['payload']) {
    this.seq += 1;
    this.ring.push({ epoch: this.epoch, seq: this.seq, type, at: this.now, payload: structuredClone(payload) });
    if (this.ring.length > 64) this.ring.splice(0, this.ring.length - 64);
    this.wake();
  }

  private full(): WorldFull {
    return { rows: this.rows(), items: this.attention(), jobs: this.rollups(), places: this.placesRecord(), workspaces: this.workspaceNotices() };
  }

  private rows(): WorldRow[] {
    return this.chats.map(row => {
      const out: WorldRow = {
        chatId: row.id, running: row.running, needsYou: row.needsYou, failed: row.failed,
        tasksRunning: row.tasksRunning, tasksTotal: row.tasksTotal, attached: row.attached, archived: row.archived,
      };
      if (row.sessionFile) out.sessionFile = row.sessionFile;
      if (row.title) out.title = row.title;
      if (row.workspace) out.workspace = row.workspace;
      if (row.updatedAt) out.updatedAt = row.updatedAt;
      return out;
    }).sort((a, b) => a.chatId.localeCompare(b.chatId));
  }

  private attention(): AttentionItem[] {
    const items: AttentionItem[] = [];
    for (const row of this.chats) {
      if (row.archived || (row.needsYou <= 0 && row.failed <= 0)) continue;
      const item: AttentionItem = { id: `q-${row.id}`, chatId: row.id, kind: row.needsYou > 0 ? 'question' : 'failed', blocking: row.needsYou > 0 };
      if (row.sessionFile) item.sessionFile = row.sessionFile;
      if (row.attached) item.sessionId = row.tokens.find(token => token !== row.id) ?? row.id;
      if (row.title) item.title = row.title;
      if (row.needsYou > 0 && row.reason) item.head = row.reason;
      if (row.updatedAt) item.at = row.updatedAt;
      items.push(item);
    }
    return items;
  }

  private rollups(): JobsRollup[] {
    return [...this.jobs.entries()].map(([chatId, jobs]) => ({
      chatId, running: jobs.filter(job => job.state === 'running').length, jobs: jobs.map(job => this.jobWire(job)),
    })).sort((a, b) => a.chatId.localeCompare(b.chatId));
  }

  private jobWire(job: HeldJob): EngineJob {
    const { log: _log, ...wire } = job;
    return wire;
  }

  private workspaceNotices(): WorkspaceRecord[] {
    const notices: WorkspaceRecord[] = [];
    const now = this.workspaces.get('now');
    notices.push({ key: 'now', revision: now?.revision ?? 0, ...(now?.writer ? { writer: now.writer } : {}) });
    for (const [key, held] of [...this.workspaces.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
      if (key === 'now') continue;
      notices.push({ key, revision: held.revision, ...(held.writer ? { writer: held.writer } : {}) });
    }
    return notices;
  }

  private placesRecord(): PlacesRecord {
    return {
      generation: this.revision,
      nodes: this.places.filter(place => !place.archived).map(place => this.placeView(place)),
      rail: this.rail(),
      members: this.chats.flatMap(row => row.places.map(placeId => ({ chatId: row.id, placeId, addedBy: 'you' as AddedBy, at: row.updatedAt ?? this.now }))),
    };
  }

  private async events(request: WorldRequest): Promise<WorldResponse> {
    if (request.method !== 'GET') return { status: 405, body: { error: 'GET required' } };
    if (cleanPath(request.path) === '/world') return { status: 200, body: { seq: this.seq, ...this.full() } };
    const raw = request.search.get('after');
    if (raw !== null && raw !== '' && !/^\d+$/.test(raw)) return { status: 400, body: { error: 'after must be a sequence number' } };
    const after = raw ? Number(raw) : 0;
    const epoch = request.search.get('epoch');
    const deadline = Date.now() + 60_000;
    while (!this.closed && Date.now() < deadline) {
      const batch = this.take(after, epoch);
      if (batch) return { status: 200, sse: this.sse(batch) };
      await this.pause(25);
    }
    return { abort: true };
  }

  /** after=0, a foreign epoch, or a gap the ring no longer holds is one reset of the live state. */
  private take(after: number, epoch: string | null): WorldRecord[] | undefined {
    const mismatch = epoch !== null && epoch !== '' && epoch !== this.epoch;
    if (after === 0 || after > this.seq || mismatch || (this.ring.length > 0 && after + 1 < this.ring[0].seq)) {
      return [{ epoch: this.epoch, seq: this.seq, type: 'reset', at: this.now, payload: structuredClone(this.full()) }];
    }
    const fresh = this.ring.filter(record => record.seq > after);
    return fresh.length ? fresh : undefined;
  }

  private sse(records: WorldRecord[]) {
    return `: connected\n\n${records.map(record => `id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n`).join('')}`;
  }

  private placesRoute(request: WorldRequest, path: string): WorldResponse {
    try {
      const parts = path.split('/').filter(Boolean).map(part => { try { return decodeURIComponent(part); } catch { return part; } });
      if (parts[0] === 'chats') return this.chatPlaces(parts[1] ?? '');
      if (parts[1] === 'policy') return this.policy(request, parts[2]);
      if (parts[1] === 'proposals') return this.proposals(request);
      return this.placeDispatch(request, parts.slice(1));
    } catch (error) {
      if (error instanceof Refusal) return { status: error.status, body: { error: error.message, code: error.code } };
      throw error;
    }
  }

  private placeDispatch(request: WorldRequest, parts: string[]): WorldResponse {
    const method = request.method;
    const get = method === 'GET';
    const post = method === 'POST';
    if (parts.length === 0) {
      if (get) return { status: 200, body: this.graph(request.search.get('archived') === '1') };
      if (post) return this.create(request.body);
      return { status: 405, body: { error: 'GET or POST required' } };
    }
    if (parts.length === 1) {
      switch (parts[0]) {
        case 'status': return get ? { status: 200, body: this.status() } : { status: 405, body: { error: 'GET required' } };
        case 'rail': return post ? this.railOp(request.body) : get ? { status: 200, body: this.rail() } : { status: 405, body: { error: 'GET required' } };
        case 'undo': return post ? this.undo(request.body) : { status: 405, body: { error: 'POST required' } };
        case 'from-folder': return post ? this.fromFolder(request.body) : { status: 405, body: { error: 'POST required' } };
        case 'stale': return get ? { status: 200, body: { revision: this.revision, readAt: this.now, afterDays: 60, snoozeDays: 30, places: [] } } : { status: 405, body: { error: 'GET required' } };
        case 'root':
        case 'now': return get ? { status: 200, body: this.digest(parts[0]) } : this.reserved();
        default: break;
      }
      if (get) return { status: 200, body: this.digest(parts[0]) };
      if (post) return this.update(parts[0], request.body);
      return { status: 405, body: { error: 'GET or POST required' } };
    }
    const [id, verb, extra] = parts;
    if (verb === 'home' && get && parts.length === 2) return { status: 200, body: this.homeView(id) };
    if (verb === 'delete-preview' && get) return { status: 200, body: this.deletePreview(id) };
    if (verb === 'effective-model' && get) return { status: 200, body: this.effectiveModel(id) };
    if (verb === 'impact' && get) return { status: 200, body: this.impact(id) };
    if (verb === 'stale-snooze' && post) return this.snooze(id);
    if (verb === 'members' && extra === 'remove') return post ? this.removeMembers(id, request.body) : { status: 405, body: { error: 'POST required' } };
    if (verb === 'sources' && extra === 'remove') return post ? this.removeSource(id, request.body) : { status: 405, body: { error: 'POST required' } };
    if (!post) return { status: 405, body: { error: 'POST required' } };
    switch (verb) {
      case 'parents': return this.parents(id, request.body);
      case 'archive': return this.archive(id, true, request.body);
      case 'restore': return this.archive(id, false, request.body);
      case 'delete': return this.removePlace(id, request.body);
      case 'merge': return this.merge(id, request.body);
      case 'pin': return this.pin(id, true, request.body);
      case 'unpin': return this.pin(id, false, request.body);
      case 'visit': return this.visit(id);
      case 'sources': return this.addSource(id, request.body);
      case 'members': return this.addMembers(id, request.body);
      default: return { status: 404, body: { error: 'unknown engine action' } };
    }
  }

  private reserved(): WorldResponse {
    return { status: 400, body: { error: "All places and Now are built in; they can't be changed this way.", code: 'reserved' } };
  }

  private guard(body: Record<string, unknown>) {
    const stamp = body.ifGeneration ?? body.ifRevision;
    if (stamp !== undefined && stamp !== this.revision) throw new Refusal(409, 'stale', 'The places changed. Look again and retry.');
  }

  private requirePlace(id: string): StorePlace {
    if (RESERVED.has(id)) throw new Refusal(400, 'reserved', "All places and Now are built in; they can't be changed this way.");
    const found = this.places.find(place => place.id === id);
    if (!found) throw new Refusal(404, 'not_found', "That place doesn't exist any more.");
    return found;
  }

  private children(id: string) { return this.places.filter(place => !place.archived && place.parents.includes(id)); }

  private descends(ancestor: string, id: string, seen = new Set<string>()): boolean {
    if (seen.has(ancestor)) return false;
    seen.add(ancestor);
    return this.children(ancestor).some(child => child.id === id || this.descends(child.id, id, seen));
  }

  private descendantIds(id: string, seen = new Set<string>()): string[] {
    const out: string[] = [];
    for (const child of this.children(id)) {
      if (seen.has(child.id)) continue;
      seen.add(child.id);
      out.push(child.id, ...this.descendantIds(child.id, seen));
    }
    return out;
  }

  private chatsIn(ids: Set<string>) {
    const seen = new Set<string>();
    return this.chats.filter(row => {
      if (row.archived || seen.has(row.id) || !row.places.some(placeId => ids.has(placeId))) return false;
      seen.add(row.id);
      return true;
    });
  }

  private directChats(id: string) { return this.chats.filter(row => !row.archived && row.places.includes(id)); }

  private rollup(rows: StoreChat[]): StatusRollup {
    return {
      chats: rows.length,
      running: rows.filter(row => row.running).length,
      needsYou: rows.filter(row => row.needsYou > 0).length,
      incomplete: rows.reduce((sum, row) => sum + row.incomplete, 0),
      failedTasks: rows.reduce((sum, row) => sum + row.failed, 0),
    };
  }

  private effectiveTint(place: StorePlace, seen = new Set<string>()): Tint {
    if (place.tint) return place.tint;
    if (seen.has(place.id)) return 'graphite';
    seen.add(place.id);
    const parent = this.places.find(row => row.id === place.parents[0]);
    return parent ? this.effectiveTint(parent, seen) : 'graphite';
  }

  private countsOf(place: StorePlace): PlaceCounts {
    const kids = this.children(place.id);
    const below = this.descendantIds(place.id);
    return {
      children: kids.length,
      descendants: below.length,
      chats: this.directChats(place.id).length,
      chatsInclusive: this.chatsIn(new Set([place.id, ...below])).length,
    };
  }

  private placeView(place: StorePlace): PlaceView {
    const below = this.descendantIds(place.id);
    const view: PlaceView = {
      id: place.id, name: place.name, parents: [...place.parents], tint: place.tint, effectiveTint: this.effectiveTint(place),
      archived: place.archived, pinned: this.pins.includes(place.id), createdAt: place.createdAt,
      counts: this.countsOf(place),
      status: this.rollup(this.directChats(place.id)),
      statusInclusive: this.rollup(this.chatsIn(new Set([place.id, ...below]))),
      hasInstructions: place.instructions.length > 0, sourceCount: place.sources.length,
      alsoIn: place.parents.slice(1).flatMap(id => {
        const parent = this.places.find(row => row.id === id);
        return parent ? [{ id, name: parent.name }] : [];
      }),
    };
    if (place.lastOpenedAt) view.lastOpenedAt = place.lastOpenedAt;
    return view;
  }

  private detail(place: StorePlace): PlaceDetail {
    return { ...this.placeView(place), instructions: place.instructions, sources: place.sources.map(row => ({ ...row })), policy: { ...place.policy } };
  }

  private rail(): RailView {
    const opened = Date.parse(this.now) - RAIL_OPEN_MS;
    const pinned = this.pins.flatMap(id => {
      const place = this.places.find(row => row.id === id && !row.archived);
      return place ? [this.placeView(place)] : [];
    });
    const open = this.places
      .filter(place => !place.archived && !this.pins.includes(place.id) && place.lastOpenedAt && Date.parse(place.lastOpenedAt) >= opened)
      .sort((a, b) => Date.parse(b.lastOpenedAt!) - Date.parse(a.lastOpenedAt!))
      .slice(0, RAIL_OPEN_MAX)
      .map(place => this.placeView(place));
    return { pinned, open, openWindowHours: 12 };
  }

  private graph(archived: boolean): PlacesGraph {
    const listed = this.places.filter(place => archived || !place.archived);
    const activeChats = this.chats.filter(row => !row.archived);
    const unplaced = activeChats.filter(row => row.places.length === 0);
    return {
      revision: this.revision, generation: this.revision, nodes: listed.map(place => this.placeView(place)),
      unplaced: unplaced.map(row => row.id), places: listed.map(place => this.placeView(place)), rail: this.rail(),
      now: { chats: unplaced.length, status: this.rollup(unplaced) },
      totals: {
        places: this.places.filter(place => !place.archived).length,
        placed: activeChats.filter(row => row.places.length > 0).length,
        unplaced: unplaced.length,
        running: activeChats.filter(row => row.running).length,
        needsYou: activeChats.filter(row => row.needsYou > 0).length,
        missingChats: 0,
      },
      readAt: this.now,
    };
  }

  private status() {
    const places: Record<string, { status: StatusRollup; statusInclusive: StatusRollup }> = {};
    for (const place of this.places.filter(row => !row.archived)) {
      const below = this.descendantIds(place.id);
      places[place.id] = { status: this.rollup(this.directChats(place.id)), statusInclusive: this.rollup(this.chatsIn(new Set([place.id, ...below]))) };
    }
    const active = this.chats.filter(row => !row.archived);
    const unplaced = active.filter(row => row.places.length === 0);
    return {
      revision: this.revision, readAt: this.now, places,
      now: { chats: unplaced.length, status: this.rollup(unplaced) },
      totals: this.graph(false).totals,
    };
  }

  private chatRow(row: StoreChat) {
    return {
      id: row.id, title: row.title, project: row.project, workspace: row.workspace ?? '', sessionFile: row.sessionFile,
      ...(row.updatedAt ? { at: row.updatedAt } : {}), archived: row.archived, live: row.live, doing: row.doing,
      needsYou: row.needsYou > 0, ...(row.reason ? { reason: row.reason } : {}),
      tasks: { running: row.tasksRunning, incomplete: row.incomplete, done: row.done, failed: row.failed },
      places: row.places.flatMap(id => {
        const place = this.places.find(item => item.id === id);
        return place ? [{ id, name: place.name, tint: this.effectiveTint(place), addedBy: 'you' as AddedBy }] : [];
      }),
    };
  }

  private attentionRows(rows: StoreChat[], placeId?: string) {
    const items = [];
    for (const row of rows) {
      if (row.needsYou <= 0 && !row.running) continue;
      const place = placeId ? this.places.find(item => item.id === placeId) : this.places.find(item => row.places.includes(item.id));
      items.push({
        kind: row.needsYou > 0 ? 'needsYou' as const : 'running' as const,
        chatId: row.id, chatTitle: row.title, placeId: place?.id ?? placeId ?? 'root', placeName: place?.name ?? 'All places',
        text: row.needsYou > 0 ? (row.reason ?? row.title) : row.title,
        ...(row.running && row.updatedAt ? { since: row.updatedAt } : {}),
      });
      if (items.length >= ATTENTION_MAX) break;
    }
    return items;
  }

  private digest(id: string) {
    if (id === 'root' || id === 'now') return this.specialDigest(id);
    const place = this.requirePlace(id);
    const rows = this.directChats(id).slice(0, HOME_CHATS_MAX);
    const below = this.descendantIds(id);
    const body: Record<string, unknown> = {
      kind: 'place', title: place.name, place: this.detail(place), breadcrumb: this.crumbs(place),
      children: this.children(id).map(child => this.placeView(child)),
      chats: rows.map(row => this.chatRow(row)), chatsTruncated: this.directChats(id).length > HOME_CHATS_MAX,
      attention: this.attentionRows(this.directChats(id), id), status: this.rollup(this.chatsIn(new Set([id, ...below]))),
      counts: this.countsOf(place), missingChats: 0, revision: this.revision, readAt: this.now,
    };
    const line = this.contextLine(place);
    if (line) body.contextLine = line;
    return body;
  }

  private specialDigest(id: 'root' | 'now') {
    const rows = id === 'now' ? this.chats.filter(row => !row.archived && row.places.length === 0) : this.chats.filter(row => !row.archived);
    const children = id === 'root' ? this.places.filter(place => !place.archived && place.parents.length === 0) : [];
    return {
      kind: id, title: id === 'root' ? 'All places' : 'Now', breadcrumb: [],
      children: children.map(place => this.placeView(place)),
      chats: rows.slice(0, HOME_CHATS_MAX).map(row => this.chatRow(row)), chatsTruncated: rows.length > HOME_CHATS_MAX,
      attention: this.attentionRows(rows), status: this.rollup(rows),
      counts: {
        children: children.length,
        descendants: id === 'root' ? this.places.filter(place => !place.archived).length : 0,
        chats: id === 'now' ? rows.length : 0,
        chatsInclusive: rows.length,
      },
      missingChats: 0, revision: this.revision, readAt: this.now,
    };
  }

  private crumbs(place: StorePlace) {
    const chain: StorePlace[] = [];
    let current: StorePlace | undefined = place;
    const seen = new Set<string>();
    while (current?.parents[0] && !seen.has(current.parents[0])) {
      seen.add(current.parents[0]);
      const parent = this.places.find(row => row.id === current!.parents[0]);
      if (!parent) break;
      chain.unshift(parent);
      current = parent;
    }
    return chain.map(row => ({ id: row.id, name: row.name, tint: this.effectiveTint(row) }));
  }

  private contextLine(place: StorePlace): string | undefined {
    const parts: string[] = [];
    if (place.instructions) parts.push('Instructions');
    if (place.sources.length === 1) parts.push('1 source');
    else if (place.sources.length > 1) parts.push(`${place.sources.length} sources`);
    const missing = place.sources.filter(row => row.check.state === 'missing').length;
    if (missing === 1) parts.push('1 missing');
    else if (missing > 1) parts.push(`${missing} missing`);
    return parts.length ? parts.join(' · ') : undefined;
  }

  private homeView(id: string) {
    if (id === 'root') {
      const unplaced = this.chats.filter(row => !row.archived && row.places.length === 0).map(row => this.homeChat(row));
      return {
        breadcrumb: [], attention: [], children: this.places.filter(place => !place.archived && place.parents.length === 0).map(place => this.homeChild(place)),
        ...(unplaced.length ? { unplaced } : {}),
      };
    }
    if (id === 'now') {
      return { breadcrumb: [], attention: [], children: [], chats: this.chats.filter(row => !row.archived && row.places.length === 0).map(row => this.homeChat(row)) };
    }
    const place = this.requirePlace(id);
    return {
      place: this.detail(place), breadcrumb: this.crumbs(place),
      attention: this.directChats(id).filter(row => row.needsYou > 0 || row.running).map(row => ({
        chatId: row.id, title: row.title, originPlaceId: id, state: row.needsYou > 0 ? 'needsYou' : 'running', ...(row.updatedAt ? { startedAt: row.updatedAt } : {}),
      })),
      children: this.children(id).map(child => this.homeChild(child)),
      chats: this.directChats(id).map(row => this.homeChat(row)),
    };
  }

  private homeChat(row: StoreChat) {
    return { chatId: row.id, ...(row.sessionFile ? { sessionFile: row.sessionFile } : {}), title: row.title, ...(row.updatedAt ? { at: row.updatedAt } : {}), ...(row.tasksRunning ? { tasksRunning: row.tasksRunning } : {}) };
  }

  private homeChild(place: StorePlace) {
    const inclusive = this.rollup(this.chatsIn(new Set([place.id, ...this.descendantIds(place.id)])));
    const child: Record<string, unknown> = { id: place.id, name: place.name, effectiveTint: this.effectiveTint(place) };
    if (inclusive.needsYou) child.needsYou = inclusive.needsYou;
    if (inclusive.failedTasks) child.failed = inclusive.failedTasks;
    if (inclusive.chats) child.chats = inclusive.chats;
    const kids = this.children(place.id).length;
    if (kids) child.childPlaces = kids;
    const also = place.parents.slice(1).flatMap(parentId => {
      const name = this.places.find(row => row.id === parentId)?.name;
      return name ? [name] : [];
    });
    if (also.length) child.alsoIn = also;
    return child;
  }

  private deletePreview(id: string) {
    const direct = this.directChats(id);
    return {
      children: this.children(id).length,
      chatsHere: direct.length,
      wouldBeUnplaced: direct.filter(row => row.places.filter(placeId => this.places.some(place => place.id === placeId && !place.archived)).length === 1).map(row => row.id),
    };
  }

  private effectiveModel(id: string) {
    const place = this.requirePlace(id);
    if (!place.policy.model) return { placeId: id, revision: this.revision, state: 'none' };
    return { placeId: id, revision: this.revision, state: 'applies', model: place.policy.model, outcome: 'decided', decidedBy: { id, name: place.name, model: place.policy.model } };
  }

  private impact(id: string) {
    this.requirePlace(id);
    return { chats: this.directChats(id).length, children: this.children(id).length };
  }

  private chatPlaces(id: string): WorldResponse {
    const row = this.chats.find(chat => chat.id === id || chat.tokens.includes(id));
    if (!row) return { status: 200, body: { chatId: id, known: false, places: [] } };
    return {
      status: 200,
      body: {
        chatId: row.id, known: true, ...(row.workspace ? { workspace: row.workspace } : {}), sessionFile: row.sessionFile,
        places: row.places.flatMap(placeId => {
          const place = this.places.find(item => item.id === placeId);
          return place ? [{ id: place.id, name: place.name, tint: this.effectiveTint(place), addedBy: 'you' as const, archived: place.archived }] : [];
        }),
      },
    };
  }

  private commit(action: string, subject: string | undefined, body: Record<string, unknown>, mutate: () => Record<string, unknown> | void): WorldResponse {
    this.guard(body);
    const before = structuredClone({ places: this.places, pins: this.pins, chats: this.chats });
    const beforeRevision = this.revision;
    const extra = mutate() ?? {};
    this.revision += 1;
    const receipt: Receipt = { id: `rcpt_${(++this.receiptN).toString(16).padStart(16, '0')}`, action, ...(subject ? { subject } : {}), beforeRevision, afterRevision: this.revision, at: this.now };
    this.history.push({ receipt, before });
    this.usingOverrides.clear();
    this.push('places', this.placesRecord());
    return { status: 200, body: { revision: this.revision, generation: this.revision, receipt, receipts: [receipt], noop: false, undo: [receipt.id], ...extra } };
  }

  private create(body: Record<string, unknown>): WorldResponse {
    const name = String(body.name ?? '').trim();
    if (!name) throw new Refusal(400, 'invalid', 'A place needs a name.');
    const parents = Array.isArray(body.parents) ? body.parents.map(String) : body.parent ? [String(body.parent)] : [];
    for (const parent of parents) {
      const found = this.requirePlace(parent);
      if (found.archived) throw new Refusal(409, 'archived', 'That place is archived. Restore it first.');
    }
    if (this.nameTaken(name, parents)) throw new Refusal(409, 'name_taken', 'Another place here already has that name.');
    const tint = this.readTint(body.tint, parents.length === 0);
    return this.commit('create', undefined, body, () => {
      const id = placeKey(this.nextPlace++);
      const place: StorePlace = { id, name, parents, tint, pinned: false, archived: false, instructions: typeof body.instructions === 'string' ? body.instructions : '', sources: [], policy: {}, createdAt: this.now };
      this.places.push(place);
      return { place: this.detail(place) };
    });
  }

  private update(id: string, body: Record<string, unknown>): WorldResponse {
    const place = this.requirePlace(id);
    if (place.archived) throw new Refusal(409, 'archived', 'That place is archived. Restore it first.');
    return this.commit('update', id, body, () => {
      if (typeof body.name === 'string') {
        const name = body.name.trim();
        if (!name) throw new Refusal(400, 'invalid', 'A place needs a name.');
        if (this.nameTaken(name, place.parents, id)) throw new Refusal(409, 'name_taken', 'Another place here already has that name.');
        place.name = name;
      }
      if ('tint' in body) place.tint = this.readTint(body.tint, false);
      if (typeof body.instructions === 'string') place.instructions = body.instructions;
      if (body.policy && typeof body.policy === 'object') {
        const policy = body.policy as { model?: unknown; permissions?: unknown };
        if (typeof policy.model === 'string') place.policy.model = policy.model;
        if (typeof policy.permissions === 'string') place.policy.permissions = policy.permissions;
      }
      return { place: this.detail(place) };
    });
  }

  private readTint(value: unknown, top: boolean): Tint | '' {
    if (value === undefined || value === '') return top ? TINTS[this.places.filter(place => place.parents.length === 0 && !place.archived).length % TINTS.length] : '';
    if (typeof value === 'string' && (TINTS as string[]).includes(value) || value === 'graphite') return value as Tint;
    throw new Refusal(400, 'invalid', 'That colour is not one a place can wear.');
  }

  private nameTaken(name: string, parents: string[], except?: string) {
    const folded = name.trim().toLowerCase();
    const key = [...parents].sort().join('\0');
    return this.places.some(place => !place.archived && place.id !== except && [...place.parents].sort().join('\0') === key && place.name.trim().toLowerCase() === folded);
  }

  private parents(id: string, body: Record<string, unknown>): WorldResponse {
    const place = this.requirePlace(id);
    return this.commit('parents', id, body, () => {
      let next = [...place.parents];
      if (Array.isArray(body.set)) next = body.set.map(String);
      else if (typeof body.add === 'string') next = [...next, body.add];
      else if (typeof body.remove === 'string') next = next.filter(parent => parent !== body.remove);
      for (const parent of next) {
        if (parent === id || this.descends(id, parent)) throw new Refusal(409, 'cycle', 'That would put a place inside itself.');
        const found = this.requirePlace(parent);
        if (found.archived) throw new Refusal(409, 'archived', 'That place is archived. Restore it first.');
      }
      place.parents = next;
      return { place: this.detail(place) };
    });
  }

  private archive(id: string, archived: boolean, body: Record<string, unknown>): WorldResponse {
    const place = this.requirePlace(id);
    if (place.archived === archived) throw new Refusal(409, archived ? 'archived' : 'not_archived', archived ? 'That place is archived. Restore it first.' : "That place isn't archived.");
    return this.commit(archived ? 'archive' : 'restore', id, body, () => {
      place.archived = archived;
      if (archived) this.pins = this.pins.filter(pin => pin !== id);
      return { place: this.detail(place) };
    });
  }

  private removePlace(id: string, body: Record<string, unknown>): WorldResponse {
    this.requirePlace(id);
    return this.commit('delete', id, body, () => {
      const place = this.requirePlace(id);
      for (const child of this.places.filter(row => row.parents.includes(id))) child.parents = [...new Set([...child.parents.filter(parent => parent !== id), ...place.parents])];
      for (const row of this.chats) row.places = row.places.filter(placeId => placeId !== id);
      this.pins = this.pins.filter(pin => pin !== id);
      this.places = this.places.filter(row => row.id !== id);
      return {};
    });
  }

  private merge(id: string, body: Record<string, unknown>): WorldResponse {
    const into = String(body.into ?? '');
    if (into === id || this.descends(id, into)) throw new Refusal(409, 'cycle', 'That would put a place inside itself.');
    this.requirePlace(into);
    return this.commit('merge', id, body, () => {
      for (const child of this.places.filter(row => row.parents.includes(id))) child.parents = child.parents.map(parent => parent === id ? into : parent);
      for (const row of this.chats) if (row.places.includes(id)) row.places = [...new Set(row.places.map(placeId => placeId === id ? into : placeId))];
      this.pins = this.pins.filter(pin => pin !== id);
      this.places = this.places.filter(row => row.id !== id);
      return {};
    });
  }

  private pin(id: string, pinned: boolean, body: Record<string, unknown>): WorldResponse {
    const place = this.requirePlace(id);
    if (place.archived) throw new Refusal(409, 'archived', 'That place is archived. Restore it first.');
    return this.commit(pinned ? 'pin' : 'unpin', id, body, () => {
      this.pins = this.pins.filter(pin => pin !== id);
      if (pinned) {
        const index = typeof body.index === 'number' ? Math.max(0, Math.min(this.pins.length, body.index)) : this.pins.length;
        this.pins.splice(index, 0, id);
      }
      return { place: this.detail(place) };
    });
  }

  private visit(id: string): WorldResponse {
    const place = this.requirePlace(id);
    place.lastOpenedAt = this.now;
    // A visit moves no revision. The rail record still goes out at the same generation,
    // which is the soft rail change the places client applies once.
    this.push('places', this.placesRecord());
    return { status: 200, body: { ok: true } };
  }

  private railOp(body: Record<string, unknown>): WorldResponse {
    const op = String(body.op ?? '');
    const id = typeof body.place === 'string' ? body.place : '';
    if (op === 'reorder' && Array.isArray(body.order)) {
      return this.commit('reorder', undefined, body, () => { this.pins = body.order!.map(String).filter(pin => this.places.some(place => place.id === pin)); return { rail: this.rail() }; });
    }
    if (!id) throw new Refusal(400, 'invalid', 'A place is required.');
    if (op === 'pin') return this.pin(id, true, body);
    if (op === 'unpin') return this.pin(id, false, body);
    if (op === 'visit' || op === 'close') {
      return this.commit(op, id, body, () => {
        const place = this.requirePlace(id);
        if (op === 'visit') place.lastOpenedAt = this.now;
        else delete place.lastOpenedAt;
        return { rail: this.rail() };
      });
    }
    return { status: 404, body: { error: 'unknown engine action' } };
  }

  private addSource(id: string, body: Record<string, unknown>): WorldResponse {
    const place = this.requirePlace(id);
    const kind = String(body.kind ?? '');
    const ref = String(body.ref ?? '');
    if (!kind || !ref) throw new Refusal(400, 'invalid', 'A source needs a kind and a path.');
    return this.commit('source', id, body, () => {
      place.sources.push(source(`src_${(++this.receiptN).toString(16).padStart(16, '0')}`, kind as SourceKind, ref));
      return { place: this.detail(place) };
    });
  }

  private removeSource(id: string, body: Record<string, unknown>): WorldResponse {
    const place = this.requirePlace(id);
    if (!place.sources.some(row => row.id === body.sourceId)) throw new Refusal(404, 'not_found', "That source isn't in this place any more.");
    return this.commit('source-remove', id, body, () => {
      place.sources = place.sources.filter(row => row.id !== body.sourceId);
      return { place: this.detail(place) };
    });
  }

  private addMembers(id: string, body: Record<string, unknown>): WorldResponse {
    this.requirePlace(id);
    const ids = Array.isArray(body.chats) ? body.chats.map(String) : [];
    const rows = ids.map(chatId => {
      const row = this.chats.find(chat => chat.id === chatId || chat.tokens.includes(chatId));
      if (!row) throw new Refusal(404, 'unknown_chat', "That conversation isn't saved on this machine, so it can't be filed yet.");
      return row;
    });
    return this.commit('members', id, body, () => {
      let added = 0;
      for (const row of rows) {
        if (typeof body.moveFrom === 'string') row.places = row.places.filter(placeId => placeId !== body.moveFrom);
        if (!row.places.includes(id)) { row.places.push(id); added += 1; }
      }
      return { chats: added };
    });
  }

  private removeMembers(id: string, body: Record<string, unknown>): WorldResponse {
    this.requirePlace(id);
    const ids = new Set(Array.isArray(body.chats) ? body.chats.map(String) : []);
    return this.commit('members-remove', id, body, () => {
      let removed = 0;
      for (const row of this.chats) if (ids.has(row.id) && row.places.includes(id)) { row.places = row.places.filter(placeId => placeId !== id); removed += 1; }
      return { chats: removed };
    });
  }

  private undo(body: Record<string, unknown>): WorldResponse {
    this.guard(body);
    const ids = Array.isArray(body.receipts) ? body.receipts.map(String) : [];
    let undone = 0;
    for (const id of ids) {
      const last = this.history[this.history.length - 1];
      if (!last || last.receipt.id !== id) {
        if (undone === 0) throw new Refusal(409, 'cannot_undo', "That can't be undone now because the places changed afterwards.");
        break;
      }
      const entry = this.history.pop()!;
      this.places = entry.before.places;
      this.pins = entry.before.pins;
      this.chats = entry.before.chats;
      undone += 1;
    }
    if (undone) {
      this.revision += 1;
      this.usingOverrides.clear();
      this.push('places', this.placesRecord());
    }
    return { status: 200, body: { revision: this.revision, undone } };
  }

  private fromFolder(body: Record<string, unknown>): WorldResponse {
    this.guard(body);
    const path = String(body.path ?? '');
    const place = this.places.find(row => row.sources.some(item => item.ref === path));
    if (!place) return { status: 400, body: { error: 'That folder is not on this machine.', code: 'invalid' } };
    return { status: 200, body: { revision: this.revision, generation: this.revision, receipts: [], noop: true, undo: [], place: this.detail(place) } };
  }

  private snooze(id: string): WorldResponse {
    this.requirePlace(id);
    return { status: 200, body: { ok: true, placeId: id, until: new Date(Date.parse(this.now) + 30 * DAY).toISOString() } };
  }

  private policy(request: WorldRequest, key: string | undefined): WorldResponse {
    const view = (row: typeof PLACES_POLICY[number]) => ({ ...row, value: this.policyChosen[row.key] ?? row.default, chosen: row.key in this.policyChosen && this.policyChosen[row.key] !== row.default });
    if (!key) {
      if (request.method !== 'GET') return { status: 405, body: { error: 'GET required' } };
      return { status: 200, body: { settings: PLACES_POLICY.map(view) } };
    }
    const row = PLACES_POLICY.find(entry => entry.key === key);
    if (!row || request.method !== 'PUT') return { status: 404, body: { error: 'There is no such setting.', code: 'not_found' } };
    const value = request.body.value;
    if (value === null || value === undefined) delete this.policyChosen[row.key];
    else if (row.kind === 'switch' ? typeof value !== 'boolean' : !Number.isInteger(value as number) || (value as number) < row.min! || (value as number) > row.max!) {
      return { status: 400, body: { error: `${row.key} is out of range`, code: 'invalid' } };
    } else this.policyChosen[row.key] = value as boolean | number;
    return { status: 200, body: view(row) };
  }

  private proposals(request: WorldRequest): WorldResponse {
    if (request.method === 'GET' && request.path.endsWith('/proposals')) return { status: 200, body: { proposals: [], organizing: false } };
    return { status: 404, body: { error: 'This engine makes no place offers.', code: 'not_found' } };
  }

  private findChat(token: string) { return this.chats.find(row => row.tokens.includes(token) || row.id === token); }

  private usingRoute(request: WorldRequest, path: string): WorldResponse {
    const match = /^\/sessions\/([^/]+)\/using(?:\/(choice|apply))?$/.exec(path);
    if (!match) return { status: 404, body: { error: 'unknown engine action' } };
    const token = decodeURIComponent(match[1]);
    const chat = this.findChat(token);
    if (!chat) return { status: 404, body: { error: 'reattach this conversation' } };
    if (request.method === 'GET' && !match[2]) return { status: 200, body: this.usingOverrides.get(token) ?? this.usingView(chat) };
    if (request.method !== 'POST' || !match[2]) return { status: 405, body: { error: 'POST required' } };
    try { this.guard(request.body); } catch (error) { if (error instanceof Refusal) return { status: error.status, body: { error: error.message, code: error.code } }; throw error; }
    const view = structuredClone(this.usingOverrides.get(token) ?? this.usingView(chat));
    const decision = view.bundle.policy.find(row => row.field === request.body.field);
    const setting = view.settings.find(row => row.field === request.body.field);
    if (!decision || !setting) return { status: 422, body: { error: 'That field has no place setting.', code: 'not_a_candidate' } };
    if (match[2] === 'choice') {
      const candidate = decision.wanted.find(want => want.placeId === request.body.placeId);
      if (!candidate) return { status: 422, body: { error: 'That place is not a candidate.', code: 'not_a_candidate' } };
      decision.value = candidate.value;
      decision.outcome = 'chosen';
      decision.decidedBy = 'you';
      decision.chosen = candidate.placeId;
      Object.assign(setting, { state: 'pending', value: candidate.value, decidedBy: 'you' });
    } else if (!decision.value) return { status: 422, body: { error: 'Choose a place first.', code: 'needs_pick' } };
    else Object.assign(setting, { state: 'yours', value: decision.value, current: decision.value, reason: 'You chose this conversation’s setting.' });
    this.usingOverrides.set(token, view);
    return { status: 200, body: view };
  }

  private usingView(row: StoreChat): UsingView {
    const places = this.usedPlaces(row);
    const instructions = [];
    const sources = [];
    for (const used of places) {
      const place = this.places.find(item => item.id === used.id);
      if (!place) continue;
      if (place.instructions) instructions.push({ placeId: used.id, text: place.instructions, bytes: new TextEncoder().encode(place.instructions).length });
      for (const item of place.sources) {
        const key = `${item.kind}:${item.ref}`;
        let found = sources.find(source => source.key === key);
        if (!found) {
          found = { key, kind: item.kind, ref: item.ref, ...(item.label ? { label: item.label } : {}), status: 'ok' as const, from: [] };
          sources.push(found);
        }
        found.from.push({ placeId: used.id, sourceId: item.id, addedBy: item.addedBy, ...(item.at ? { at: item.at } : {}), level: used.level });
      }
    }
    return {
      chatId: row.id, engine: { places: true }, revision: this.revision, readAt: this.now,
      bundle: { chatId: row.id, revision: this.revision, places, instructions, sources, trimmed: [], refused: [], policy: [], counts: { places: places.length, sources: sources.length } },
      settings: [],
    };
  }

  private usedPlaces(row: StoreChat) {
    const out: UsingView['bundle']['places'] = [];
    const levels = new Map<string, number>();
    const queue = row.places.map(id => ({ id, level: 0, through: [] as string[] }));
    for (let index = 0; index < queue.length; index++) {
      const item = queue[index];
      if (item.level > 2 || levels.has(item.id)) continue;
      const place = this.places.find(candidate => candidate.id === item.id && !candidate.archived);
      if (!place) continue;
      levels.set(item.id, item.level);
      out.push({ id: place.id, name: place.name, tint: this.effectiveTint(place), level: item.level, inherited: item.level > 0, ...(item.level > 0 ? { through: item.through } : {}) });
      queue.push(...place.parents.map(id => ({ id, level: item.level + 1, through: [place.id] })));
    }
    return out;
  }

  private async workspaceRoute(request: WorldRequest, path: string): Promise<WorldResponse> {
    const key = decodeURIComponent(path.split('/')[2] ?? '');
    if (!KEY.test(key)) return { status: 404, body: { error: 'there is no such tab set', code: 'unknown_key' } };
    if (request.method === 'GET') return this.readWorkspace(key, request.search);
    if (request.method === 'PUT') return this.writeWorkspace(key, request.body);
    return { status: 405, body: { error: 'GET or PUT required' } };
  }

  private workspaceBody(key: string) {
    const held = this.workspaces.get(key);
    if (!held || held.revision === 0) return { key, revision: 0 };
    return { key, revision: held.revision, ...(held.updatedAt ? { updatedAt: held.updatedAt } : {}), ...(held.writer ? { writer: held.writer } : {}), workspace: held.workspace };
  }

  private async readWorkspace(key: string, search: URLSearchParams): Promise<WorldResponse> {
    if (!search.get('wait')) return { status: 200, body: this.workspaceBody(key) };
    const after = search.get('after');
    if (after === null || !/^\d+$/.test(after)) return { status: 400, body: { error: 'after must be a revision', code: 'invalid' } };
    const want = Number(after);
    const started = Date.now();
    // Playwright delivers one body, so a long poll cannot sit for the engine's 25s.
    // Return as soon as the revision moves, otherwise the record after this short wait.
    while (!this.closed && Date.now() - started < WORKSPACE_WAIT_MS) {
      if ((this.workspaces.get(key)?.revision ?? 0) !== want) return { status: 200, body: this.workspaceBody(key) };
      await this.pause(25);
    }
    return { status: 200, body: this.workspaceBody(key) };
  }

  private writeWorkspace(key: string, body: Record<string, unknown>): WorldResponse {
    const current = this.workspaces.get(key)?.revision ?? 0;
    if (typeof body.revision !== 'number' || !Number.isSafeInteger(body.revision) || body.revision < 0) return { status: 400, body: { error: 'that tab set is not one this engine can keep', code: 'invalid' } };
    if (body.revision !== current) return { status: 409, body: { error: 'these tabs changed in another window', code: 'conflict', current: this.workspaceBody(key) } };
    if (body.workspace === undefined || body.workspace === null || typeof body.workspace !== 'object') return { status: 400, body: { error: 'that tab set is not one this engine can keep', code: 'invalid' } };
    const saved: HeldWorkspace = { key, revision: current + 1, workspace: body.workspace, updatedAt: this.now, ...(typeof body.writer === 'string' && body.writer ? { writer: body.writer } : {}) };
    this.workspaces.set(key, saved);
    this.push('workspace', { key, revision: saved.revision, ...(saved.writer ? { writer: saved.writer } : {}) });
    return { status: 200, body: this.workspaceBody(key) };
  }

  private settingsRoute(request: WorldRequest, path: string): WorldResponse {
    if (path === '/settings/key') {
      if (request.method !== 'GET') return { status: 405, body: { error: 'GET required' } };
      return { status: 200, body: this.keyPresent ? { present: true, source: 'profile' } : { present: false } };
    }
    if (path === '/settings/engine') {
      if (request.method !== 'GET') return { status: 405, body: { error: 'GET required' } };
      return { status: 200, body: { local: true, connection: 'local', ...(this.model ? { model: this.model } : {}) } };
    }
    if (path === '/settings/permissions') {
      if (request.method === 'GET') return { status: 200, body: { ...this.permissions, modes: [...this.permissions.modes] } };
      if (request.method !== 'PUT') return { status: 405, body: { error: 'GET required' } };
      const mode = String(request.body.mode ?? '');
      if (!this.permissions.modes.includes(mode)) return { status: 400, body: { error: 'unknown tool approval mode' } };
      this.permissions = { ...this.permissions, mode };
      return { status: 200, body: { mode, modes: [...this.permissions.modes] } };
    }
    return { status: 404, body: { error: 'unknown engine action' } };
  }

  private jobsRoute(request: WorldRequest, path: string): WorldResponse {
    const match = /^\/sessions\/([^/]+)\/jobs(?:\/([^/]+)\/(stop|log))?$/.exec(path);
    if (!match) return { status: 404, body: { error: 'unknown job action' } };
    const token = decodeURIComponent(match[1]);
    const chat = this.findChat(token);
    if (!chat) return { status: 404, body: { error: 'reattach this conversation' } };
    const jobs = this.jobs.get(chat.id) ?? [];
    if (!match[2]) {
      if (request.method !== 'GET') return { status: 405, body: { error: 'GET required' } };
      return { status: 200, body: jobs.map(job => this.jobWire(job)) };
    }
    const job = jobs.find(row => String(row.id) === decodeURIComponent(match[2]));
    if (!job) return { status: 404, body: { error: `there is no job ${decodeURIComponent(match[2])} in this conversation` } };
    if (match[3] === 'stop') {
      if (request.method !== 'POST') return { status: 405, body: { error: 'POST required' } };
      if (job.state === 'running') job.state = 'stopped';
      this.jobs.set(chat.id, jobs);
      this.push('jobs', { chatId: chat.id, running: jobs.filter(row => row.state === 'running').length, jobs: jobs.map(row => this.jobWire(row)) });
      return { status: 200, body: { accepted: true } };
    }
    if (request.method !== 'GET') return { status: 405, body: { error: 'GET required' } };
    const tailed = tailOf(job.log, request.search.get('tail'));
    if ('error' in tailed) return { status: 400, body: { error: tailed.error } };
    return { status: 200, body: tailed };
  }
}

function tailOf(text: string, query: string | null): { text: string; truncated: boolean } | { error: string } {
  const bytes = Buffer.from(text);
  let limit = JOB_LOG_CAP;
  if (query !== null && query !== '') {
    if (!/^\d+$/.test(query)) return { error: 'tail must be a number of bytes' };
    limit = Math.min(limit, Number(query));
  }
  if (bytes.length <= limit) return { text, truncated: false };
  let cut = bytes.length - limit;
  while (cut < bytes.length && (bytes[cut] & 0xc0) === 0x80) cut += 1;
  return { text: bytes.subarray(cut).toString('utf8'), truncated: true };
}

export function createWorldEngine(scenario: WorldScenarioName = 'typicalDay'): WorldEngine {
  if (!WORLD_SCENARIOS.includes(scenario)) throw new Error(`unknown world scenario ${scenario}`);
  const engine = new Engine(scenario);
  return {
    calls: engine.calls, counts: engine.counts, scenario: engine.scenario,
    owns: path => engine.owns(path), hit: (method, path, body) => engine.hit(method, path, body),
    dispatch: request => engine.dispatch(request), nudge: () => engine.nudge(), close: () => engine.close(),
  };
}

/** Answers one Playwright route when it belongs to this mock. False lets the conversation mock take it. */
export async function answerWorldRoute(engine: WorldEngine, route: Route): Promise<boolean> {
  const request = route.request();
  const url = new URL(request.url());
  // Leave percent-encoding in place. A slash inside an id is one segment (`sess%2F1`), decoded by the route that reads it.
  const path = cleanPath(url.pathname.replace(/^.*\/api\/engine/, '') || '/');
  if (!engine.owns(path)) return false;
  const method = request.method();
  let body: Record<string, unknown> = {};
  if (method === 'POST' || method === 'PUT') {
    const raw = request.postData();
    if (raw) {
      try {
        const parsed = JSON.parse(raw) as unknown;
        body = parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {};
      } catch {
        engine.hit(method, path, {});
        await route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: 'That request was not JSON.', code: 'invalid' }) });
        return true;
      }
    }
  }
  const answer = await engine.dispatch({ method, path, search: url.searchParams, body });
  if (!answer) return false;
  if (answer.abort) {
    await route.abort().catch(() => undefined);
    return true;
  }
  if (answer.sse !== undefined) {
    await route.fulfill({ status: answer.status, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: answer.sse });
    return true;
  }
  await route.fulfill({ status: answer.status, contentType: 'application/json', body: JSON.stringify(answer.body ?? null) });
  return true;
}

/**
 * Installs the world mock on one page, and on `alsoServe` so two windows share
 * one fixture the way two app windows share one engine. Register it before the
 * fetches the spec asserts. Unrecognised /api/engine paths fall through.
 */
export async function installWorldEngine(page: Page, scenario: WorldScenarioName = 'typicalDay', alsoServe: Page[] = []): Promise<WorldEngine> {
  const engine = createWorldEngine(scenario);
  const pages = [page, ...alsoServe];
  let open = pages.length;
  for (const view of pages) {
    view.on('close', () => { if (--open <= 0) engine.close(); });
    await view.route('**/api/engine/**', async route => {
      if (await answerWorldRoute(engine, route)) return;
      await route.fallback();
    });
  }
  return engine;
}
