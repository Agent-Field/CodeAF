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

