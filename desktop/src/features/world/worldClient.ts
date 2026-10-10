import type { AttentionItem, EngineJob, PlacesRecord, WorkspaceRecord, WorldRecord, WorldRow } from './types.ts';

export type WorldTransport = (path: string, init: RequestInit) => Promise<Response>;
export type WorldClientOptions = {
 transport?: WorldTransport;
 retryMs?: number; maxRetryMs?: number;
 setTimer?: (fn: () => void, ms: number) => unknown;
 clearTimer?: (handle: unknown) => void;
};
const EMPTY_ROWS: readonly WorldRow[] = Object.freeze([]);
const EMPTY_ITEMS: readonly AttentionItem[] = Object.freeze([]);
const EMPTY_JOBS: readonly EngineJob[] = Object.freeze([]);
const same = (a: unknown, b: unknown) => a === b || JSON.stringify(a) === JSON.stringify(b);
const stable = <T>(previous: T, next: T): T => same(previous, next) ? previous : next;

/** Authentication stays in the shared engine door, never in renderer storage or an SSE URL. */
async function engineTransport(path: string, init: RequestInit): Promise<Response> {
 const { fetchEngine } = await import('../chat/engine-client.ts');
 return fetchEngine(path, init, true);
}

/** A factory supplies isolated clocks and streams to tests; views share worldClient below. */
export function createWorldClient(options: WorldClientOptions = {}) {
 const transport = options.transport ?? engineTransport;
 const first = options.retryMs ?? 1000, cap = options.maxRetryMs ?? 30_000;
 const setTimer = options.setTimer ?? ((fn, ms) => setTimeout(fn, ms));
 const clearTimer = options.clearTimer ?? (handle => clearTimeout(handle as ReturnType<typeof setTimeout>));
 let rows: readonly WorldRow[] = EMPTY_ROWS, items: readonly AttentionItem[] = EMPTY_ITEMS;
 let jobs = new Map<string, readonly EngineJob[]>();
 let places: PlacesRecord | undefined;
 let workspaces = new Map<string, WorkspaceRecord>();
 let seq = 0, epoch: string | undefined;
 let hasRecord = false;
 const listeners = new Map<symbol, () => void>();
 let run: { abort: AbortController; timer?: unknown; delay: number } | undefined;

 function replaceRows(next: readonly WorldRow[]) {
  const previous = new Map(rows.map(row => [row.chatId, row]));
  rows = stable(rows, [...next].sort((a, b) => a.chatId.localeCompare(b.chatId)).map(row => stable(previous.get(row.chatId) ?? row, row)));
 }

 function apply(record: WorldRecord) {
  if (!Number.isSafeInteger(record.seq) || record.seq < 0 || typeof record.epoch !== 'string') throw new Error('Invalid world stream cursor.');
  const newEpoch = epoch !== record.epoch;
  if (record.type !== 'reset' && newEpoch && hasRecord) return;
  if (hasRecord && !newEpoch && record.seq <= seq) return;
  // The previous feed used session ids and boolean question counts; accepting it would corrupt this store.
  if (record.type === 'world' || record.type === 'reset') {
   if (!Array.isArray(record.payload.rows) || record.payload.rows.some(row => typeof row.chatId !== 'string' || !row.chatId || typeof row.needsYou !== 'number')) throw new Error('Invalid world rows.');
  }
  if (record.type === 'attention' || record.type === 'reset') {
   if (!Array.isArray(record.payload.items) || record.payload.items.some(item => typeof item.id !== 'string' || typeof item.chatId !== 'string')) throw new Error('Invalid world attention.');
  }
  const oldRows = rows, oldItems = items, oldJobs = jobs, oldPlaces = places, oldWorkspaces = workspaces;
  switch (record.type) {
   case 'reset': {
    replaceRows(record.payload.rows);
    items = stable(items, record.payload.items);
    jobs = new Map((record.payload.jobs ?? []).map(value => [value.chatId, stable(jobs.get(value.chatId) ?? EMPTY_JOBS, value.jobs)]));
    places = stable(places, record.payload.places);
    workspaces = new Map((record.payload.workspaces ?? []).map(value => [value.key, stable(workspaces.get(value.key) ?? value, value)]));
    break;
   }
   case 'world': {
    const next = new Map(rows.map(row => [row.chatId, row]));
    for (const row of record.payload.rows) next.set(row.chatId, row);
    for (const id of record.payload.removed) next.delete(id);
    replaceRows([...next.values()]);
    break;
   }
   case 'attention': items = stable(items, record.payload.items); break;
   case 'jobs': {
    const next = stable(jobs.get(record.payload.chatId) ?? EMPTY_JOBS, record.payload.jobs);
    if (next !== (jobs.get(record.payload.chatId) ?? EMPTY_JOBS)) jobs = new Map(jobs).set(record.payload.chatId, next);
    break;
   }
   case 'places': places = stable(places, record.payload); break;
   case 'workspace': {
    const next = stable(workspaces.get(record.payload.key), record.payload);
    if (next !== workspaces.get(record.payload.key)) workspaces = new Map(workspaces).set(record.payload.key, record.payload);
    break;
   }
  }
  seq = record.seq; epoch = record.epoch; hasRecord = true;
  // Cursors advance even on an unchanged payload; selectors only notify for data changes.
  if (oldRows !== rows || oldItems !== items || !same(oldJobs && [...oldJobs], [...jobs]) || oldPlaces !== places || !same([...oldWorkspaces], [...workspaces])) {
   for (const listener of [...listeners.values()]) listener();
  }
 }

 async function read(current: NonNullable<typeof run>) {
  const query = new URLSearchParams({ after: String(seq) });
  if (epoch !== undefined) query.set('epoch', epoch);
  const response = await transport(`/events?${query}`, { signal: current.abort.signal, headers: { Accept: 'text/event-stream' } });
  if (!response.ok || !response.body) throw new Error('The engine world stream is unavailable.');
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
   while (run === current) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let boundary: RegExpExecArray | null;
    while ((boundary = /\r?\n\r?\n/.exec(buffer))) {
     const frame = buffer.slice(0, boundary.index);
     buffer = buffer.slice(boundary.index + boundary[0].length);
     const data = frame.split(/\r?\n/).filter(line => line.startsWith('data:')).map(line => line.slice(5).replace(/^ /, '')).join('\n');
     if (!data || run !== current) continue;
     apply(JSON.parse(data) as WorldRecord);
     current.delay = first;
    }
    // A corrupt unframed stream must not grow the window's memory indefinitely.
    if (buffer.length > 1_048_576) throw new Error('Invalid world stream frame.');
   }
  } finally {
   await reader.cancel().catch(() => undefined);
   reader.releaseLock();
  }
 }

 function connect(current: NonNullable<typeof run>) {
  current.timer = undefined;
  void read(current).catch(() => undefined).then(() => {
   if (run !== current || current.abort.signal.aborted) return;
   const delay = current.delay;
   current.delay = Math.min(cap, delay * 2);
   current.timer = setTimer(() => connect(current), delay);
  });
 }

 const api = {
  rows: () => rows,
  row: (chatId: string) => rows.find(row => row.chatId === chatId),
  attention: () => items,
  jobs: (chatId: string) => jobs.get(chatId) ?? EMPTY_JOBS,
  lastPlaces: () => places,
  lastWorkspace: (key: string) => workspaces.get(key),
  subscribe(listener: () => void): () => void {
   const key = Symbol();
   listeners.set(key, listener);
   if (!run) {
    run = { abort: new AbortController(), delay: first };
    connect(run);
   }
   return () => {
    if (!listeners.delete(key) || listeners.size || !run) return;
    const current = run; run = undefined;
    if (current.timer !== undefined) clearTimer(current.timer);
    current.abort.abort();
   };
  },
 };
 return api;
}

export type WorldClient = ReturnType<typeof createWorldClient>;
/** Module scope is window scope: all selectors and React roots share this connection. */
export const worldClient = createWorldClient();
