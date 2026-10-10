import { engineFetch } from '../../design/engineFetch.ts';
import { isTail, mergeTail } from './snapshotMerge.ts';
import { invoke, isTauri } from '@tauri-apps/api/core';

/** Canonical read-only transport from session.PlanTaskRow. No engine policy lives here. */
export type PlanTaskRow = {
 ID: string; Title: string; Status: string; Parent?: string; Hold?: string;
 Stopped?: boolean; Interrupted?: boolean; Archived?: boolean;
 Paused?: boolean; Waiting?: boolean; Waits?: readonly string[];
 Seat?: string; Note?: string; Steps?: number; Started?: string; Ended?: string;
 Program?: string; Stage?: string;
 Live?: { Step?: number; Command?: string; Since?: string };
 Done?: number; Running?: number; Queued?: number; Failed?: number; Total?: number;
};
/** One quote-aware command part (session.PlanCommandPart; no json tags, so PascalCase). */
export type PlanCommandPart = { Command?: string; Separator?: string; Start?: number; End?: number; SepEnd?: number; RecordAddressed?: boolean; RunCopyPrefix?: boolean };
/** One recorded worker step, as session.PlanStep's json tags spell it. */
export type PlanStep = {
 kind?: string; step?: number; command?: string; observation?: string;
 /** Path of the file holding the whole output; observation is only its head. */
 full_output?: string;
 writes?: string[]; children?: string[];
 not_run?: boolean; refused?: boolean;
 parts?: PlanCommandPart[];
 observation_head_withheld?: boolean;
};
export type PlanTaskPage = {
 Row: PlanTaskRow; Description?: string; Result?: string; Checks?: readonly string[];
 Notes?: readonly { Author?: string; Person?: boolean; Body: string; At?: string }[];
 Steps?: readonly PlanStep[];
};

/** What every model role starts on; the model is a setting now, so nothing checks a snapshot against it. */
export const ENGINE_MODEL = 'deepseek/deepseek-v4.1-flash';
export type EngineQuestionBlock = { kind: string; title?: string; body?: string; rows?: string[][]; path?: string };
export type EngineQuestion = {
 id: number; ref?: string; kind: string; ask: string; form?: string; head: string; reason?: string;
 options?: { key: string; label: string; body?: string; consequence?: string; safe?: boolean; widening?: boolean; blocks?: EngineQuestionBlock[]; dimensions?: Record<string, string> }[];
 input?: { kind?: string; prompt?: string; secret?: boolean; blanks?: { label: string; kind?: string; default?: string; choices?: string[] }[]; dial?: { min: number; max: number; default: number; labels?: string[] } };
 attach?: EngineQuestionBlock[]; scope?: string[]; asked?: string; deadline?: string;
 pick?: { key: string; reason?: string; confidence?: 'sure' | 'fairly' | 'unsure'; wouldChange?: string };
 stakes?: 'reversible' | 'costly' | 'irreversible';
 blocking?: { turn?: boolean; tasks?: string[] };
 asker?: { kind?: 'model' | 'engine' | 'task' | 'surface' | 'window'; name?: string };
 subject?: { kind?: string; id?: number; callId?: string; ref?: string; name?: string };
 batch?: string; later?: boolean; clarificationDepth?: number;
 policy?: { kind?: 'ask' | 'recommend-then-auto' | 'decide'; after?: number }; // after: nanoseconds, a Go duration
 withdrawn?: { reason?: string; by?: string; at?: string };
};
export type EngineAnswer = { kind: string; id: number; ref?: string; key: string; picked?: string[]; change?: string; blanks?: Record<string,string>; scope?: string; dial?: number; decidedBy?: 'person' | 'dial' | 'record' | 'asker' | 'window'; comments?: Record<string,string> };
export type EngineEntry = {
 Role: 'user' | 'assistant' | 'tool' | 'note' | 'aside'; Text: string;
 Answer?: boolean; Addressed?: boolean; Interrupted?: boolean;
 Tool?: string; Hint?: string; CallID?: string; Answered?: boolean; Failed?: boolean;
 Args?: string; Output?: string;
 /** The engine left a large Output out of the snapshot; fetch it with readToolResult when the view needs it. */
 OutputOmitted?: boolean; OutputBytes?: number;
 Caption?: string; CaptionCategory?: string;
 TaskIDs?: string[] | null;
 /** What wrote an aside: "task" | "job" | "watch" | "resume"; absent when unknown. */
 AsideKind?: string;
 /** Exact context-mutation receipts; absent for older/ambiguous notes. */
 UndoReceipts?: string[];
 /** A job aside's own short name (its label or command); absent when it has none. */
 AsideTitle?: string;
 /** Set on a user line typed into the running turn rather than starting one. */
 Steer?: { At?: string; Consumed?: boolean; Landing?: string } | null;
};
export type EngineTaskRow = PlanTaskRow & { Depth?: number; USD?: number; Model?: string; Tokens?: number; LiveParts?: unknown[]; Folder?: string; TrajectoryPath?: string };
/** One message waiting behind the running turn. The id names it to edit, move and remove until its turn starts. */
export type EngineQueued = { id: string; text: string };
/** Where a chat started in a place works, present only when that place listed a folder. */
export type WorkingFolderReport = {
 from: 'place' | 'launch';
 path?: string;
 label?: string;
 skipped?: { sourceId: string; ref: string; reason: string }[];
 note?: string;
};
export type EngineSnapshot = {
 id: string; sessionFile: string; workspace: string; model: string; persistent: boolean;
 running: boolean; needsPerson: boolean; questions?: EngineQuestion[]; planError?: string; entries: EngineEntry[]; tasks: EngineTaskRow[];
 usage: { Input: number; Output: number; CostUSD: number; Duration: number; Turns: number; Calls?: number; CacheRead?: number; CacheWrite?: number };
 /** Messages queued behind the running turn, in the order they will run. */
 queue?: EngineQueued[];
 title: string; seq: number; updatedAt?: string;
 /** Absent when the place listed no folder, and on a reopened saved conversation. */
 workingFolder?: WorkingFolderReport;
};
export type EngineEvent = { kind: string; text: string; tool: string; hint: string; error?: string; raw: Record<string, unknown> };
export type EngineTaskPage = PlanTaskPage & { Folder?: string; Live?: PlanTaskRow['Live']; Children?: EngineTaskRow[] | null; WaitRows?: EngineTaskRow[] | null; Program?: unknown };
export class EngineError extends Error {
 readonly status: number;
 /** True when nothing answered: the engine is not running or the proxy cannot reach it. */
 readonly unreachable: boolean;
 constructor(message: string, status = 0, unreachable = false) { super(message); this.name = 'EngineError'; this.status = status; this.unreachable = unreachable; }
}
type Connection = { url: string; token: string; model: string };
async function endpoint(path: string): Promise<{ url: string; headers: Headers }> {
 const headers = new Headers({ Accept: 'application/json' });
 if (!isTauri()) return { url: `/api/engine${path}`, headers };
 const connection = await invoke<Connection>('engine_connection');
 if (typeof connection.model !== 'string' || !connection.model) throw new EngineError('The local engine announced no model.');
 const base = new URL(connection.url);
 if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname)) throw new EngineError('The local engine announced an invalid connection.');
 headers.set('Authorization', `Bearer ${connection.token}`);
 return { url: `${base.origin}/api/engine${path}`, headers };
}
/**
 * How long one request (never a stream) may wait for the engine to answer. A
 * request the browser never sends, or the engine never answers, must surface as
 * the unreachable line with the draft kept, never as a send that hangs silently.
 */
