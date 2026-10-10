import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useWorld } from '../world/useWorld.ts';
import { sessionFor } from '../terminal/open.ts';
import { listJobs, jobLog } from './client.ts';
import type { EngineJob } from './types.ts';

/** How often a running job's record and log tail are read again. A finished job is read once. */
export const jobPollMs = 1500;
/** The tail asked for, so the pane never depends on the bridge's own cap. */
export const jobTailBytes = 256 << 10;

export type JobLogState = {
  /** The engine session the job belongs to; stop and ask address it. Absent until attached. */
  sessionId?: string;
  /** The id the world feed files this conversation's jobs under (its folder name, else the session id). */
  chatId?: string;
  job?: EngineJob;
  text: string;
  /** True when the engine cut the front of the log; the pane says so in one muted line. */
  truncated: boolean;
  /** When the record was read, so a running job's clock can be advanced between reads. */
  readAt: number;
  /** The engine no longer lists this job (it was removed, or the session started afresh). */
  gone: boolean;
  /** The sentence for a read that failed. Cleared by the next good read. */
  error?: string;
  refresh: () => void;
};

/** The bridge's own rule (jobsChatID): the folder the journal lives in, or the handle when it has no folder yet. */
export function chatIdOf(sessionFile: string | undefined, sessionId: string): string {
  const folder = sessionFile?.split(/[\\/]/).slice(0, -1).pop();
  return folder && folder !== '.' ? folder : sessionId;
}

const noRows: readonly EngineJob[] = [];
const empty = { text: '', truncated: false, readAt: 0, gone: false } as const;

/**
 * Follows one engine job by id: attaches the conversation's session (the same call a reload makes, so the tab
 * reattaches by `jobId` alone), reads the job's record, then its log tail, and keeps both fresh while it runs.
 * No stream is held; a poll is one request and ends when the job does.
 */
export function useJobLog(sessionFile: string | undefined, jobId: string): JobLogState {
  const [state, setState] = useState<Omit<JobLogState, 'refresh'>>(empty);
  const [tick, setTick] = useState(0);
  const sessionId = useRef<string>(undefined);
  const chatId = useRef<string>(undefined);
  const refresh = useCallback(() => setTick(n => n + 1), []);

  useEffect(() => {
    const abort = new AbortController();
    let timer = 0;
    async function read() {
      try {
        if (!sessionId.current) {
          const session = await sessionFor(sessionFile);
          sessionId.current = session.id;
          chatId.current = chatIdOf(session.sessionFile, session.id);
        }
        const id = sessionId.current;
        const job = (await listJobs(id)).find(row => String(row.id) === jobId);
        if (abort.signal.aborted) return;
        if (!job) { setState({ ...empty, sessionId: id, readAt: Date.now(), gone: true }); return; }
        const log = await jobLog(id, jobId, jobTailBytes);
        if (abort.signal.aborted) return;
        setState({ sessionId: id, chatId: chatId.current, job, text: log.text, truncated: log.truncated, readAt: Date.now(), gone: false });
        if (job.state === 'running') timer = window.setTimeout(() => void read(), jobPollMs);
      } catch (failure) {
        if (abort.signal.aborted) return;
        setState(prev => ({ ...prev, error: failure instanceof Error ? failure.message : 'The job could not be read.' }));
      }
    }
    void read();
    return () => { abort.abort(); window.clearTimeout(timer); };
  }, [sessionFile, jobId, tick]);


  // The world feed already says when a job ends, for every conversation, with no session stream held. A finished
  // record wins over a poll that still reads "running", because a job never starts again; the log is re-read once.
  const pushed = useWorld(useCallback(world => (state.chatId ? world.jobs(state.chatId) : noRows), [state.chatId])).find(row => String(row.id) === jobId);
  const ended = !!pushed?.state && pushed.state !== 'running' && state.job?.state === 'running';
  useEffect(() => { if (ended) refresh(); }, [ended, refresh]);
  // Memoized: the pane's summary effect depends on the job object, so a fresh merge each render would loop.
  const job = useMemo(() => (ended ? { ...state.job, ...pushed } as EngineJob : state.job), [ended, state.job, pushed]);
  return { ...state, job, refresh };
}
