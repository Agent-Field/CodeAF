// The one world-feed subscription for the window. Components subscribe through
// it (or a hook over useSyncExternalStore); none of them run an interval. The
// first subscriber opens a single stream, the last closes it, and a lost stream
// reconnects from the last sequence it applied, so nothing is replayed twice.

import { WorldError, engineTransport, watchWorld } from './world-client.ts';
import type { AttentionItem, WorldRecord, WorldRow, WorldTransport } from './world-client.ts';

export type WorldStatus = 'idle' | 'connecting' | 'live' | 'unavailable';
/** Immutable; a new object is produced on every change so it is a valid store snapshot. */
export type WorldState = {
 status: WorldStatus;
 /** Why the feed is unavailable, in the engine's own words; absent otherwise. */
 error?: string;
 epoch?: string;
 seq: number;
 rows: readonly WorldRow[];
 items: readonly AttentionItem[];
};

export type WorldStoreOptions = {
 transport?: WorldTransport;
 /** Reconnect delays: first, then doubling to max. */
 retryMs?: number; maxRetryMs?: number;
 setTimer?: (fn: () => void, ms: number) => unknown;
 clearTimer?: (handle: unknown) => void;
};

const EMPTY: WorldState = { status: 'idle', seq: 0, rows: [], items: [] };

export function applyWorldRecord(state: WorldState, record: WorldRecord): WorldState {
 // A reset is the engine saying the cursor is unusable; it is applied even at an equal seq.
 if (record.epoch && state.epoch && record.epoch !== state.epoch && record.type !== 'reset') return state;
 if (record.type !== 'reset' && record.seq <= state.seq) return state;
 if (record.type === 'reset') return { ...state, status: 'live', error: undefined, epoch: record.epoch, seq: record.seq, rows: record.payload.rows, items: record.payload.items };
 if (record.type === 'attention') return { ...state, seq: record.seq, items: record.payload.items };
 // A jobs roll-up is not a conversation row. Advance the cursor so the feed stays up; this store does not keep the shelf.
 if (record.type === 'jobs') return { ...state, seq: record.seq };
 const removed = new Set(record.payload.removed);
 const changed = new Map(record.payload.rows.map(row => [row.session, row] as const));
 const rows = state.rows.filter(row => !removed.has(row.session)).map(row => changed.get(row.session) ?? row);
 const known = new Set(rows.map(row => row.session));
 for (const row of record.payload.rows) if (!known.has(row.session)) rows.push(row);
 return { ...state, seq: record.seq, rows };
}

export function createWorldStore(options: WorldStoreOptions = {}) {
 const transport = options.transport ?? engineTransport;
 const first = options.retryMs ?? 1000, cap = options.maxRetryMs ?? 30_000;
 const setTimer = options.setTimer ?? ((fn, ms) => setTimeout(fn, ms));
 const clearTimer = options.clearTimer ?? (handle => clearTimeout(handle as ReturnType<typeof setTimeout>));
 let state: WorldState = EMPTY;
 const listeners = new Set<() => void>();
 let refs = 0;
 let run: { abort: AbortController; timer?: unknown; delay: number } | null = null;

 const set = (next: WorldState) => { if (next !== state) { state = next; for (const listener of [...listeners]) listener(); } };

 function connect(current: NonNullable<typeof run>) {
  if (run !== current || current.abort.signal.aborted) return;
  current.timer = undefined;
  // Keep what was last known while reconnecting; only the status changes.
  if (state.status !== 'live') set({ ...state, status: 'connecting' });
  watchWorld(transport, state.seq, record => {
   if (run !== current) return;
   current.delay = first;
   set(applyWorldRecord(state, record));
  }, current.abort.signal, state.epoch).catch((error: unknown) => {
   if (run !== current || current.abort.signal.aborted) return;
   const denied = error instanceof WorldError && (error.status === 401 || error.status === 403);
   const message = error instanceof Error ? error.message : 'The engine world feed failed.';
   set({ ...state, status: 'unavailable', error: message });
   // A refused connection will not heal by itself; wait for focus or a fresh subscription.
   if (denied) return;
   const wait = current.delay;
   current.delay = Math.min(cap, wait * 2);
   current.timer = setTimer(() => connect(current), wait);
  });
  // A stream that opened applies its first record before status can be read as live.
 }

 // Returning to the window is the one moment worth reconnecting early; there is no interval.
 const onFocus = () => { if (typeof document === 'undefined' || document.visibilityState !== 'hidden') api.retryNow(); };
 function start() {
  const current = { abort: new AbortController(), delay: first };
  run = current;
  if (typeof window !== 'undefined') { window.addEventListener('focus', onFocus); document.addEventListener('visibilitychange', onFocus); }
  connect(current);
 }
 function stop() {
  const current = run; run = null;
  if (!current) return;
  if (typeof window !== 'undefined') { window.removeEventListener('focus', onFocus); document.removeEventListener('visibilitychange', onFocus); }
  if (current.timer !== undefined) clearTimer(current.timer);
  current.abort.abort();
  // Keep the last known rows for the next subscriber; they are labelled by status, not hidden.
  if (state.status !== 'idle') set({ ...state, status: 'idle', error: undefined });
 }

 const api = {
  getState: () => state,
  /** Ref-counted: the first subscriber opens the stream, the last one closes it. */
  subscribe(listener: () => void): () => void {
   listeners.add(listener);
   refs++;
   if (refs === 1) start();
   let done = false;
   return () => {
    if (done) return;
    done = true;
    listeners.delete(listener);
    refs--;
    if (refs === 0) stop();
   };
  },
  /** Window focus or an explicit Retry: reconnect now instead of waiting out the backoff. */
  retryNow() {
   const current = run;
   if (!current || state.status === 'live' || state.status === 'connecting') return;
   if (current.timer !== undefined) clearTimer(current.timer);
   current.delay = first;
   connect(current);
  },
  subscribers: () => refs,
 };
 return api;
}

/** The window's single store. */
export const worldStore = createWorldStore();