export const ENGINE_REQUEST_TIMEOUT_MS = 30_000;
/** Only a stream reader holds its connection open, so only it runs without the request clock. */
export async function fetchEngine(path: string, init?: RequestInit, stream = false): Promise<Response> {
 const target = await endpoint(path);
 const headers = target.headers;
 new Headers(init?.headers).forEach((value, name) => headers.set(name, value));
 if (init?.body) headers.set('Content-Type', 'application/json');
 const clock = new AbortController();
 const timer = stream ? undefined : setTimeout(() => clock.abort(), ENGINE_REQUEST_TIMEOUT_MS);
 let response: Response;
 try { response = await engineFetch(target.url, { ...init, headers, cache: 'no-store', signal: stream ? init?.signal : clock.signal }); }
 catch (error) {
  if (init?.signal?.aborted) throw error;
  throw new EngineError('codeaf engine is not running', 0, true);
 }
 finally { clearTimeout(timer); }
 if (!response.ok) {
  const body = await response.json().catch(() => null) as { error?: unknown } | null;
  // The engine always explains itself in JSON; a bare gateway failure means nothing answered.
  if (typeof body?.error !== 'string' && response.status >= 500) throw new EngineError('codeaf engine is not running', response.status, true);
  throw new EngineError(typeof body?.error === 'string' ? body.error : `The engine request failed (${response.status}).`, response.status);
 }
 return response;
}
/** Shared JSON requests keep authentication, PUT/If-Match headers and bridge errors on one transport. */
export async function engineJson<T>(path: string, init?: RequestInit): Promise<T> {
 return (await fetchEngine(path, init)).json() as Promise<T>;
}
function snapshotFrom(value: unknown): EngineSnapshot {
 if (!value || typeof value !== 'object') throw new EngineError('The engine returned an invalid session.');
 const s = value as EngineSnapshot;
 if (typeof s.model !== 'string' || !s.model) throw new EngineError('The engine returned a conversation without a model.');
 if (typeof s.id !== 'string' || !s.id || typeof s.sessionFile !== 'string' || typeof s.workspace !== 'string' || typeof s.running !== 'boolean' || typeof s.needsPerson !== 'boolean' || typeof s.persistent !== 'boolean' || typeof s.title !== 'string' || !Number.isSafeInteger(s.seq) || s.seq < 0 || (s.entries !== null && !Array.isArray(s.entries)) || (s.tasks !== null && !Array.isArray(s.tasks)) || !s.usage || typeof s.usage !== 'object') throw new EngineError('The engine returned an invalid session.');
 if (!s.persistent || !s.sessionFile) throw new EngineError('The engine session is not persistent; it cannot safely survive closing this tab.');
 const entries = s.entries ?? [];
 if (!entries.every(entry => entry && ['user', 'assistant', 'tool', 'note', 'aside'].includes(entry.Role) && typeof entry.Text === 'string')) throw new EngineError('The engine returned invalid conversation history.');
 const tasks = s.tasks ?? [];
 if (!tasks.every(row => row && typeof row.ID === 'string' && typeof row.Title === 'string' && typeof row.Status === 'string' && (row.Waits == null || (Array.isArray(row.Waits) && row.Waits.every(id => typeof id === 'string'))))) throw new EngineError('The engine returned an invalid task plan.');
 if (s.planError !== undefined && typeof s.planError !== 'string') throw new EngineError('The engine returned an invalid plan read status.');
 const questions = s.questions ?? [];
 if (!Array.isArray(questions) || !questions.every(q => q && Number.isSafeInteger(q.id) && q.id >= 0 && (q.id > 0 || typeof q.ref === 'string' && !!q.ref) && typeof q.kind === 'string' && typeof q.ask === 'string' && typeof q.head === 'string' && (q.options == null || Array.isArray(q.options) && q.options.every(option => option && typeof option.key === 'string' && typeof option.label === 'string')))) throw new EngineError('The engine returned an invalid pending question.');
 const queue = s.queue ?? [];
 if (!Array.isArray(queue) || !queue.every(item => item && typeof item.id === 'string' && !!item.id && typeof item.text === 'string')) throw new EngineError('The engine returned an invalid message queue.');
 return { ...s, questions, entries, queue, tasks: tasks.map(row => ({ ...row, Waits: row.Waits ?? [] })) };
}
const sessionPath = (id: string) => `/sessions/${encodeURIComponent(id)}`;
/**
 * Attaches a saved conversation once however many views ask at the same moment:
 * the open tab, its background observer and React's development remount share
 * the one POST in flight. A new conversation (no sessionFile) is never shared,
 * because each empty POST creates a session of its own.
 * A new place chat supplies its place before the host chooses its working folder;
 * a saved attachment always uses only its own session file.
 */
