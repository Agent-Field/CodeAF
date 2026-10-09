// The live mirror of one place's tab set (Places 6d "Same place, two windows: both windows show the same tab
// set, live. No forked tab sets. A window is just a view of a place.").
//
// THE ENGINE HOLDS THE CANONICAL TAB SET; THIS WINDOW HOLDS ITS OWN INTENT. Every change a person makes is a real
// WorkspaceAction, applied at once through the pure workspaceReducer and queued. The queue is saved with a
// compare-and-swap write; when the engine refuses it because another window wrote first, the refusal carries the
// current tab set and the queue is REPLAYED over it, action by action, so both windows' changes land. Nothing is
// ever resolved by "last writer wins" over a whole document, which is how the localStorage mirror lost one of two
// simultaneous edits.
//
// Three rules make the replay honest:
//   1. Ids are stable. An action whose reducer mints ids (a new tab, a group, a split, the fresh tab after the
//      last one closes) records them the first time it runs and is replayed with the same ids, so a tab keeps its
//      id through any number of rebases and the other window's references to it stay good.
//   2. Only the shared half merges. Which tab this window shows, its recency order, which pane of a split it
//      focuses and its scroll are window-local (shared.ts); an action that changes only those is never queued, so
//      selecting a tab never echoes into another window.
//   3. Nothing is dropped in silence. A replayed action that no longer does anything (another window closed the
//      tab it changed) is counted and named in the status; a draft for a tab closed elsewhere is kept on the closed
//      tab, so Reopen brings it back with the words in it.
//
// Offline, the window keeps working on its own copy, the queue is kept (and saved locally, so a reload does not
// lose it), the status says the engine cannot be reached, and saving is retried on a doubling delay, never in a
// busy loop. No request here calls a model.
import { createId, setIdSource } from '../tabs/helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceAction, type WorkspaceState } from '../tabs/model.ts';
import { visibleTabs } from '../tabs/helpers.ts';
import { WorkspaceSyncError, type WorkspaceClient, type WorkspaceKey, type WorkspaceRecord } from './client.ts';
import { limits, retryCeilingMs, retryFloorMs, saveDelayMs } from './limits.ts';
import { compose, duplicateIds, emptyLocal, localOf, sharedOf, sharedText, type SharedWorkspace, type WindowLocal } from './shared.ts';

/**
 * One queued change: the action, the ids its reducer minted the first time it ran, and what a toggle meant.
 * `want` is the value a pin or collapse toggle produced here, so a replay over a tab set where another window
 * already made the same change leaves it alone instead of toggling it back. `reopened` is the tab Reopen brought
 * back, so a replay reopens that tab and not whichever one another window closed last. `prior` is the draft a
 * typing burst started from: replayed over a draft another window changed in the meantime, it still wins (the
 * person here typed last) but the other window's words are counted as overtaken, never replaced in silence.
 */
export type Entry = { action: WorkspaceAction; ids: string[]; want?: boolean; reopened?: string; prior?: string };

/** What a window saves locally for one place, so a reload or relaunch resumes exactly where it was. */
export type Persisted = {
  /** The last confirmed tab set, or the local import seed until the first engine answer. */
  base?: SharedWorkspace;
  revision: number;
  /** Changes made here that the engine has not confirmed yet. */
  pending: Entry[];
  local: WindowLocal;
};

export type SyncPhase = 'loading' | 'saved' | 'saving' | 'offline' | 'refused';
export type SyncStatus = {
  phase: SyncPhase;
  /** The engine's sentence for the last failure; absent when the last request succeeded. */
  error?: string;
  code?: string;
  /** Changes made here that another window's edit overtook, since the person last saw the status. */
  overtaken: number;
  /** Changes not yet confirmed by the engine. */
  unsaved: number;
};

type Timer = unknown;
export type Clock = { set(run: () => void, ms: number): Timer; clear(timer: Timer): void };
const realClock: Clock = { set: (run, ms) => setTimeout(run, ms), clear: timer => clearTimeout(timer as ReturnType<typeof setTimeout>) };

