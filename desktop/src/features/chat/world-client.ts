// Wire client for the engine-wide world feed (internal/desktopbridge/world*.go):
// GET /world and the SSE /events?after=N. It holds no state and starts no timer;
// world-store.ts owns the one connection and its reconnects.

export type WorldTask = { id: string; label: string; state: string; phase?: string };
export type WorldRow = {
 session: string; title: string; project: string; workspace?: string;
 /** The conversation's journal, the path a window reattaches with; absent when unknown. */
 sessionFile?: string;
 /** Filesystem folders the conversation referred to. NOT design-graph places. */
 sourceFolders: string[]; model?: string;
 /** The presence file's own word ("working", "waiting on you", "idle"), or "open" / "closed". */
 state: string;
 live: boolean; open: boolean; running: boolean; needsYou: boolean; failed: number;
 tasks: { running: number; incomplete: number; done: number; failed: number; total: number };
 liveTasks?: WorldTask[]; at?: string; archived?: boolean; deletionPending?: boolean; reason?: string;
};
export type AttentionItem = {
 key: string; session: string; kind: string; id?: number; text: string; sourceFolders: string[];
 title?: string; answerable: boolean; asked?: string;
};
export type WorldFull = { rows: WorldRow[]; items: AttentionItem[] };
export type WorldRecord =
 | { seq: number; type: 'reset'; at: string; payload: WorldFull }
 | { seq: number; type: 'world'; at: string; payload: { rows: WorldRow[]; removed: string[] } }
 | { seq: number; type: 'attention'; at: string; payload: { items: AttentionItem[] } };

export class WorldError extends Error {
 readonly status: number;
 /** True when nothing answered: the engine is not running or cannot be reached. */
 readonly unreachable: boolean;
 constructor(message: string, status = 0, unreachable = false) { super(message); this.name = 'WorldError'; this.status = status; this.unreachable = unreachable; }
}

/** Sends one authenticated engine request. Injected so tests and the shell can supply their own. */
export type WorldTransport = (path: string, init: { signal?: AbortSignal; headers?: Record<string, string> }) => Promise<Response>;

type Connection = { url: string; token: string };

/** The packaged window asks Tauri for the loopback URL and token; the browser dev server proxies. The token never reaches storage, URLs or logs. */
export const engineTransport: WorldTransport = async (path, init) => {
 const headers = new Headers(init.headers);
 let url = `/api/engine${path}`;
 const core = await import('@tauri-apps/api/core');
 if (core.isTauri()) {
  const connection = await core.invoke<Connection>('engine_connection');
  const base = new URL(connection.url);
  if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname)) throw new WorldError('The local engine announced an invalid connection.');
  headers.set('Authorization', `Bearer ${connection.token}`);
  url = `${base.origin}/api/engine${path}`;
 }
 try { return await fetch(url, { headers, cache: 'no-store', signal: init.signal }); }
 catch (error) {
  if (init.signal?.aborted) throw error;
  throw new WorldError('codeaf engine is not running', 0, true);
 }
};

async function failure(response: Response): Promise<WorldError> {
 const body = await response.json().catch(() => null) as { error?: unknown } | null;
 const named = typeof body?.error === 'string';
 return new WorldError(named ? String(body?.error) : `The engine request failed (${response.status}).`, response.status, !named && response.status >= 500);
}

/** One current reading: the state and the sequence it is consistent with. */
export async function fetchWorld(transport: WorldTransport, signal?: AbortSignal): Promise<{ seq: number } & WorldFull> {
 const response = await transport('/world', { signal, headers: { Accept: 'application/json' } });
 if (!response.ok) throw await failure(response);
 const value = await response.json().catch(() => null) as ({ seq?: unknown } & Partial<WorldFull>) | null;
 if (!value || !Number.isSafeInteger(value.seq) || !Array.isArray(value.rows) || !Array.isArray(value.items)) throw new WorldError('The engine returned an invalid world.');
 return { seq: value.seq as number, rows: value.rows, items: value.items };
}

/** Validates one stream record; anything else is a protocol error, never silently skipped. */
export function parseWorldRecord(text: string): WorldRecord {
 let value: unknown;
 try { value = JSON.parse(text); } catch { throw new WorldError('The engine sent an invalid world record.'); }
 const record = value as { seq?: unknown; type?: unknown; payload?: Record<string, unknown> } | null;
 if (!record || typeof record !== 'object' || !Number.isSafeInteger(record.seq) || (record.seq as number) < 0 || !record.payload || typeof record.payload !== 'object') throw new WorldError('The engine sent an invalid world record.');
 const p = record.payload;
 if (record.type === 'reset' && Array.isArray(p.rows) && Array.isArray(p.items)) return value as WorldRecord;
 if (record.type === 'world' && Array.isArray(p.rows) && Array.isArray(p.removed)) return value as WorldRecord;
 if (record.type === 'attention' && Array.isArray(p.items)) return value as WorldRecord;
 throw new WorldError('The engine sent an unknown world record.');
}

/**
 * Reads the world stream from `after` until it ends or the signal aborts, handing
 * each record to onRecord in order. Resolves only when aborted; a stream the
 * engine closed rejects so the caller can reconnect from its cursor. Aborting
 * detaches the reader and nothing else; no engine work is stopped.
 */
export async function watchWorld(transport: WorldTransport, after: number, onRecord: (record: WorldRecord) => void, signal: AbortSignal): Promise<void> {
 const response = await transport(`/events?after=${after}`, { signal, headers: { Accept: 'text/event-stream' } });
 if (!response.ok) throw await failure(response);
 if (!response.body || !response.headers.get('content-type')?.includes('text/event-stream')) throw new WorldError('The engine did not open a world stream.');
 const reader = response.body.getReader();
 const decoder = new TextDecoder();
 let buffer = '';
 let data: string[] = [];
 const dispatch = () => {
  if (signal.aborted || !data.length) { data = []; return; }
  const text = data.join('\n'); data = [];
  onRecord(parseWorldRecord(text));
 };
 const consume = (final = false) => {
  let index: number;
  while ((index = buffer.indexOf('\n')) >= 0) {
   const line = buffer.slice(0, index).replace(/\r$/, ''); buffer = buffer.slice(index + 1);
   if (!line) dispatch();
   else if (line === 'data') data.push('');
   else if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
  }
  if (final) { const line = buffer.replace(/\r$/, ''); buffer = ''; if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, '')); dispatch(); }
 };
 try {
  while (!signal.aborted) {
   const chunk = await reader.read();
   if (chunk.done) { buffer += decoder.decode(); consume(true); if (!signal.aborted) throw new WorldError('The engine world stream closed.', 0, true); break; }
   buffer += decoder.decode(chunk.value, { stream: true }); consume();
  }
 } catch (error) { if (!signal.aborted) throw error; }
 finally { await reader.cancel().catch(() => undefined); reader.releaseLock(); }
}