const attaching = new Map<string, Promise<EngineSnapshot>>();
export function connectEngine(sessionFile?: string, place?: string): Promise<EngineSnapshot> {
 if (!sessionFile) return openSession(place ? { place } : {});
 const pending = attaching.get(sessionFile);
 if (pending) return pending;
 const attach = openSession({ sessionFile }).finally(() => attaching.delete(sessionFile));
 attaching.set(sessionFile, attach);
 return attach;
}
async function openSession(body: { sessionFile?: string; place?: string }): Promise<EngineSnapshot> {
 const response = await fetchEngine('/sessions', { method: 'POST', body: JSON.stringify(body) });
 return snapshotFrom(await response.json());
}
/**
 * Reads a conversation. With `since` (how many entries the window already
 * holds) the engine answers with a tail, which is merged into `held`; a
 * tail that cannot be merged falls back to one whole read, so a window never
 * shows a transcript it could not check.
 */
export async function readEngine(id: string, since?: number, held?: EngineSnapshot): Promise<EngineSnapshot> {
 if (since === undefined || !held) return snapshotFrom(await (await fetchEngine(sessionPath(id))).json());
 const body: unknown = await (await fetchEngine(`${sessionPath(id)}?since=${since}`)).json();
 if (!isTail(body)) return snapshotFrom(body);
 try { return snapshotFrom(mergeTail(held, body)); }
 catch (error) { if (error instanceof EngineError) throw error; return readEngine(id); }
}
async function action(id: string, kind: 'turn' | 'stop' | 'answer' | 'queue-edit' | 'queue-move' | 'queue-remove', body?: unknown): Promise<EngineSnapshot> {
 const response = await fetchEngine(`${sessionPath(id)}/${kind}`, { method: 'POST', body: JSON.stringify(body ?? {}) });
 const result: unknown = await response.json();
 if (!result || typeof result !== 'object' || !('accepted' in result) || result.accepted !== true) throw new EngineError('The engine did not accept this action.');
 return readEngine(id);
}
export function sendEngine(id: string, text: string, mode: 'submit' | 'steer' | 'queue' = 'submit'): Promise<EngineSnapshot> {
 return action(id, 'turn', { text, mode });
}
/** Replaces the words of a queued message. Refused (409) once its turn has started. */
export function editQueued(id: string, queued: string, text: string): Promise<EngineSnapshot> { return action(id, 'queue-edit', { id: queued, text }); }
/** Moves a queued message to index `to` of the queue that remains without it. Refused (409) once its turn has started. */
export function moveQueued(id: string, queued: string, to: number): Promise<EngineSnapshot> { return action(id, 'queue-move', { id: queued, to }); }
/** Takes a queued message back so it never runs. Refused (409) once its turn has started. */
export function removeQueued(id: string, queued: string): Promise<EngineSnapshot> { return action(id, 'queue-remove', { id: queued }); }
export function stopEngine(id: string): Promise<EngineSnapshot> { return action(id, 'stop'); }
/** Identity and explicit canonical option keys cross unchanged; only the engine resolves a question. */
export function answerEngine(id: string, answer: EngineAnswer): Promise<EngineSnapshot> { return action(id, 'answer', answer); }
export async function readTaskPage(id: string, taskId: string): Promise<EngineTaskPage> {
 return (await (await fetchEngine(`${sessionPath(id)}/tasks/${encodeURIComponent(taskId)}`)).json()) as EngineTaskPage;
}

