// What a job header says, as pure functions of the engine's job record (Shell 3c).
import type { StateTone } from '../terminal/TerminalHeader.tsx';
import type { TabMetaSource } from '../terminal/tabMeta.ts';
import type { EngineJob } from './types.ts';

const span = (ms: number) => {
  const s = Math.max(0, Math.floor(ms / 1000));
  return s < 60 ? `${s}s` : s < 3600 ? `${Math.floor(s / 60)}m ${s % 60}s` : `${Math.floor(s / 3600)}h ${Math.floor(s % 3600 / 60)}m`;
};

/** The glyph colour: accent while it runs, quiet when it ended well, red when it failed. A stopped job is not a failed one. */
export function jobTone(job: EngineJob): StateTone | undefined {
  return job.state;
}

/**
 * "Running · 2m 14s", "exit 0 · 2m ago", "stopped · 2m ago". The clock needs a start time or an elapsed
 * figure; when the engine gave neither, the words are the state alone and no duration is invented.
 */
export function jobWords(job: EngineJob, now: number, readAt = now): string {
  const started = Date.parse(job.startedAt ?? '');
  if (job.state === 'running') {
    if (Number.isFinite(started)) return `Running · ${span(now - started)}`;
    return job.elapsedMs ? `Running · ${span(job.elapsedMs + (now - readAt))}` : 'Running';
  }
  if (!job.state) return '';
  const ended = Number.isFinite(started) && job.elapsedMs ? started + job.elapsedMs : NaN;
  const ago = Number.isFinite(ended) ? ` · ${span(now - ended)} ago` : '';
  if (job.state === 'stopped') return `stopped${ago}`;
  return job.exitCode !== undefined ? `exit ${job.exitCode}${ago}` : `${job.state === 'failed' ? 'failed' : 'done'}${ago}`;
}

/** The place and the kind of thing: the engine does not name a folder for a job, so only "job" is said. */
export const jobMeta = () => 'job';

/** What the tab strip reads for the exit words: only a job that ended itself says `exit N`. */
export function jobTabMeta(job: EngineJob | undefined): TabMetaSource | undefined {
  if (!job?.state) return undefined;
  if (job.state === 'running') return { kind: 'job', state: 'running' };
  if (job.state === 'stopped') return { kind: 'job', state: 'closed' };
  return { kind: 'job', state: 'exited', exitCode: job.exitCode };
}

/** The log as xterm wants it: the bridge sends bare newlines, the screen does not convert them. */
export const toScreen = (text: string) => text.replace(/\r?\n/g, '\r\n');