export type ControllerOptions = {
  key: WorkspaceKey;
  client: WorkspaceClient;
  /** This window's writer tag ([A-Za-z0-9_-]{1,64}); it marks its writes so their echo is recognised. */
  writer: string;
  /**
   * The tab set to start from when this window has nothing saved for the place: the one-time import of the
   * localStorage tab set a person had before the engine kept them. It is offered to the engine only when the
   * engine has nothing either; once the engine holds a tab set it is never imported again.
   */
  initial: () => WorkspaceState;
  /** What this window saved last time (its own queue, focus and scroll). */
  persisted?: Persisted;
  /** Queued changes of a window that closed before the engine confirmed them; replayed after this window's own. */
  adopted?: Entry[];
  /** Saves `Persisted` locally; called after every change, at most once per `persistDelayMs`. */
  persist?: (state: Persisted) => void;
  /** A window handed a tab (Move to new window) shows it first. */
  focus?: string;
  /** A draft typed in one pane is copied to every pane of the same conversation (Places 6e "one composer state"). */
  mirrorDrafts?: boolean;
  reduce?: (state: WorkspaceState, action: WorkspaceAction) => WorkspaceState;
  clock?: Clock;
  saveDelayMs?: number;
  persistDelayMs?: number;
};

/** Runs `run` with createId answering `ids` in order (then fresh ids), and reports every id it handed out. */
export function withIds<T>(ids: readonly string[] | undefined, run: () => T): { result: T; ids: string[] } {
  const fresh = createId;
  const used: string[] = [];
  let at = 0;
  setIdSource(() => { const id = ids && at < ids.length ? ids[at++] : fresh(); used.push(id); return id; });
  try { return { result: run(), ids: used }; } finally { setIdSource(fresh); }
}

// Changes that only ever replace the previous one of their kind on the same target: a burst is one entry.
const coalescing = new Set(['draft', 'split-resize']);
const targetOf = (action: WorkspaceAction) => (action as { id?: string }).id;

/** The pane with this id, in an open tab or a split. */
function paneById(state: WorkspaceState, id: string) {
  for (const tab of state.tabs) {
    if (tab.id === id && !tab.split) return tab;
    const pane = tab.split?.panes.find(p => p.id === id);
    if (pane) return pane;
  }
  return undefined;
}

/** A draft for a tab another window closed is kept on the closed tab, so Reopen brings the words back. */
function keepDraftOnClosed(state: WorkspaceState, id: string, draft: string): WorkspaceState | undefined {
  let found = false;
  const closed = state.closed.map((tab): Tab => {
    if (tab.id === id && !tab.split) { found = true; return { ...tab, draft }; }
    if (!tab.split?.panes.some(p => p.id === id)) return tab;
    found = true;
    return { ...tab, split: { ...tab.split, panes: tab.split.panes.map(p => (p.id === id ? { ...p, draft } : p)) } };
  });
  return found ? { ...state, closed } : undefined;
}

export type WorkspaceController = ReturnType<typeof createWorkspaceController>;