export async function readToolResult(id: string, callId: string): Promise<{output:string;full:boolean}> {
 const result:unknown=await (await fetchEngine(`${sessionPath(id)}/tools/${encodeURIComponent(callId)}`)).json();
 if(!result || typeof result!=='object' || !('output' in result) || typeof result.output!=='string' || !('full' in result) || typeof result.full!=='boolean')throw new EngineError('The engine returned an invalid tool result.');
 return result as {output:string;full:boolean};
}

/**
 * Reads one server-sent-event body: handles chunked CRLF and multiline data,
 * hands each complete record's data text to onRecord, and reports a stream the
 * engine closed. Aborting the signal only detaches this reader.
 */
async function pumpEventStream(response: Response, signal: AbortSignal, onRecord: (text: string) => void): Promise<void> {
 if (!response.body || !response.headers.get('content-type')?.includes('text/event-stream')) throw new EngineError('The engine did not open a conversation stream.');
 const reader = response.body.getReader();
 const decoder = new TextDecoder();
 let buffer = '';
 let data: string[] = [];
 function dispatch() {
  if (signal.aborted || !data.length) { data = []; return; }
  const text = data.join('\n'); data = [];
  onRecord(text);
 }
 function consume(final = false) {
  let index: number;
  while ((index = buffer.indexOf('\n')) >= 0) {
   const line = buffer.slice(0, index).replace(/\r$/, ''); buffer = buffer.slice(index + 1);
   if (!line) dispatch();
   else if (line === 'data') data.push('');
   else if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
  }
  if (final && buffer) { const line = buffer.replace(/\r$/, ''); if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, '')); buffer = ''; }
  if (final) dispatch();
 }
 try {
  while (!signal.aborted) {
   const chunk = await reader.read();
   if (chunk.done) { buffer += decoder.decode(); consume(true); if (!signal.aborted) throw new EngineError('The engine connection closed. Reconnect this saved conversation.'); break; }
   buffer += decoder.decode(chunk.value, { stream: true }); consume();
  }
 } catch (error) { if (!signal.aborted) throw error; }
 finally { await reader.cancel().catch(() => undefined); reader.releaseLock(); }
}

/** Shared streams replay after the supplied sequence without the request clock; only the caller interprets records. */
export async function engineEventStream(path: string, after: number, onRecord: (text: string) => void, signal: AbortSignal): Promise<void> {
 const separator = path.includes('?') ? '&' : '?';
 const response = await fetchEngine(`${path}${separator}after=${after}`, { signal, headers: { Accept: 'text/event-stream' } }, true);
 await pumpEventStream(response, signal, onRecord);
}

// Fetch supports the native Bearer header; EventSource cannot. Aborting only
// detaches this reader. Stop is a separate, explicit POST.
export async function watchEngine(snapshot: EngineSnapshot, onSnapshot: (snapshot: EngineSnapshot) => void, onEvent: (event: EngineEvent) => void, signal: AbortSignal): Promise<void> {
 let after = snapshot.seq;
 const response = await fetchEngine(`${sessionPath(snapshot.id)}/events?after=${after}`, { signal, headers: { Accept: 'text/event-stream' } }, true);
 await pumpEventStream(response, signal, text => {
  let record: unknown;
  try { record = JSON.parse(text); } catch { throw new EngineError('The engine sent an invalid stream record.'); }
  if (!record || typeof record !== 'object') throw new EngineError('The engine sent an invalid stream record.');
  const item = record as { seq: number; type: string; snapshot?: unknown; event?: EngineEvent };
  if (!Number.isSafeInteger(item.seq) || item.seq < 0) throw new EngineError('The engine sent an invalid stream sequence.');
  if (item.seq <= after) return;
  if (item.type === 'snapshot') {
   const next = snapshotFrom(isTail(item.snapshot) ? mergeTail(snapshot, item.snapshot) : item.snapshot);
   if (next.id !== snapshot.id || next.sessionFile !== snapshot.sessionFile) throw new EngineError('The engine stream changed conversations.');
   // Each tail starts where the preceding snapshot ended, including within one replay batch.
   snapshot = next;
   onSnapshot(next);
  } else if (item.type === 'event' && item.event && typeof item.event.kind === 'string') onEvent(item.event);
  else throw new EngineError('The engine sent an unknown stream record.');
  after = item.seq;
 });
}

