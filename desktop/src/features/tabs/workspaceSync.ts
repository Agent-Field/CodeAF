// One place's tab set, shared by every window on that place (Places 6d: same tab set, live, no forks).
//
// The engine holds the tabs. This window holds only what is a view of them: which tab is showing, which pane
// of a split is focused, and where the reader has scrolled. Those three stay in sessionStorage under this
// window's label, so a second window on the same place does not steal the tab this one is showing.
//
// Saves wait saveDelayMs after the last change and go out as one PUT with If-Match set to the revision this
// window loaded. A 412 (or the bridge's current 409 spelling of the same refusal) means another window wrote
// first: take that window's tabs, keep this window's active tab and split where those tabs still exist, and
// retry the write once. A `workspace` notice from any other writer pulls the saved document; a notice stamped
// with this window's own writer is the echo of its own save and is ignored.
//
// The tabs a person had before the engine kept them live in localStorage under the v1 key. The first time Now
// is empty they are copied in, the copy is marked, and the old key is left where it is. Offline, the window
// keeps editing the copy it has and the pending save goes out when the engine can be reached again.
import { worldClient } from '../world/worldClient.ts';

/** How long a burst of edits waits before one save. A keystroke does not become its own request. */
export const saveDelayMs = 300;

/** The tab set this app kept before the engine stored one per place. Now is that set. */
export const v1StorageKey = 'codeaf.desktop.workspace.v1';
/** Set after the v1 tabs have been considered, so a later empty Now does not copy them in again. */
export const v1ImportedKey = 'codeaf.desktop.workspace.v1.imported';

/** This window's view of one place. The label is the native window's name, so a relaunch finds the same view. */
export const viewStorageKey = (label: string, key: string) => `codeaf.desktop.workspace-view.v1:${label}:${key}`;

/** Which tab this window shows, which pane of each split it focuses, and the scroll of each pane. */
export type WindowView = {
  activeId?: string;
  split: Record<string, number>;
  scroll: Record<string, number>;
};

export type WorkspaceWire = {
  key: string;
  revision: number;
  writer?: string;
  workspace: unknown;
};

/** One engine request. Tests stand a fake bridge in here; the app uses the shared engine door. */
export type WorkspaceTransport = (
  path: string,
  init: { method: 'GET' | 'PUT'; headers?: Record<string, string>; body?: string },
) => Promise<{ status: number; body: unknown }>;

/** The world stream's `workspace` notices. The document itself is always fetched; the notice only says it changed. */
export type WorkspaceNotices = {
  subscribe(listener: () => void): () => void;
  lastWorkspace(key: string): { key: string; revision: number; writer?: string } | undefined;
};

export type WorkspaceClock = {
  set(run: () => unknown, ms: number): unknown;
  clear(timer: unknown): void;
};

export type WorkspaceSyncOptions<T> = {
  key: string;
  serialize: (state: T) => unknown;
  deserialize: (document: unknown, view: WindowView) => T;
  transport?: WorkspaceTransport;
  notices?: WorkspaceNotices;
  session?: Storage;
  local?: Storage;
  writer?: string;
  windowLabel?: string;
  clock?: WorkspaceClock;
  online?: () => boolean;
  listenOnline?: (retry: () => void) => () => void;
  viewOf?: (state: T) => WindowView;
};

export type WorkspaceSync<T> = {
  load: () => Promise<T>;
  /** Remembers the view at once and publishes the tabs after saveDelayMs. A view-only change is not published. */
  save: (state: T) => void;
  /** Fires when another window's tabs arrive, including a stale save that had to take them. */
  onRemote: (cb: (state: T) => void) => () => void;
  close: () => void;
};

const realClock: WorkspaceClock = {
  set: (run, ms) => setTimeout(run, ms),
  clear: timer => clearTimeout(timer as ReturnType<typeof setTimeout>),
};

const isObject = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);

