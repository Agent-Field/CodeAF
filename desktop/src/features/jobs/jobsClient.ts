import { EngineError, fetchEngine } from '../chat/engine-client.ts';

/**
 * Typed client for the engine's background-job routes
 * (internal/desktopbridge/jobs.go): the list a conversation already holds,
 * a stop that is Cancel("job:N"), and a log tail read from that conversation's
 * own folder.
 *
 * The terminal pane is the reader. This module holds no list and no timer:
 * each call is one request, and a refusal arrives as EngineError carrying the
 * engine's own sentence. Empty facts stay absent. A duration or a tick count
 * the bridge omits at zero is not invented back; an exit code of 0 is kept,
 * because a finished command really did exit 0.
 *
 * The log path is never a query. The route reads the path off the job it
 * listed, and a client that named a file would be asking it to read something
 * the conversation did not spool.
 */

/** What sort of background work a job is, in the engine's own words. */
export type JobKind = 'command' | 'watch' | 'render' | 'task';
/** Where a job is. A stopped job is not a failed one. */
export type JobState = 'running' | 'done' | 'failed' | 'stopped';

/**
 * One engine background job, as GET /sessions/{id}/jobs spells it.
 * The same shape rides a `jobs` world record. Absent fields were omitted
 * by the bridge; they are not filled in here.
 */
export type EngineJob = {
  id: number;
  name?: string;
  command?: string;
  detail?: string;
  kind?: JobKind;
  state?: JobState;
  startedAt?: string;
  /** How long it has run, in milliseconds. Absent at zero: nothing to draw. */
  elapsedMs?: number;
  /** Set only once the command has exited. Zero is a real exit and stays. */
  exitCode?: number;
  /** A watch's completed runs. Absent at zero, and for every other kind. */
  ticks?: number;
  logPath?: string;
};

/** What a successful stop answers. `line` is Cancel's sentence, when it had one. */
export type JobStop = { accepted: true; line?: string };

/** The tail of one job's log, after the bridge has stripped control bytes. */
export type JobLog = { text: string; truncated: boolean };

const kinds = new Set<string>(['command', 'watch', 'render', 'task']);
const states = new Set<string>(['running', 'done', 'failed', 'stopped']);

function invalid(what: string): never {
  throw new EngineError(`The engine returned an invalid ${what}.`);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

/** A JSON number the bridge can have sent: a whole count, not a float and not past what JS can hold. */
function whole(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value);
}

/**
 * An optional string the bridge omits when empty. A non-string is a lie.
 * The text is kept as sent: a command's spaces are part of the command.
 */
function optionalText(value: unknown, what: string): string | undefined {
  if (value === undefined || value === '') return undefined;
  if (typeof value !== 'string') invalid(what);
  return value;
}

function optionalKind(value: unknown): JobKind | undefined {
  if (value === undefined || value === '') return undefined;
  if (typeof value !== 'string' || !kinds.has(value)) invalid('job');
  return value as JobKind;
}

function optionalState(value: unknown): JobState | undefined {
  if (value === undefined || value === '') return undefined;
  if (typeof value !== 'string' || !states.has(value)) invalid('job');
  return value as JobState;
}

/**
 * A count the bridge omits at zero. Zero and absent are the same fact here,
 * so a present zero is dropped rather than drawn as "0".
 */
function optionalCount(value: unknown, what: string): number | undefined {
  if (value === undefined) return undefined;
  if (!whole(value) || value < 0) invalid(what);
  return value === 0 ? undefined : value;
}

/** An exit code, including 0. Only a non-number is refused; absence stays absence. */
function optionalCode(value: unknown, what: string): number | undefined {
  if (value === undefined) return undefined;
  if (!whole(value)) invalid(what);
  return value;
}