// ---- Contract v2 endpoints (docs/ELEMENTS.md §9). The bridge lane implements the
// server side; until then these reject with the engine's own 404 text. ----
export type TaskAction = 'note' | 'amend' | 'pause' | 'resume' | 'cancel';
/** E1: talk to or control one task. `text` is required for note/amend. */
export async function taskAction(id: string, taskId: string, action: TaskAction, text?: string): Promise<void> {
  await fetchEngine(`${sessionPath(id)}/tasks/${encodeURIComponent(taskId)}/${action}`, { method: 'POST', body: JSON.stringify(text === undefined ? {} : { text }) });
}
export type EngineFile = { name: string; mime: string; size: number; hash: string; dataBase64: string };
export type EnginePathFact = { path: string; exists: boolean; dir: boolean; size: number; modTime?: string; outside?: boolean };
/** E2: read one workspace file (confined, symlink-safe, 16MB cap). */
export async function readEngineFile(id: string, path: string): Promise<EngineFile> {
  return (await (await fetchEngine(`${sessionPath(id)}/files?path=${encodeURIComponent(path)}`)).json()) as EngineFile;
}
/** E2: stat up to 64 paths at once. */
export async function statEnginePaths(id: string, paths: string[]): Promise<EnginePathFact[]> {
  return (await (await fetchEngine(`${sessionPath(id)}/files/stat`, { method: 'POST', body: JSON.stringify({ paths }) })).json()) as EnginePathFact[];
}
// ---- File and diff tabs (docs/ENGINE.md "File and diff tabs"). Every byte comes from the
// engine, which may be on another machine; paths are workspace-relative, slash separated. ----
/** One workspace file as text. `refusal` set means no text: show `message`, offer the editor. */
export type EngineTextFile = {
 path: string; name: string; dir: string; abs: string; size: number; hash?: string;
 language?: string; lines: number; text: string;
 refusal?: 'binary' | 'too-large'; message?: string;
};
/** One Matching row of the command field. */
export type EngineFoundFile = { path: string; name: string; dir: string };
export type EngineFoundFiles = { files: EngineFoundFile[]; truncated?: boolean };
export type EngineChangeStatus = 'modified' | 'added' | 'deleted' | 'untracked';
/** One row of "what changed": header "lexer.go internal/parse +12 −3" is name, dir, added, deleted. */
export type EngineChangedFile = { path: string; name: string; dir: string; status: EngineChangeStatus; added: number; deleted: number; binary?: boolean };
/** What a diff was measured against: `start` is the commit the conversation began on; `head` is the latest commit (no start recorded, or history was rewritten past it). */
/** `startGone` is set only when a start commit was recorded and history no longer reaches it; a `head` base without it means no start was ever recorded. */
export type EngineDiffBase = { kind: 'start' | 'head'; sha?: string; startGone?: boolean };
export type EngineChangedFiles = { git: boolean; base?: EngineDiffBase; branch?: string; files: EngineChangedFile[]; added: number; deleted: number; truncated?: boolean };
/** Unified-diff row: `old`/`new` are the two line-number columns (absent = blank). */
export type EngineDiffLine = { kind: 'context' | 'add' | 'del'; old?: number; new?: number; text: string };
export type EngineDiffHunk = { header: string; oldStart: number; oldLines: number; newStart: number; newLines: number; section?: string; lines: EngineDiffLine[] };
/** `lines` is the current file's length: the gap before a hunk is `newStart - 1 - previous end`, after the last is `lines - end`. */
export type EngineFileDiff = {
 path: string; name: string; dir: string; abs: string; git: boolean; base?: EngineDiffBase;
 status: 'clean' | EngineChangeStatus; added: number; deleted: number; binary?: boolean;
 lines: number; hunks: EngineDiffHunk[]; truncated?: boolean;
};
/** Where "Open in editor" may go. Open only when `local` is true and `host` equals this machine's name. */
export type EngineEditorTarget = { path: string; abs: string; host: string; local: boolean };
/** Read one workspace file as text (1MB cap; binary and large files come back with `refusal`). */
export async function readEngineText(id: string, path: string): Promise<EngineTextFile> {
 return (await (await fetchEngine(`${sessionPath(id)}/files/text?path=${encodeURIComponent(path)}`)).json()) as EngineTextFile;
}
/** Files matching a query, best first (gitignore-aware); an empty query matches nothing. */
export async function findEngineFiles(id: string, query: string, limit = 20): Promise<EngineFoundFiles> {
 return (await (await fetchEngine(`${sessionPath(id)}/files/find?q=${encodeURIComponent(query)}&limit=${limit}`)).json()) as EngineFoundFiles;
}
/** Files that differ from the base (the conversation's start commit, else git HEAD; see `base.kind`); `paths` narrows to what a conversation touched. */
export async function engineChanges(id: string, paths: string[] = []): Promise<EngineChangedFiles> {
 const query = paths.map(path => `path=${encodeURIComponent(path)}`).join('&');
 return (await (await fetchEngine(`${sessionPath(id)}/changes${query ? `?${query}` : ''}`)).json()) as EngineChangedFiles;
}
/** One file's diff against the base, with hunks and counts. */
export async function engineFileDiff(id: string, path: string): Promise<EngineFileDiff> {
 return (await (await fetchEngine(`${sessionPath(id)}/diff?path=${encodeURIComponent(path)}`)).json()) as EngineFileDiff;
}
/** Absolute path for "Open in editor"; 404 when the file is gone, 403 outside the workspace. */
export async function engineEditorTarget(id: string, path: string): Promise<EngineEditorTarget> {
 return (await (await fetchEngine(`${sessionPath(id)}/files/locate?path=${encodeURIComponent(path)}`)).json()) as EngineEditorTarget;
}
/** One editor registered on the engine machine. `id` is a desktop name or a bundle id, never a command. */
export type EngineEditor = { id: string; name: string; default?: boolean };
/** `open` is false when the engine cannot start a program (another machine, or no display). */
export type EngineEditors = { editors: EngineEditor[]; local: boolean; open: boolean; reason?: string };
function editorListFrom(value: unknown): EngineEditors {
 const body = value as EngineEditors | null;
 if (!body || !Array.isArray(body.editors) || body.editors.some(editor => !editor || typeof editor.id !== 'string' || typeof editor.name !== 'string' || !editor.id || !editor.name)) throw new EngineError('The engine returned an invalid editor list.');
 return { editors: body.editors, local: body.local === true, open: body.open === true, reason: typeof body.reason === 'string' ? body.reason : undefined };
}
/** Editors for a workspace file, default first, at most the engine's own cap. An empty list is a real answer. */
export async function engineEditors(id: string, path: string): Promise<EngineEditors> {
 return editorListFrom(await (await fetchEngine(`${sessionPath(id)}/editors?path=${encodeURIComponent(path)}`)).json());
}
/** Start one editor from that list. The id is checked again on the engine; this sends no command line. */
export async function openEngineEditor(id: string, path: string, editorId: string): Promise<void> {
 await fetchEngine(`${sessionPath(id)}/editors/open`, { method: 'POST', body: JSON.stringify({ path, id: editorId }) });
}
/** E3: send a message with attachments. Files are base64 in the body (≤20MB total). */
export type OutgoingFile = { name: string; mime: string; dataBase64: string };
export function sendEngineWithFiles(id: string, text: string, files: OutgoingFile[]): Promise<EngineSnapshot> {
  return action(id, 'turn', { text, mode: 'submit', files });
}
/** E4: stop a question's clock while the person reads it. */
export async function holdQuestion(id: string, q: { kind: string; id: number; ref?: string }): Promise<void> {
  await fetchEngine(`${sessionPath(id)}/questions/hold`, { method: 'POST', body: JSON.stringify(q) });
}
/** E8: favicon for a domain the engine itself contacted in this conversation; data URL or null. */
export async function engineFavicon(id: string, domain: string): Promise<string | null> {
  try { const r = (await (await fetchEngine(`${sessionPath(id)}/favicon?domain=${encodeURIComponent(domain)}`)).json()) as { dataUrl?: string }; return r.dataUrl ?? null; } catch { return null; }
}