export function createWorkspaceController(options: ControllerOptions) {
  const { key, client } = options;
  let writer = options.writer;
  let persist = options.persist;
  const reduce = options.reduce ?? workspaceReducer;
  const clock = options.clock ?? realClock;
  const saveDelay = options.saveDelayMs ?? saveDelayMs;
  const persistDelay = options.persistDelayMs ?? 150;
  const mirrorDrafts = options.mirrorDrafts ?? true;

  const saved = options.persisted;
  let base: SharedWorkspace | undefined = saved?.base;
  let revision = saved?.revision ?? 0;
  let pending: Entry[] = [...(saved?.pending ?? []), ...(options.adopted ?? [])];
  let inflight: Entry[] | null = null;
  let relocating = false;
  let movedTo: Record<string, WorkspaceKey> = {};
  let local: WindowLocal = saved?.local ? { ...emptyLocal(), ...saved.local } : emptyLocal();
  let requestedFocus = options.focus;
  if (options.focus) local = { ...local, activeId: options.focus };
  /** Keep the import seed locally even before first contact, so offline edits replay over the same tabs after reload. */
  let view: WorkspaceState = base ? compose(base, local) : options.initial();
  if (!base) {
    base = sharedOf(view);
    local = localOf(view, local);
  }
  let status: SyncStatus = { phase: 'loading', overtaken: 0, unsaved: pending.length };
  let loaded = false;
  let stopped = true;
  /** The engine refused this build's writes for good (a newer codeaf wrote the file): read and mirror only. */
  let readOnly = false;
  let conflictsInARow = 0;
  let retryDelay = retryFloorMs;
  let settle: Promise<void> = Promise.resolve();
  let watchAbort: AbortController | undefined;
  let saveTimer: Timer | undefined;
  let persistTimer: Timer | undefined;
  let wakeWatch: (() => void) | undefined;
  const listeners = new Set<() => void>();
  // A window reopened with changes the engine never confirmed shows them at once, over its last confirmed set.
  if (base && pending.length) rebuild();

  const notify = () => { for (const listener of listeners) listener(); };
  const setStatus = (next: Partial<SyncStatus>) => { status = { ...status, ...next, unsaved: pending.length + (inflight?.length ?? 0) }; };

  const snapshot = (): Persisted => ({ base, revision, pending: [...(inflight ?? []), ...pending], local });
  const persistNow = () => {
    if (persistTimer !== undefined) { clock.clear(persistTimer); persistTimer = undefined; }
    try { persist?.(snapshot()); } catch { /* A full or unavailable local store must not stop the tabs. */ }
  };
  const persistSoon = () => { if (persistTimer === undefined) persistTimer = clock.set(() => { persistTimer = undefined; persistNow(); }, persistDelay); };

  /** Applies one entry with its recorded ids. A result that would hold an id twice is the change already being there. */
  function apply(state: WorkspaceState, entry: Entry): { next: WorkspaceState; ids: string[]; repeated: boolean } {
    const settled = intentHolds(state, entry);
    if (settled !== undefined) return { next: settled, ids: entry.ids, repeated: true };
    const { result, ids } = withIds(entry.ids, () => reduce(state, entry.action));
    if (result !== state && duplicateIds(result)) return { next: state, ids: entry.ids, repeated: true };
    return { next: result, ids, repeated: false };
  }

  /** Once nothing is queued or in flight, a window that was saving (or catching up after being offline) is saved. */
  function settleIfIdle() {
    if (!inflight && !pending.length && base && (status.phase === 'saving' || status.phase === 'offline')) setStatus({ phase: 'saved', error: undefined, code: undefined });
  }

  /** Recomputes the view: the confirmed tab set, this window's focus over it, and every unconfirmed change replayed. */
  function rebuild() {
    if (!base) return;
    const before = view;
    const requested = requestedFocus && base.tabs.some(tab => tab.id === requestedFocus || tab.split?.panes.some(pane => pane.id === requestedFocus));
    const intendedLocal = requested ? { ...local, activeId: requestedFocus } : local;
    let state = compose(base, intendedLocal, before);
    if (requested) requestedFocus = undefined;
    const kept: Entry[] = [];
    let overtaken = 0;
    for (const entry of pending) {
      const was = sharedText(state);
      if (entry.action.type === 'draft' && entry.prior !== undefined) {
        const now = paneById(state, entry.action.id)?.draft;
        if (now !== undefined && now !== entry.prior && now !== entry.action.draft) overtaken++;
      }
      const { next, ids, repeated } = apply(state, entry);
      // The change is already in the confirmed set (another window made it, or this window's save landed and the
      // answer was lost): nothing to redo. A Reopen pointed back at its own tab is a change and is kept.
      if (repeated) {
        if (sharedText(next) !== was) { state = next; kept.push(entry); }
        continue;
      }
      if (sharedText(next) !== was) { state = next; kept.push({ ...entry, ids }); continue; }
      const draft = entry.action.type === 'draft' ? entry.action : undefined;
      if (draft && !paneById(state, draft.id)) {
        const rescued = keepDraftOnClosed(state, draft.id, draft.draft);
        if (rescued) { state = rescued; kept.push(entry); continue; }
        // Keep a late draft until its canonical destination acknowledges it, even if a relocation hint is missing.
        kept.push(entry); continue;
      }
      // Still a change only if it sits on what it changed; otherwise another window's edit overtook it.
      if (draft ? !paneById(state, draft.id) : true) overtaken++;
    }
    pending = kept;
    // Replayed actions carry shared intent; their reducer focus side effects
    // must not replace the window's later selection saved beside that queue.
    view = compose(sharedOf(state), intendedLocal, before);
    local = localOf(view, local);
    if (overtaken) status = { ...status, overtaken: status.overtaken + overtaken };
  }

  /**
   * Toggles and Reopen replayed as what they meant. Answers the state to continue from when the entry's intent
   * already holds (nothing to do), or undefined to run the action; a Reopen is pointed back at its own tab.
   */
  function intentHolds(state: WorkspaceState, entry: Entry): WorkspaceState | undefined {
    const action = entry.action;
    if (action.type === 'pin' && entry.want !== undefined) {
      const tab = state.tabs.find(t => t.id === action.id);
      return !tab || tab.pinned === entry.want ? state : undefined;
    }
    if (action.type === 'collapse-group' && entry.want !== undefined) {
      const group = state.groups.find(g => g.id === action.id);
      return !group || group.collapsed === entry.want ? state : undefined;
    }
    if (action.type === 'reopen' && entry.reopened) {
      if (state.tabs.some(t => t.id === entry.reopened)) return state;
      const tab = state.closed.find(t => t.id === entry.reopened);
      if (!tab) return state;
      // Reopen takes the newest closed tab: make this one the newest for the replay.
      return reduce({ ...state, closed: [...state.closed.filter(t => t !== tab), tab] }, action);
    }
    return undefined;
  }

  /** What a toggle or Reopen meant, read off the state it produced here. */
  function intentOf(action: WorkspaceAction, before: WorkspaceState, after: WorkspaceState): Pick<Entry, 'want' | 'reopened' | 'prior'> {
    if (action.type === 'pin') return { want: after.tabs.find(t => t.id === action.id)?.pinned };
    if (action.type === 'collapse-group') return { want: after.groups.find(g => g.id === action.id)?.collapsed };
    if (action.type === 'reopen') return { reopened: before.closed[before.closed.length - 1]?.id };
    if (action.type === 'draft') return { prior: paneById(before, action.id)?.draft };
    return {};
  }

  function enqueue(entry: Entry) {
    const last = pending[pending.length - 1];
    if (last && coalescing.has(entry.action.type) && last.action.type === entry.action.type && targetOf(last.action) === targetOf(entry.action)) pending[pending.length - 1] = { ...entry, prior: last.prior };
    else pending.push(entry);
  }

  /** The actions a person's one action stands for: a draft is typed into every pane of the same conversation. */
  function expand(action: WorkspaceAction): WorkspaceAction[] {
    if (!mirrorDrafts || action.type !== 'draft') return [action];
    const pane = paneById(view, action.id);
    if (!pane?.sessionFile) return [action];
    const twins = view.tabs.flatMap(tab => (tab.split ? tab.split.panes : [tab])).filter(p => p.id !== action.id && p.sessionFile === pane.sessionFile && p.draft !== action.draft);
    return [action, ...twins.map(p => ({ type: 'draft' as const, id: p.id, draft: action.draft }))];
  }

  /** The window's dispatch: applies at once, queues what changed the shared tab set, and saves shortly after. */
  function dispatch(action: WorkspaceAction) {
    let changedShared = false;
    for (const expanded of expand(action)) {
      // Selection belongs to this window. Persist its concrete intent before another window replays it.
      const one: WorkspaceAction = expanded.type === 'group-picked'
        ? { ...expanded, type: 'group', id: view.activeId, ids: [...(view.picked ?? [])] }
        : expanded;
      const before = view;
      const { result, ids } = withIds(undefined, () => reduce(view, one));
      if (result === before || duplicateIds(result)) continue;
      const shared = sharedText(before) !== sharedText(result);
      view = result;
      if (shared) { enqueue({ action: one, ids, ...intentOf(one, before, result) }); changedShared = true; }
    }
    local = localOf(view, local);
    setStatus({});
    persistSoon();
    notify();
    if (changedShared) scheduleSave(saveDelay);
  }

  function scheduleSave(ms: number) {
    if (stopped || readOnly) return;
    if (saveTimer !== undefined) clock.clear(saveTimer);
    saveTimer = clock.set(() => { saveTimer = undefined; void save(); }, ms);
  }

  const backoff = () => { const wait = retryDelay; retryDelay = Math.min(retryDelay * 2, retryCeilingMs); return wait; };
  /** Waits out a retry delay; retry() and stop() end it early. */
  const pause = (ms: number) => new Promise<void>(resolve => {
    const timer = clock.set(() => { wakeWatch = undefined; resolve(); }, ms);
    wakeWatch = () => { clock.clear(timer); wakeWatch = undefined; resolve(); };
  });

  /** Sends the queue as one compare-and-swap write. One write is in flight at a time. */
  async function save(): Promise<void> {
    if (stopped || readOnly || !loaded || inflight || relocating) return;
    const outgoing = pending.filter(entry => { const action = entry.action; return action.type === 'draft' && !paneById(view, action.id) && !view.closed.some(tab => tab.id === action.id || tab.split?.panes.some(p => p.id === action.id)); });
    if (outgoing.length) {
      relocating = true;
      try {
        for (const entry of outgoing) {
          if (entry.action.type !== 'draft') continue;
          const action = entry.action;
          let target = movedTo[action.id];
          if (!target) throw new WorkspaceSyncError('A draft is kept here until codeaf can find its moved tab.', 409, 'relocation_missing');
          const seen = new Set<WorkspaceKey>([key]);
          let confirmed = false;
          let overtookDraft = false;
          for (let attempt = 0; attempt < 12 && !confirmed; attempt++) {
            if (seen.has(target)) throw new WorkspaceSyncError('A moved tab location could not be confirmed.', 409, 'relocation_missing');
            const record = await client.get(target);
            if (!record.workspace) throw new WorkspaceSyncError('A draft is kept here until its moved tab is available.', 409, 'relocation_missing');
            let destination = compose(record.workspace, emptyLocal());
            const currentDraft = paneById(destination, action.id)?.draft;
            if (entry.prior !== undefined && currentDraft !== undefined && currentDraft !== entry.prior && currentDraft !== action.draft) overtookDraft = true;
            if (!paneById(destination, action.id)) {
              const closed = keepDraftOnClosed(destination, action.id, action.draft);
              if (closed) destination = closed;
              else if (record.movedTo?.[action.id]) { seen.add(target); target = record.movedTo[action.id]; continue; }
              else throw new WorkspaceSyncError('A draft is kept here until its moved tab is available.', 409, 'relocation_missing');
            } else destination = reduce(destination, action);
            const result = await client.put(target, record.revision, writer, sharedOf(destination));
            if (result.kind === 'saved') confirmed = true;
          }
          if (!confirmed) throw new WorkspaceSyncError('The moved tab is busy. Its draft is kept here for retry.', 409, 'busy');
          if (overtookDraft) setStatus({ overtaken: status.overtaken + 1 });
          pending = pending.filter(current => current !== entry);
          persistNow();
        }
      } catch (failure) {
        const error = failure instanceof WorkspaceSyncError ? failure : new WorkspaceSyncError('The moved draft could not be saved.', 0, '', true);
        setStatus({ phase: error.unreachable || error.code === 'busy' ? 'offline' : 'refused', code: error.code, error: error.message });
        persistNow(); notify();
        if (error.unreachable || error.code === 'busy') scheduleSave(backoff());
        return;
      } finally { relocating = false; }
      rebuild(); setStatus({ phase: pending.length ? 'saving' : 'saved', error: undefined, code: undefined });
    }
    if (base && !pending.length) { settleIfIdle(); notify(); return; }
    const document = sharedOf(view);
    if (new TextEncoder().encode(JSON.stringify(document)).length > limits.documentBytes) {
      setStatus({ phase: 'refused', code: 'too_large', error: 'these tabs are larger than codeaf can save; close some tabs or shorten a draft' });
      notify();
      return;
    }
    inflight = pending;
    pending = [];
    setStatus({ phase: 'saving' });
    notify();
    let done!: () => void;
    settle = new Promise(resolve => { done = resolve; });
    try {
      const answer = await client.put(key, revision, writer, document);
      const sent = inflight;
      inflight = null;
      if (answer.kind === 'saved') {
        conflictsInARow = 0;
        retryDelay = retryFloorMs;
        adoptRecord(answer.record);
        setStatus({ phase: pending.length ? 'saving' : 'saved', error: undefined, code: undefined });
        if (pending.length) scheduleSave(0);
      } else {
        // Another window wrote first: take what it wrote and replay this window's changes over it.
        conflictsInARow++;
        pending = [...sent, ...pending];
        adoptRecord(answer.current);
        setStatus({ phase: 'saving', error: undefined, code: undefined });
        // A tab set two windows keep rewriting converges quickly; past three refusals in a row, step back a little.
        scheduleSave(conflictsInARow > 3 ? Math.min(250 * 2 ** (conflictsInARow - 4), 5_000) : 0);
      }
    } catch (failure) {
      pending = [...(inflight ?? []), ...pending];
      inflight = null;
      const error = failure instanceof WorkspaceSyncError ? failure : new WorkspaceSyncError('codeaf could not save these tabs', 0, '', true);
      if (error.unreachable || error.code === 'busy') {
        setStatus({ phase: 'offline', error: error.message, code: error.code });
        scheduleSave(backoff());
      } else {
        // A refusal that repeating cannot fix: say it, keep every change here, and try again only on the next change.
        if (error.code === 'newer') readOnly = true;
        setStatus({ phase: 'refused', error: error.message, code: error.code });
      }
    } finally {
      done();
      persistNow();
      notify();
    }
  }

  /** Takes a record the engine answered with as the confirmed tab set. */
  function adoptRecord(record: WorkspaceRecord) {
    revision = record.revision;
    movedTo = record.movedTo ?? {};
    if (!record.workspace) {
      // The engine has nothing (or a file it could not read): this window's tab set becomes the first saved one.
      base = undefined;
      return;
    }
    base = record.workspace;
    rebuild();
  }

  /** The long poll: takes every revision another window writes, as it is written. */
  async function watch() {
    while (!stopped) {
      const abort = new AbortController();
      watchAbort = abort;
      try {
        const record = await client.wait(key, revision, abort.signal);
        if (stopped) return;
        retryDelay = retryFloorMs;
        // A write of this window's own is in flight: its answer decides; the next wait sees anything newer.
        if (inflight) { await settle; continue; }
        if (record.revision !== revision) {
          adoptRecord(record);
          persistSoon();
          if (pending.length || !base) scheduleSave(0);
        }
        if (status.phase === 'offline') setStatus({ phase: 'saving', error: undefined, code: undefined });
        setStatus({});
        settleIfIdle();
        notify();
      } catch (failure) {
        if (stopped) return;
        const error = failure instanceof WorkspaceSyncError ? failure : undefined;
        if (status.phase !== 'refused') setStatus({ phase: 'offline', error: error?.message ?? 'codeaf engine is not running', code: error?.code });
        notify();
        await pause(backoff());
      }
    }
  }

  /** First contact: read the engine's tab set, or offer this window's as the first one. */
  async function load() {
    try {
      const record = await client.get(key);
      if (stopped) return;
      loaded = true;
      if (record.workspace) {
        // The import seed is not replayed over the engine's tab set; the changes made on it are.
        adoptRecord(record);
        setStatus({ phase: pending.length ? 'saving' : 'saved', error: undefined, code: undefined });
      } else {
        revision = record.revision;
        base = undefined;
        setStatus({ phase: 'saving' });
      }
      if (pending.length || !base) scheduleSave(0);
    } catch (failure) {
      if (stopped) return;
      const error = failure instanceof WorkspaceSyncError ? failure : undefined;
      setStatus({ phase: 'offline', error: error?.message ?? 'codeaf engine is not running', code: error?.code });
      notify();
      // Until the engine answers this window works on its own copy; the watch loop retries the read.
      await pause(backoff());
      if (!stopped) return load();
      return;
    }
    persistNow();
    notify();
    void watch();
  }

  /** Detaches: ends the long poll and timers and saves the queue locally. Nothing queued is thrown away. */
  function stop() {
    if (stopped) return;
    stopped = true;
    watchAbort?.abort();
    wakeWatch?.();
    if (saveTimer !== undefined) clock.clear(saveTimer);
    saveTimer = undefined;
    persistNow();
  }

  return {
    /** The window's tab set: the shared tabs with this window's own focus. */
    getState: () => view,
    getStatus: () => status,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    dispatch,
    /** A paired transfer answer is authoritative; preserve this window's queued edits while adopting it. */
    receiveTransfer(record: WorkspaceRecord, incoming?: Pick<WindowLocal, 'focus' | 'scroll'>) {
      if (record.key !== key || record.revision < revision) return;
      if (incoming) local = { ...local, focus: { ...local.focus, ...incoming.focus }, scroll: { ...local.scroll, ...incoming.scroll } };
      adoptRecord(record); setStatus({}); persistNow(); notify();
      if (pending.length) scheduleSave(0);
    },
    /** Starts reading and mirroring. Returns the stop function. */
    start() {
      if (stopped) { stopped = false; void load(); }
      return stop;
    },
    stop,
    /** Try now: the engine may be back (the network came up, the window became visible, the person asked). */
    retry() {
      if (stopped) return;
      retryDelay = retryFloorMs;
      wakeWatch?.();
      if (loaded && (pending.length || !base)) scheduleSave(0);
    },
    /** The person has seen the overtaken count. */
    acknowledge() { if (status.overtaken) { status = { ...status, overtaken: 0 }; notify(); } },
    /** Saves the local copy at once (the page is going away). */
    persistNow,
    /** Where this window had a pane scrolled; window-local, never shared. */
    scrollOf: (paneId: string) => local.scroll[paneId],
    setScroll(paneId: string, top: number) {
      if (!Number.isFinite(top) || top < 0 || local.scroll[paneId] === Math.round(top)) return;
      local = { ...local, scroll: { ...local.scroll, [paneId]: Math.round(top) } };
      persistSoon();
    },
    /**
     * The focused-view handoff for "Move to new window" on the SAME place. Both windows mirror one tab set, so
     * the tab cannot be removed from this window without removing it from the new one too. This moves only THIS
     * window's focus off the tab (to its neighbour) and answers what the new window should focus. The tab set is
     * unchanged; nothing is queued.
     */
    handoff(tabId: string): { key: WorkspaceKey; tabId: string } | undefined {
      const tab = view.tabs.find(t => t.id === tabId || t.split?.panes.some(p => p.id === tabId));
      if (!tab) return undefined;
      if (view.activeId === tab.id && view.tabs.length > 1) {
        const order = visibleTabs(view);
        const at = order.findIndex(t => t.id === tab.id);
        const next = order[at + 1] ?? order[at - 1] ?? view.tabs.find(t => t.id !== tab.id)!;
        view = { ...view, activeId: next.id, recentIds: [next.id, ...view.recentIds.filter(id => id !== next.id)] };
        local = localOf(view, local);
        persistSoon();
        notify();
      }
      return { key, tabId: tab.id };
    },
    /**
     * Takes over the unconfirmed changes of a window that is gone, after this window's own; they are replayed with
     * the ids they were made with, so a change that did reach the engine is recognised and not made twice.
     */
    adopt(entries: readonly Entry[]) {
      if (!entries.length) return;
      for (const entry of entries) {
        const { next, ids, repeated } = apply(view, entry);
        if (!repeated && next !== view) view = next;
        pending.push({ ...entry, ids });
      }
      local = localOf(view, local);
      setStatus({});
      persistSoon();
      notify();
      scheduleSave(saveDelay);
    },
    /** This window's writer tag (and the local copy it saves under). */
    writer: () => writer,
    /** A browser tab that turned out to share its name with another live one takes a new name. */
    rename(next: string, save?: (state: Persisted) => void) { writer = next; persist = save; persistSoon(); },
    /** For tests and diagnostics: the confirmed revision and the unconfirmed queue. */
    inspect: () => ({ revision, base, pending: [...pending], inflight: inflight ? [...inflight] : null, local }),
  };
}
