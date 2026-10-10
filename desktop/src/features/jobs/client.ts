import { useCallback } from 'react';
import { chatIdFromSessionFile } from '../places/client.ts';
import { useWorld } from '../world/useWorld.ts';
import type { WorldClient } from '../world/worldClient.ts';
import { parseJobs } from './jobsClient.ts';
import type { EngineJob } from './types.ts';

export { listJobs, stopJob, jobLog, parseJobs } from './jobsClient.ts';
export type { EngineJob, JobKind, JobState, JobStop, JobLog } from './types.ts';

const EMPTY: readonly EngineJob[] = Object.freeze([]);
const parsed = new WeakMap<object, readonly EngineJob[]>();

/** The durable conversation folder identifies world records, not the ephemeral attachment id. */
export function jobsForSession(world: Pick<WorldClient, 'jobs'>, sessionFile?: string): readonly EngineJob[] {
  const chatId = sessionFile ? chatIdFromSessionFile(sessionFile) : '';
  if (!chatId) return EMPTY;
  const rows = world.jobs(chatId);
  if (!rows.length) return EMPTY;
  const previous = parsed.get(rows);
  if (previous) return previous;
  // Stable snapshots keep unrelated world updates from repeatedly rendering this pane.
  let jobs: readonly EngineJob[];
  try { jobs = parseJobs(rows); } catch { jobs = EMPTY; }
  parsed.set(rows, jobs);
  return jobs;
}

/** Every job pane shares the window's world connection; it never attaches a session stream. */
export function useJobs(sessionFile?: string): readonly EngineJob[] {
  return useWorld(useCallback((world: WorldClient) => jobsForSession(world, sessionFile), [sessionFile]));
}