// ---- Terminals and jobs. The shell runs on the engine host, in the session
// workspace, behind the same bearer-authenticated bridge as everything above. ----
/** state: "running"; "exited" (ended by itself, exitCode set); "closed" (the person ended it). */
export type TerminalInfo = {
  id: string; kind: 'terminal' | 'job'; title: string; command?: string; cwd: string; shell: string;
  state: 'running' | 'exited' | 'closed'; exitCode?: number; startedAt: string; endedAt?: string;
  durationMs: number; cols: number; rows: number;
  /** Offset one past the newest output byte; pass a stream's last end offset as `after` to resume. */
  bytes: number;
};
const terminalPath = (id: string, terminal?: string, action?: string) => `${sessionPath(id)}/terminals${terminal ? `/${encodeURIComponent(terminal)}` : ''}${action ? `/${action}` : ''}`;
function terminalInfo(value: unknown): TerminalInfo {
  const t = value as TerminalInfo | null;
  if (!t || typeof t.id !== 'string' || !t.id || (t.kind !== 'terminal' && t.kind !== 'job') || typeof t.title !== 'string' || !['running', 'exited', 'closed'].includes(t.state) || !Number.isFinite(t.durationMs)) throw new EngineError('The engine returned an invalid terminal.');
  return t;
}
/** Starts an interactive shell, or a job when `command` is given. */
export async function startTerminal(id: string, options: { command?: string; title?: string; cols?: number; rows?: number } = {}): Promise<TerminalInfo> {
  return terminalInfo(await (await fetchEngine(terminalPath(id), { method: 'POST', body: JSON.stringify(options) })).json());
}
export async function listTerminals(id: string): Promise<TerminalInfo[]> {
  const list: unknown = await (await fetchEngine(terminalPath(id))).json();
  if (!Array.isArray(list)) throw new EngineError('The engine returned an invalid terminal list.');
  return list.map(terminalInfo);
}
export async function readTerminal(id: string, terminal: string): Promise<TerminalInfo> {
  return terminalInfo(await (await fetchEngine(terminalPath(id, terminal))).json());
}
const toBase64 = (bytes: Uint8Array) => { let text = ''; for (let i = 0; i < bytes.length; i += 0x8000) text += String.fromCharCode(...bytes.subarray(i, i + 0x8000)); return btoa(text); };
const fromBase64 = (text: string) => Uint8Array.from(atob(text), c => c.charCodeAt(0));
/** Types into the terminal; a string is sent as UTF-8. */
export async function writeTerminal(id: string, terminal: string, data: string | Uint8Array): Promise<void> {
  await fetchEngine(terminalPath(id, terminal, 'input'), { method: 'POST', body: JSON.stringify({ dataBase64: toBase64(typeof data === 'string' ? new TextEncoder().encode(data) : data) }) });
}
export async function resizeTerminal(id: string, terminal: string, cols: number, rows: number): Promise<TerminalInfo> {
  return terminalInfo(await (await fetchEngine(terminalPath(id, terminal, 'resize'), { method: 'POST', body: JSON.stringify({ cols, rows }) })).json());
}
/** Ends the process group. A job stays listed with its log; a terminal leaves the list. */
export async function closeTerminal(id: string, terminal: string): Promise<TerminalInfo> {
  return terminalInfo(await (await fetchEngine(terminalPath(id, terminal, 'close'), { method: 'POST', body: '{}' })).json());
}
/** Ends it if running and forgets it and its log. */
export async function removeTerminal(id: string, terminal: string): Promise<void> {
  await fetchEngine(terminalPath(id, terminal, 'remove'), { method: 'POST', body: '{}' });
}
/**
 * Follows one terminal's output from byte offset `after` (0 replays the kept
 * scrollback). Hold at most one of these per open tab: browsers allow six
 * connections per origin. Resolves after the exit record. `cut` says the first
 * bytes were dropped from the bounded scrollback, so the screen should reset.
 */
