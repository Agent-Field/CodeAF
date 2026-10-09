// The task page as the engine sends it, plus the optional fields the session
// lane adds (Ended, Changed, LastWords, per-step parts). Every addition is
// optional: a page without them draws exactly what it knows.

import type { EngineTaskPage, EngineTaskRow, PlanStep } from '../../chat/engine-client';

/** session.PlanStep as recorded. The engine does not send step timings or exit
 * codes yet; the log reads them when a later engine does, and draws nothing otherwise. */
export type PageStep = PlanStep & {
  /** Nanoseconds the command ran, when the engine recorded it. */
  took?: number;
  exit?: number;
};

export type PageNote = NonNullable<EngineTaskPage['Notes']>[number];

export type TaskPage = Omit<EngineTaskPage, 'Steps' | 'Row'> & {
  Row: EngineTaskRow;
  Steps?: readonly PageStep[];
  /** Why and when the task ended, from the trajectory's last line. */
  Ended?: { Reason?: string; Result?: string; At?: string };
  /** Repo-relative paths the task changed. */
  Changed?: readonly string[] | null;
  /** The worker's last words, when it ended without a result. */
  LastWords?: string;
};

export type LogState = 'done' | 'failed' | 'refused' | 'running';

/** One line of the terminal log. */
export type LogLine = {
  id: string;
  command: string;
  state: LogState;
  observation: string;
  fullOutput?: string;
  /** Finished commands: how long it ran, when known. */
  took: string;
  /** The live line: when it started, so the view can run its clock. */
  since?: string;
};

export type NoteLine = {
  id: string;
  person: boolean;
  /** "Conversation", or the engine's own word for who wrote it. */
  author: string;
  body: string;
  /** Person notes only, and only while the task runs. */
  receipt: string;
};