/** Copies one wire row. Unknown fields are dropped; a missing id is not guessed. */
function jobFrom(value: unknown): EngineJob {
  if (!isRecord(value) || !whole(value.id) || value.id <= 0) invalid('job');
  const job: EngineJob = { id: value.id };
  const name = optionalText(value.name, 'job');
  const command = optionalText(value.command, 'job');
  const detail = optionalText(value.detail, 'job');
  const startedAt = optionalText(value.startedAt, 'job');
  const logPath = optionalText(value.logPath, 'job');
  const kind = optionalKind(value.kind);
  const state = optionalState(value.state);
  const elapsedMs = optionalCount(value.elapsedMs, 'job');
  const ticks = optionalCount(value.ticks, 'job');
  const exitCode = optionalCode(value.exitCode, 'job');
  if (name !== undefined) job.name = name;
  if (command !== undefined) job.command = command;
  if (detail !== undefined) job.detail = detail;
  if (kind !== undefined) job.kind = kind;
  if (state !== undefined) job.state = state;
  if (startedAt !== undefined) job.startedAt = startedAt;
  if (elapsedMs !== undefined) job.elapsedMs = elapsedMs;
  if (exitCode !== undefined) job.exitCode = exitCode;
  if (ticks !== undefined) job.ticks = ticks;
  if (logPath !== undefined) job.logPath = logPath;
  return job;
}

function listFrom(value: unknown): EngineJob[] {
  if (!Array.isArray(value)) invalid('job list');
  return value.map(jobFrom);
}

function stopFrom(value: unknown): JobStop {
  if (!isRecord(value) || value.accepted !== true) invalid('stop');
  const line = optionalText(value.line, 'stop');
  return line === undefined ? { accepted: true } : { accepted: true, line };
}

/**
 * Refuses a body that is not `{text, truncated}`. A missing `truncated` must
 * not be read as "this is the whole log", and a missing `text` must not become
 * an empty string the pane would draw as silence.
 */
function logFrom(value: unknown): JobLog {
  if (!isRecord(value) || typeof value.text !== 'string' || typeof value.truncated !== 'boolean') invalid('job log');
  return { text: value.text, truncated: value.truncated };
}

async function readBody(response: Response, what: string): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    invalid(what);
  }
}

const jobsPath = (id: string) => `/sessions/${encodeURIComponent(id)}/jobs`;

/**
 * One job's action. The id is one path segment: encoding it keeps a slash in
 * an id from becoming a second segment the route would read as a different action.
 */
function jobAction(id: string, jobId: string | number, action: 'stop' | 'log'): string {
  return `${jobsPath(id)}/${encodeURIComponent(String(jobId))}/${action}`;
}

/**
 * Lists the conversation's background jobs in the engine's order. An empty
 * shelf is an empty list. Nothing here re-sorts.
 */
export async function listJobs(id: string): Promise<EngineJob[]> {
  return listFrom(await readBody(await fetchEngine(jobsPath(id)), 'job list'));
}

/**
 * Stops one job. The route addresses it as Cancel("job:" + id); this client
 * sends the bare number and lets the bridge add that prefix, so a task id
 * cannot be stopped by accident.
 */
export async function stopJob(id: string, jobId: string | number): Promise<JobStop> {
  const response = await fetchEngine(jobAction(id, jobId, 'stop'), { method: 'POST', body: '{}' });
  return stopFrom(await readBody(response, 'stop'));
}

/**
 * Reads the tail of one job's log. `tail` is a number of bytes. Omitted, the
 * bridge applies its own cap (the last 1 MiB) and never more than that; this
 * client does not pick a size of its own. A `tail` the bridge refuses comes
 * back as that refusal's sentence.
 */
export async function jobLog(id: string, jobId: string | number, tail?: number): Promise<JobLog> {
  const query = tail === undefined ? '' : `?tail=${encodeURIComponent(String(tail))}`;
  return logFrom(await readBody(await fetchEngine(`${jobAction(id, jobId, 'log')}${query}`), 'job log'));
}