export async function watchTerminal(id: string, terminal: string, after: number, onOutput: (bytes: Uint8Array, end: number, cut: boolean) => void, onExit: (info: TerminalInfo) => void, signal: AbortSignal): Promise<void> {
  const response = await fetchEngine(`${terminalPath(id, terminal, 'stream')}?after=${after}`, { signal, headers: { Accept: 'text/event-stream' } }, true);
  let finished = false;
  await pumpEventStream(response, signal, text => {
    let record: { seq?: number; type?: string; dataBase64?: string; cut?: boolean; info?: unknown };
    try { record = JSON.parse(text); } catch { throw new EngineError('The engine sent an invalid terminal record.'); }
    if (record.type === 'output' && typeof record.dataBase64 === 'string' && Number.isSafeInteger(record.seq)) onOutput(fromBase64(record.dataBase64), record.seq!, record.cut === true);
    else if (record.type === 'exit') { finished = true; onExit(terminalInfo(record.info)); }
    else throw new EngineError('The engine sent an unknown terminal record.');
  }).catch(error => { if (!finished) throw error; });
}
/** The recent output as plain text (escape codes removed, redraws collapsed). */
export async function readTerminalOutput(id: string, terminal: string, tailBytes?: number): Promise<{ text: string; truncated: boolean; info: TerminalInfo }> {
  const r = await (await fetchEngine(`${terminalPath(id, terminal, 'output')}${tailBytes ? `?tail=${tailBytes}` : ''}`)).json() as { text?: unknown; truncated?: unknown; info?: unknown };
  if (typeof r.text !== 'string') throw new EngineError('The engine returned invalid terminal output.');
  return { text: r.text, truncated: r.truncated === true, info: terminalInfo(r.info) };
}
/**
 * "Ask codeaf about this output": sends the question with the selected text, or
 * else the terminal's recent output, attached as a file through the ordinary
 * attachment path (E3). Continues `conversationId`, or opens a new conversation
 * when none is given. Returns the conversation the question went to.
 */
