/**
 * Typed client for the engine's tab-set routes (internal/desktopbridge/workspaces.go).
 *
 * It owns no state and no policy: each call is one request, each answer is validated before it is returned, and a
 * refusal arrives as a WorkspaceSyncError carrying the engine's own sentence and code. A refused write is NOT an
 * error: it resolves as `{ kind: 'conflict', current }` so the caller can replay its intent over the current tab set.
 *
 * The transport is injectable so tests (and the browser dev proxy) can stand in for the native connection. The
 * default asks Tauri for the loopback URL and bearer token exactly as the Places client does; the token is never
 * stored, logged or put in a URL.
 */
import { parseShared, type SharedWorkspace } from './shared.ts';

export type WorkspaceKey = 'now' | `pl_${string}`;
export const isWorkspaceKey = (value: unknown): value is WorkspaceKey => typeof value === 'string' && /^(now|pl_[0-9a-f]{16})$/.test(value);

/** One place's tab set as the engine holds it. Revision 0 with no workspace: nothing saved yet. */
export type WorkspaceRecord = {
  key: WorkspaceKey;
  revision: number;
  updatedAt?: string;
  writer?: string;
  workspace?: SharedWorkspace;
  /** The saved file could not be read; it is set aside, never deleted, by the next write. */
  damaged?: boolean;
};

export type PutResult = { kind: 'saved'; record: WorkspaceRecord } | { kind: 'conflict'; current: WorkspaceRecord };

export class WorkspaceSyncError extends Error {
  readonly status: number;
  /** The engine's slug: invalid, too_large, busy, newer, unknown_key, origin, unavailable; '' when it sent none. */
  readonly code: string;
  /** Nothing answered: the engine is not running or the proxy cannot reach it. */
  readonly unreachable: boolean;
  constructor(message: string, status = 0, code = '', unreachable = false) {
    super(message);
    this.name = 'WorkspaceSyncError';
    this.status = status; this.code = code; this.unreachable = unreachable;
  }
}

export type WorkspaceRequest = { method: 'GET' | 'PUT'; body?: unknown; signal?: AbortSignal };
/** Sends one request to /api/engine{path}; answers the status and the parsed JSON body (null when there was none). */
export type WorkspaceTransport = (path: string, request: WorkspaceRequest) => Promise<{ status: number; body: unknown }>;

/** A plain request gives up after this; a long poll is held by the engine for at most 25s. */
export const WORKSPACE_REQUEST_TIMEOUT_MS = 40_000;

const isObject = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);

/** Validates a record from the engine. A workspace that does not validate is a protocol error, never an empty set. */
export function parseRecord(value: unknown, key: WorkspaceKey): WorkspaceRecord {
  if (!isObject(value) || value.key !== key || !Number.isSafeInteger(value.revision) || (value.revision as number) < 0) throw new WorkspaceSyncError('The engine returned an invalid tab set.');
  const record: WorkspaceRecord = { key, revision: value.revision as number };
  if (typeof value.updatedAt === 'string') record.updatedAt = value.updatedAt;
  if (typeof value.writer === 'string') record.writer = value.writer;
  if (value.damaged === true) record.damaged = true;
  if (value.workspace !== undefined && value.workspace !== null) {
    const workspace = parseShared(value.workspace);
    if (!workspace) throw new WorkspaceSyncError('The engine returned an invalid tab set.');
    record.workspace = workspace;
  } else if (record.revision > 0) throw new WorkspaceSyncError('The engine returned an invalid tab set.');
  return record;
}

function refusal(status: number, body: unknown): WorkspaceSyncError {
  const e = isObject(body) ? body : {};
  const code = typeof e.code === 'string' ? e.code : '';
  if (typeof e.error !== 'string' && status >= 500) return new WorkspaceSyncError('codeaf engine is not running', status, code, true);
  return new WorkspaceSyncError(typeof e.error === 'string' ? e.error : `The engine request failed (${status}).`, status, code);
}

export function createWorkspaceClient(transport: WorkspaceTransport = defaultTransport) {
  const path = (key: WorkspaceKey) => `/workspaces/${key}`;
  return {
    /** The current tab set of `key`. */
    async get(key: WorkspaceKey, signal?: AbortSignal): Promise<WorkspaceRecord> {
      const { status, body } = await transport(path(key), { method: 'GET', signal });
      if (status !== 200) throw refusal(status, body);
      return parseRecord(body, key);
    },
    /** Answers as soon as the revision is not `after`, or with the unchanged record after the engine's hold (25s). */
    async wait(key: WorkspaceKey, after: number, signal?: AbortSignal): Promise<WorkspaceRecord> {
      const { status, body } = await transport(`${path(key)}?after=${after}&wait=1`, { method: 'GET', signal });
      if (status !== 200) throw refusal(status, body);
      return parseRecord(body, key);
    },
    /** Saves `workspace` if the engine still holds `revision`; otherwise answers the current record. */
    async put(key: WorkspaceKey, revision: number, writer: string, workspace: SharedWorkspace, signal?: AbortSignal): Promise<PutResult> {
      const { status, body } = await transport(path(key), { method: 'PUT', body: { revision, writer, workspace }, signal });
      if (status === 200) return { kind: 'saved', record: parseRecord(body, key) };
      if (status === 409 && isObject(body) && body.code === 'conflict') return { kind: 'conflict', current: parseRecord(body.current, key) };
      throw refusal(status, body);
    },
  };
}
export type WorkspaceClient = ReturnType<typeof createWorkspaceClient>;

// ---- the default transport -------------------------------------------------

type Connection = { url: string; token: string };

async function defaultTransport(path: string, request: WorkspaceRequest): Promise<{ status: number; body: unknown }> {
  const { invoke, isTauri } = await import('@tauri-apps/api/core');
  const headers = new Headers({ Accept: 'application/json' });
  let url = `/api/engine${path}`;
  if (isTauri()) {
    const connection = await invoke<Connection>('engine_connection').catch(() => { throw new WorkspaceSyncError('codeaf engine is not running', 0, '', true); });
    const base = new URL(connection.url);
    if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname)) throw new WorkspaceSyncError('The local engine announced an invalid connection.');
    headers.set('Authorization', `Bearer ${connection.token}`);
    url = `${base.origin}/api/engine${path}`;
  }
  const init: RequestInit = { method: request.method, headers, cache: 'no-store' };
  if (request.body !== undefined) { init.body = JSON.stringify(request.body); headers.set('Content-Type', 'application/json'); }
  const clock = new AbortController();
  const timer = setTimeout(() => clock.abort(), WORKSPACE_REQUEST_TIMEOUT_MS);
  const forward = () => clock.abort();
  request.signal?.addEventListener('abort', forward, { once: true });
  init.signal = clock.signal;
  let response: Response;
  try { response = await fetch(url, init); }
  catch (error) {
    if (request.signal?.aborted) throw error;
    throw new WorkspaceSyncError('codeaf engine is not running', 0, '', true);
  } finally { clearTimeout(timer); request.signal?.removeEventListener('abort', forward); }
  // The dev proxy answers 502/504 with no JSON when the engine is down: that is "not running", not a refusal.
  const body: unknown = await response.json().catch(() => null);
  if (!isObject(body) && response.status >= 500) throw new WorkspaceSyncError('codeaf engine is not running', response.status, '', true);
  return { status: response.status, body };
}