function clone<T>(value: T): T {
  if (value === undefined) return value;
  return JSON.parse(JSON.stringify(value)) as T;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

function numberMap(value: unknown): Record<string, number> {
  if (!isObject(value)) return {};
  const out: Record<string, number> = {};
  for (const [id, n] of Object.entries(value)) {
    if (id && typeof n === 'number' && Number.isFinite(n) && n >= 0) out[id] = n;
  }
  return out;
}

/** A `{ tabId: focus }` map is this window's split view. A real split also carries its panes, so it is not one. */
function isFocusMap(value: unknown): boolean {
  return isObject(value) && Object.values(value).every(item => typeof item === 'number');
}

function stripFocus(tab: unknown): unknown {
  if (!isObject(tab) || !isObject(tab.split)) return tab;
  const split = { ...tab.split };
  delete split.focus;
  return { ...tab, split };
}

/**
 * The document the engine stores. View fields are removed here even when serialize left them in, because a
 * shared document that names the focused tab would move every other window.
 */
export function sharedDocument(value: unknown): unknown {
  if (!isObject(value)) return value;
  const copy = clone(value);
  delete copy.activeId;
  delete copy.recentIds;
  delete copy.scroll;
  delete copy.picked;
  if (isFocusMap(copy.split)) delete copy.split;
  if (Array.isArray(copy.tabs)) copy.tabs = copy.tabs.map(stripFocus);
  if (Array.isArray(copy.closed)) copy.closed = copy.closed.map(stripFocus);
  return copy;
}

function emptyDocument(): unknown {
  return { tabs: [] };
}

function documentOf(workspace: unknown): unknown {
  if (workspace == null) return emptyDocument();
  const shared = sharedDocument(workspace);
  return shared ?? emptyDocument();
}

/** Tab ids and, inside a split, pane ids. A view that names anything else belongs to a tab that is gone. */
function tabIds(doc: unknown): Set<string> {
  const ids = new Set<string>();
  const tabs = isObject(doc) && Array.isArray(doc.tabs) ? doc.tabs : [];
  for (const tab of tabs) {
    if (!isObject(tab) || typeof tab.id !== 'string' || !tab.id) continue;
    ids.add(tab.id);
    const panes = isObject(tab.split) && Array.isArray(tab.split.panes) ? tab.split.panes : [];
    for (const pane of panes) if (isObject(pane) && typeof pane.id === 'string' && pane.id) ids.add(pane.id);
  }
  return ids;
}

function keepView(view: WindowView, doc: unknown): WindowView {
  const ids = tabIds(doc);
  const keep = (map: Record<string, number>) => Object.fromEntries(Object.entries(map).filter(([id]) => ids.has(id)));
  return {
    activeId: view.activeId && ids.has(view.activeId) ? view.activeId : undefined,
    split: keep(view.split),
    scroll: keep(view.scroll),
  };
}

function idOf(item: unknown): string | undefined {
  return isObject(item) && typeof item.id === 'string' && item.id ? item.id : undefined;
}

/**
 * Three-way merge of one list. The other window's new rows stay. Rows only this window added are appended.
 * A row this window closed is dropped. A row both changed keeps this window's copy (DESIGN-QUESTIONS WS-1).
 */
function mergeList(base: unknown, pending: unknown, remote: unknown): unknown {
  if (!Array.isArray(pending) || !Array.isArray(remote)) return same(pending, base) ? remote : pending;
  const baseItems = Array.isArray(base) ? base : [];
  const baseIds = new Set(baseItems.map(idOf).filter((id): id is string => !!id));
  const pendingIds = new Set(pending.map(idOf).filter((id): id is string => !!id));
  const removed = new Set([...baseIds].filter(id => !pendingIds.has(id)));
  const pendingById = new Map(pending.flatMap(item => { const id = idOf(item); return id ? [[id, item] as const] : []; }));
  const baseById = new Map(baseItems.flatMap(item => { const id = idOf(item); return id ? [[id, item] as const] : []; }));
  const seen = new Set<string>();
  const out: unknown[] = [];
  for (const item of remote) {
    const id = idOf(item);
    if (id && removed.has(id)) continue;
    if (id) seen.add(id);
    const local = id ? pendingById.get(id) : undefined;
    const was = id ? baseById.get(id) : undefined;
    out.push(local && was && !same(local, was) ? local : item);
  }
  for (const item of pending) {
    const id = idOf(item);
    if (!id || baseIds.has(id) || seen.has(id)) continue;
    out.push(item);
  }
  return out;
}

const listKeys = new Set(['tabs', 'groups', 'closed']);

/** The remote document, plus the edits this window made since the revision it loaded. */
export function mergeDocuments(base: unknown, pending: unknown, remote: unknown): unknown {
  const remoteShared = documentOf(remote);
  if (!isObject(pending)) return remoteShared;
  const pendingShared = documentOf(pending);
  const baseShared = documentOf(base);
  if (!isObject(remoteShared) || !isObject(pendingShared) || !isObject(baseShared)) return remoteShared;
  const merged: Record<string, unknown> = { ...remoteShared };
  for (const key of Object.keys(pendingShared)) {
    if (listKeys.has(key)) {
      merged[key] = mergeList(baseShared[key], pendingShared[key], remoteShared[key]);
      continue;
    }
    if (same(pendingShared[key], baseShared[key])) continue;
    const local = pendingShared[key];
    const other = remoteShared[key];
    merged[key] = key === 'nextNumber' && typeof local === 'number' && typeof other === 'number' ? Math.max(local, other) : local;
  }
  return merged;
}

function readRecord(value: unknown): WorkspaceWire | undefined {
  if (!isObject(value)) return undefined;
  const revision = value.revision;
  if (typeof revision !== 'number' || !Number.isSafeInteger(revision) || revision < 0) return undefined;
  return {
    key: typeof value.key === 'string' ? value.key : '',
    revision,
    writer: typeof value.writer === 'string' ? value.writer : undefined,
    workspace: value.workspace === undefined ? null : value.workspace,
  };
}

function currentRecord(body: unknown): WorkspaceWire | undefined {
  return readRecord(body) ?? readRecord(isObject(body) ? body.current : undefined);
}

export function defaultView(state: unknown): WindowView {
  if (!isObject(state)) return { split: {}, scroll: {} };
  const activeId = typeof state.activeId === 'string' ? state.activeId : undefined;
  const scroll = numberMap(state.scroll);
  const explicit = numberMap(state.split);
  if (Object.keys(explicit).length) return { activeId, split: explicit, scroll };
  const split: Record<string, number> = {};
  if (Array.isArray(state.tabs)) {
    for (const tab of state.tabs) {
      if (!isObject(tab) || typeof tab.id !== 'string' || !isObject(tab.split)) continue;
      const focus = tab.split.focus;
      if (typeof focus === 'number' && Number.isInteger(focus) && focus >= 0) split[tab.id] = focus;
    }
  }
  return { activeId, split, scroll };
}

function readItem(store: Storage | undefined, key: string): string | null {
  try { return store?.getItem(key) ?? null; } catch { return null; }
}

function writeItem(store: Storage | undefined, key: string, value: string) {
  try { store?.setItem(key, value); } catch { /* A blocked store only costs the restore. */ }
}

function readStoredView(raw: string | null): WindowView {
  if (!raw) return { split: {}, scroll: {} };
  try {
    const value = JSON.parse(raw) as unknown;
    if (!isObject(value)) return { split: {}, scroll: {} };
    return {
      activeId: typeof value.activeId === 'string' ? value.activeId : undefined,
      split: numberMap(value.split),
      scroll: numberMap(value.scroll),
    };
  } catch { return { split: {}, scroll: {} }; }
}

function browserStore(kind: 'localStorage' | 'sessionStorage'): Storage | undefined {
  try { return globalThis[kind]; } catch { return undefined; }
}

function windowLabelOf(given: string | undefined, session: Storage | undefined): string {
  if (given && /^[A-Za-z0-9_-]{1,64}$/.test(given)) return given;
  const native = (globalThis as { __TAURI_INTERNALS__?: { metadata?: { currentWindow?: { label?: unknown } } } }).__TAURI_INTERNALS__?.metadata?.currentWindow?.label;
  if (typeof native === 'string' && /^[A-Za-z0-9_-]{1,64}$/.test(native)) return native;
  const kept = readItem(session, 'codeaf.desktop.window-label');
  if (kept && /^[A-Za-z0-9_-]{1,64}$/.test(kept)) return kept;
  const minted = `browser-${Math.random().toString(36).slice(2, 10)}`;
  writeItem(session, 'codeaf.desktop.window-label', minted);
  return minted;
}

function writerOf(given: string | undefined, label: string): string {
  if (given && /^[A-Za-z0-9_-]{1,64}$/.test(given)) return given;
  const id = `win-${label}`.replace(/[^A-Za-z0-9_-]/g, '').slice(0, 64);
  return /^[A-Za-z0-9_-]{1,64}$/.test(id) ? id : 'win-desktop';
}

function holdsTabs(value: unknown): value is Record<string, unknown> {
  return isObject(value) && Array.isArray(value.tabs) && value.tabs.length > 0
    && value.tabs.every(tab => isObject(tab) && typeof tab.id === 'string' && !!tab.id);
}

function isEmptySet(record: WorkspaceWire): boolean {
  if (record.revision > 0) return false;
  if (record.workspace == null) return true;
  return isObject(record.workspace) && Array.isArray(record.workspace.tabs) && record.workspace.tabs.length === 0;
}

type EngineFailure = { status: number; unreachable: boolean };

function engineFailure(error: unknown): EngineFailure | undefined {
  if (!error || typeof error !== 'object') return undefined;
  const candidate = error as { name?: string; status?: unknown; unreachable?: unknown };
  if (candidate.name !== 'EngineError' || typeof candidate.status !== 'number') return undefined;
  return { status: candidate.status, unreachable: candidate.unreachable === true };
}

async function engineTransport(path: string, init: { method: 'GET' | 'PUT'; headers?: Record<string, string>; body?: string }) {
  const { fetchEngine } = await import('../chat/engine-client.ts');
  try {
    const response = await fetchEngine(path, { method: init.method, headers: init.headers, body: init.body });
    return { status: response.status, body: await response.json().catch(() => null) };
  } catch (error) {
    const failure = engineFailure(error);
    // fetchEngine keeps the status and drops the body. A stale write is then read with GET.
    if (failure) return { status: failure.unreachable ? 0 : failure.status, body: null };
    return { status: 0, body: null };
  }
}

function defaultOnline(): boolean {
  return typeof navigator === 'undefined' || navigator.onLine !== false;
}

function defaultListenOnline(retry: () => void): () => void {
  if (typeof window === 'undefined') return () => {};
  window.addEventListener('online', retry);
  return () => window.removeEventListener('online', retry);
}

export function createWorkspaceSync<T>(options: WorkspaceSyncOptions<T>): WorkspaceSync<T> {
  const key = options.key;
  const transport = options.transport ?? engineTransport;
  const notices = options.notices ?? worldClient;
  const session = options.session ?? browserStore('sessionStorage');
  const local = options.local ?? browserStore('localStorage');
  const clock = options.clock ?? realClock;
  const isOnline = options.online ?? defaultOnline;
  const viewOf = options.viewOf ?? ((state: T) => defaultView(state));
  const label = windowLabelOf(options.windowLabel, session);
  const writer = writerOf(options.writer, label);
  const path = `/workspaces/${encodeURIComponent(key)}`;
  const viewKey = viewStorageKey(label, key);

  let base: unknown = emptyDocument();
  let revision = 0;
  let pending: unknown;
  let currentView: WindowView = { split: {}, scroll: {} };
  let memory: T | undefined;
  let dirty = false;
  let importPending = false;
  let ready = false;
  let closed = false;
  let needsPull = false;
  let seenRevision = -1;
  let saveEpoch = 0;
  let pullGen = 0;
  let timer: unknown;
  let tail: Promise<void> = Promise.resolve();
  const remoteListeners = new Set<(state: T) => void>();

  function storedView(): WindowView {
    return readStoredView(readItem(session, viewKey));
  }

  function storeView(view: WindowView) {
    currentView = view;
    writeItem(session, viewKey, JSON.stringify({ activeId: view.activeId, split: view.split, scroll: view.scroll }));
  }

  function emit(state: T) {
    memory = state;
    for (const listener of [...remoteListeners]) listener(state);
  }

  function marked(): boolean {
    return readItem(local, v1ImportedKey) === '1';
  }

  function markImported() {
    writeItem(local, v1ImportedKey, '1');
    importPending = false;
  }

  function readV1(): T | undefined {
    const raw = readItem(local, v1StorageKey);
    if (!raw) return undefined;
    try {
      const value = JSON.parse(raw) as unknown;
      return holdsTabs(value) ? value as T : undefined;
    } catch { return undefined; }
  }

  async function request(method: 'GET' | 'PUT', match?: number, doc?: unknown) {
    if (!isOnline()) return { status: 0, body: null as unknown };
    const headers: Record<string, string> = { Accept: 'application/json' };
    let body: string | undefined;
    if (method === 'PUT') {
      // The revision rides in If-Match, and again in the body: today's bridge compares the body and
      // does not read the header. They are the same number, so either door refuses a stale write.
      headers['If-Match'] = String(match);
      body = JSON.stringify({ revision: match, writer, workspace: doc });
    }
    return transport(path, { method, headers, body });
  }

  async function get(): Promise<WorkspaceWire | undefined> {
    const response = await request('GET');
    if (response.status === 0 || response.status >= 500) return undefined;
    if (response.status !== 200) throw new Error('The engine returned an invalid tab set.');
    const record = readRecord(response.body);
    if (!record) throw new Error('The engine returned an invalid tab set.');
    return record;
  }

  function applyDocument(doc: unknown, view: WindowView, tell: boolean): T {
    const kept = keepView(view, doc);
    storeView(kept);
    const state = options.deserialize(doc, kept);
    memory = state;
    if (tell) emit(state);
    return state;
  }

  async function resolveRemote(body: unknown): Promise<WorkspaceWire | undefined> {
    return currentRecord(body) ?? get();
  }

  async function publish(epoch: number): Promise<void> {
    if (closed || epoch !== saveEpoch || !dirty || pending === undefined) return;
    if (!isOnline()) return;
    let doc: unknown = pending;
    let view = currentView;
    let match = revision;
    let rebased = false;
    for (let attempt = 0; attempt < 2; attempt++) {
      if (epoch !== saveEpoch) return;
      const response = await request('PUT', match, doc);
      if (epoch !== saveEpoch) {
        if (response.status === 200) {
          const saved = readRecord(response.body);
          if (saved && saved.revision > revision) revision = saved.revision;
        }
        return;
      }
      if (response.status === 200) {
        const saved = readRecord(response.body);
        revision = saved?.revision ?? match + 1;
        base = clone(doc);
        pending = undefined;
        dirty = false;
        needsPull = false;
        if (importPending) markImported();
        if (rebased) applyDocument(base, view, true);
        else applyDocument(base, view, false);
        return;
      }
      const stale = response.status === 412 || response.status === 409;
      if (!stale) {
        dirty = true;
        return;
      }
      const remote = await resolveRemote(response.body);
      if (!remote || epoch !== saveEpoch) {
        dirty = true;
        return;
      }
      if (attempt === 1) {
        // The one retry was refused too. The other window's tabs stand, and this write stops.
        dirty = false;
        pending = undefined;
        if (importPending) markImported();
        applyWire(remote, view);
        return;
      }
      doc = mergeDocuments(base, doc, remote.workspace);
      view = keepView(view, doc);
      storeView(view);
      match = remote.revision;
      revision = remote.revision;
      pending = doc;
      rebased = true;
    }
  }

  function applyWire(record: WorkspaceWire, view: WindowView) {
    revision = record.revision;
    base = documentOf(record.workspace);
    applyDocument(base, view, true);
  }

  function kick(epoch = saveEpoch): Promise<void> {
    if (timer !== undefined) {
      clock.clear(timer);
      timer = undefined;
    }
    const job = tail.then(() => publish(epoch));
    tail = job.then(() => undefined, () => undefined);
    return job;
  }

  function arm() {
    if (closed) return;
    if (timer !== undefined) clock.clear(timer);
    timer = clock.set(() => {
      timer = undefined;
      return kick(saveEpoch);
    }, saveDelayMs);
  }

  async function pull(): Promise<void> {
    const gen = ++pullGen;
    const remote = await get();
    if (gen !== pullGen || !remote) {
      if (!remote) needsPull = true;
      return;
    }
    needsPull = false;
    if (remote.writer === writer) {
      if (remote.revision > revision) revision = remote.revision;
      return;
    }
    if (remote.revision === revision && !dirty) return;
    const view = keepView(currentView, documentOf(remote.workspace));
    if (dirty && pending !== undefined) {
      const merged = mergeDocuments(base, pending, remote.workspace);
      revision = remote.revision;
      base = documentOf(remote.workspace);
      pending = merged;
      saveEpoch += 1;
      dirty = true;
      applyDocument(merged, view, true);
      arm();
      return;
    }
    revision = remote.revision;
    base = documentOf(remote.workspace);
    applyDocument(base, view, true);
  }

  function onNotice() {
    if (!ready || closed) return;
    const notice = notices.lastWorkspace(key);
    if (!notice || notice.revision === seenRevision) return;
    seenRevision = notice.revision;
    // Our own save comes back on the stream. Applying it would move this window onto itself.
    if (notice.writer === writer) {
      if (notice.revision > revision) revision = notice.revision;
      return;
    }
    if (!dirty && notice.revision <= revision) return;
    void pull();
  }

  const stopNotices = notices.subscribe(onNotice);
  const stopOnline = (options.listenOnline ?? defaultListenOnline)(() => {
    const job = tail.then(async () => {
      if (needsPull) await pull();
      if (dirty) await publish(saveEpoch);
    });
    tail = job.then(() => undefined, () => undefined);
  });

  async function importV1(record: WorkspaceWire): Promise<WorkspaceWire> {
    if (key !== 'now' || marked()) return record;
    if (!isEmptySet(record)) {
      markImported();
      return record;
    }
    const saved = readV1();
    if (!saved) return record;
    const doc = sharedDocument(options.serialize(saved));
    const sessionView = storedView();
    const importedView = keepView(defaultView(saved), doc);
    const view = sessionView.activeId || Object.keys(sessionView.split).length || Object.keys(sessionView.scroll).length
      ? sessionView : importedView;
    storeView(keepView(view, doc));
    const response = await request('PUT', record.revision, doc);
    if (response.status === 200) {
      markImported();
      const written = readRecord(response.body);
      base = clone(doc);
      revision = written?.revision ?? record.revision + 1;
      return written ?? { key, revision, writer, workspace: doc };
    }
    if (response.status === 412 || response.status === 409) {
      // Someone else filled Now while this copy was in flight. Their tabs stay; the old key is not tried again.
      markImported();
      return await get() ?? record;
    }
    importPending = true;
    pending = doc;
    dirty = true;
    base = emptyDocument();
    revision = record.revision;
    return { key, revision: record.revision, writer, workspace: doc };
  }

  async function load(): Promise<T> {
    if (!isOnline()) {
      if (memory) return memory;
      const saved = key === 'now' && !marked() ? readV1() : undefined;
      if (saved) {
        const doc = sharedDocument(options.serialize(saved));
        importPending = true;
        pending = doc;
        dirty = true;
        ready = true;
        return applyDocument(doc, keepView(defaultView(saved), doc), false);
      }
      throw new Error('codeaf engine is not running');
    }
    const fetched = await get();
    if (!fetched) {
      if (memory) return memory;
      throw new Error('codeaf engine is not running');
    }
    const record = await importV1(fetched);
    const view = keepView(storedView(), documentOf(record.workspace));
    const state = applyDocument(documentOf(record.workspace), view, false);
    base = documentOf(record.workspace);
    revision = record.revision;
    ready = true;
    onNotice();
    return state;
  }

  function save(state: T) {
    const doc = sharedDocument(options.serialize(state));
    storeView(keepView(viewOf(state), doc));
    memory = state;
    if (same(doc, base)) {
      pending = undefined;
      dirty = false;
      saveEpoch += 1;
      if (timer !== undefined) {
        clock.clear(timer);
        timer = undefined;
      }
      return;
    }
    pending = doc;
    dirty = true;
    saveEpoch += 1;
    arm();
  }

  function onRemote(cb: (state: T) => void) {
    remoteListeners.add(cb);
    return () => { remoteListeners.delete(cb); };
  }

  function close() {
    closed = true;
    if (timer !== undefined) clock.clear(timer);
    timer = undefined;
    stopNotices();
    stopOnline();
    remoteListeners.clear();
  }

  return { load, save, onRemote, close };
}