export async function askAboutTerminalOutput(terminal: { sessionId: string; id: string }, question: string, options: { conversationId?: string; selection?: string; tailBytes?: number } = {}): Promise<EngineSnapshot> {
  const picked = options.selection?.trim();
  const source = picked ? { text: picked, label: 'selection', info: await readTerminal(terminal.sessionId, terminal.id) } : await readTerminalOutput(terminal.sessionId, terminal.id, options.tailBytes ?? 64 << 10).then(r => ({ ...r, label: 'output' }));
  if (!source.text.trim()) throw new EngineError('There is no output to ask about yet.');
  const target = options.conversationId ?? (await connectEngine()).id;
  const name = `${source.info.title.replace(/[^\w.-]+/g, '-')}-${source.label}.txt`;
  return sendEngineWithFiles(target, question.trim() || 'What is going on in this output?', [{ name, mime: 'text/plain', dataBase64: toBase64(new TextEncoder().encode(source.text)) }]);
}
/** "Running · 2m 14s" or "exit 0 · 2m ago": the header's state words. `now` is injectable for tests. */
export function terminalStateWords(info: TerminalInfo, now = Date.now()): string {
  const span = (ms: number) => { const s = Math.max(0, Math.floor(ms / 1000)); return s < 60 ? `${s}s` : s < 3600 ? `${Math.floor(s / 60)}m ${s % 60}s` : `${Math.floor(s / 3600)}h ${Math.floor(s % 3600 / 60)}m`; };
  if (info.state === 'running') return `Running · ${span(now - Date.parse(info.startedAt))}`;
  const ended = Date.parse(info.endedAt ?? '');
  const ago = Number.isFinite(ended) ? ` · ${span(now - ended)} ago` : '';
  return info.state === 'closed' ? `closed${ago}` : `exit ${info.exitCode ?? 0}${ago}`;
}
/** One thing the person picks a model for, as the engine names and describes it. */
/**
 * One model role. `category` is the settings section it is listed under; `live` is false while nothing in the engine
 * makes this role's calls yet; `inherits` names the role it follows until it is chosen itself.
 */
export type ModelRole = { id: string; name: string; controls: string; model: string; default: string; effort?: string; chosen: boolean; category?: string; live?: boolean; inherits?: string };
export type ModelRoleCategory = { id: string; name: string };
export type ModelRoles = { default: string; roles: ModelRole[]; categories?: ModelRoleCategory[] };
/** One model the provider offers; `efforts` lists the effort words it accepts, when it has any. */
export type CatalogModel = { id: string; name: string; contextLength?: number; efforts?: string[] };
/** `fallback` means the provider's list could not be read and only the models in use are offered. */
export type ModelCatalog = { models: CatalogModel[]; fallback?: boolean };
/** The word for the role that answers what the person types; the composer's picker drives it. */
export const CONVERSATION_ROLE = 'conversation';
export async function readModelRoles(): Promise<ModelRoles> {
 const view = await (await fetchEngine('/models/roles')).json() as ModelRoles;
 if (!view || !Array.isArray(view.roles)) throw new EngineError('The engine returned invalid model roles.');
 return view;
}
export async function readModelCatalog(): Promise<ModelCatalog> {
 const view = await (await fetchEngine('/models')).json() as ModelCatalog;
 if (!view || !Array.isArray(view.models)) throw new EngineError('The engine returned an invalid model list.');
 return view;
}
/** Sets a role's model (and effort). An empty model puts the role back on the default. The change applies to the role's next call. */
export async function setModelRole(role: string, choice: { model: string; effort?: string }): Promise<ModelRole> {
 const response = await fetchEngine(`/models/roles/${encodeURIComponent(role)}`, { method: 'PUT', body: JSON.stringify({ model: choice.model, effort: choice.effort ?? '' }) });
 return await response.json() as ModelRole;
}
/**
 * One Places organization setting, as internal/placegraph's policy table describes it. `design` is true when the
 * default is the design's own figure; otherwise it is a provisional engineering choice.
 */
export type PlacesSetting = {
 key: string; group: string; name: string; explain: string; kind: 'switch' | 'number';
 default: boolean | number; value: boolean | number; min?: number; max?: number; unit?: string; design: boolean; chosen: boolean;
};
export async function readPlacesPolicy(): Promise<PlacesSetting[]> {
 const view = await (await fetchEngine('/places/policy')).json() as { settings?: PlacesSetting[] };
 if (!view || !Array.isArray(view.settings)) throw new EngineError('The engine returned invalid Places settings.');
 return view.settings;
}
/** Saves one Places setting; `null` puts it back on its default. */
export async function setPlacesPolicy(key: string, value: boolean | number | null): Promise<PlacesSetting> {
 const response = await fetchEngine(`/places/policy/${encodeURIComponent(key)}`, { method: 'PUT', body: JSON.stringify({ value }) });
 return await response.json() as PlacesSetting;
}
/** One pinned model: its id and the short word the composer's segmented control shows for it. */
export type PinnedModel = { id: string; label: string };
export type PinnedModels = { pinned: PinnedModel[]; chosen: boolean };
/** Fired on the window after a model choice is saved, so the composer's picker reads the lists again. */
export const MODELS_CHANGED = 'codeaf:models-changed';
export async function readPinnedModels(): Promise<PinnedModels> {
 const view = await (await fetchEngine('/models/pinned')).json() as PinnedModels;
 if (!view || !Array.isArray(view.pinned)) throw new EngineError('The engine returned an invalid pinned list.');
 return view;
}
/** Pins exactly three catalog models in segment order; an empty list puts the pins back on the default three. */
export async function setPinnedModels(models: string[]): Promise<PinnedModels> {
 const response = await fetchEngine('/models/pinned', { method: 'PUT', body: JSON.stringify({ models }) });
 return await response.json() as PinnedModels;
}
