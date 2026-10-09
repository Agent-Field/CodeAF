// Pure mapping from a plan row to the person-facing state word and its still
// mark. It follows the engine's StateWord table so the tree, the task view and
// the notices all say the same thing.

import type { IconName } from '../../components/ui/Icon';
import type { Status } from '../../components/ui/StatusMark';

export type TaskKind = 'queued' | 'running' | 'done' | 'incomplete' | 'stopped' | 'interrupted' | 'yourcall' | 'paused';

export type TaskFlags = {
  stopped?: boolean;
  interrupted?: boolean;
  /** The engine's hold reason; a held ready/running task is still queued. */
  hold?: string;
  /** The person paused it (PlanPause). */
  paused?: boolean;
};

export type TaskMark = {
  kind: TaskKind;
  icon: IconName;
  label: string;
  tone: 'muted' | 'normal' | 'warning' | 'attention';
};

const marks: Record<TaskKind, TaskMark> = {
  done: { kind: 'done', icon: 'check', label: 'Done', tone: 'muted' },
  // Still on purpose: a calm dot reads as "in progress" without pulling the eye.
  running: { kind: 'running', icon: 'running', label: 'Running', tone: 'normal' },
  queued: { kind: 'queued', icon: 'queued', label: 'Queued', tone: 'muted' },
  incomplete: { kind: 'incomplete', icon: 'failed', label: 'Incomplete', tone: 'warning' },
  stopped: { kind: 'stopped', icon: 'cancelled', label: 'Stopped', tone: 'muted' },
  interrupted: { kind: 'interrupted', icon: 'cancelled', label: 'Interrupted', tone: 'muted' },
  yourcall: { kind: 'yourcall', icon: 'alert', label: 'Your call', tone: 'attention' },
  paused: { kind: 'paused', icon: 'pause', label: 'Paused', tone: 'muted' },
};

const kindByStatus: Record<string, TaskKind> = {
  pending: 'queued',
  ready: 'running',
  claimed: 'running',
  running: 'running',
  done: 'done',
  failed: 'incomplete',
  cancelled: 'incomplete',
  incomplete: 'incomplete',
  stopped: 'stopped',
  paused: 'yourcall',
};

const ENDED: ReadonlySet<TaskKind> = new Set(['done', 'incomplete', 'stopped', 'interrupted']);

function flagsOf(flags: boolean | TaskFlags): TaskFlags {
  return typeof flags === 'boolean' ? { stopped: flags } : flags;
}

/** An unknown status reads as queued: quiet, and never claims progress. */
export function taskKind(status: string, input: boolean | TaskFlags = {}): TaskKind {
  const flags = flagsOf(input);
  const base = kindByStatus[status.trim().toLowerCase()] ?? 'queued';
  if (flags.stopped) return 'stopped';
  if (flags.interrupted) return 'interrupted';
  if (flags.paused) return 'paused';
  return flags.hold && base === 'running' ? 'queued' : base;
}

export function taskMark(status: string, flags: boolean | TaskFlags = {}): TaskMark {
  return marks[taskKind(status, flags)];
}

/** The shared StatusMark each task state draws with (foundations: the eight status marks). */
const statuses: Record<TaskKind, Status> = {
  running: 'running',
  yourcall: 'waiting',
  queued: 'queued',
  done: 'done',
  incomplete: 'incomplete',
  stopped: 'stopped',
  interrupted: 'stopped',
  paused: 'paused',
};

export const statusOf = (kind: TaskKind): Status => statuses[kind];

/** The row-shaped fields the mapping reads. */
export type StateSource = { Status: string; Stopped?: boolean; Interrupted?: boolean; Hold?: string; Paused?: boolean };

export function rowFlags(row: StateSource): TaskFlags {
  return { stopped: row.Stopped, interrupted: row.Interrupted, hold: row.Hold, paused: row.Paused };
}

export const rowKind = (row: StateSource): TaskKind => taskKind(row.Status, rowFlags(row));

export const isEnded = (kind: TaskKind): boolean => ENDED.has(kind);

export type TaskControls = { pause: boolean; resume: boolean; stop: boolean };

/** What can be done to a task now. The run root cannot be paused: the engine refuses it. */
export function taskControls(kind: TaskKind, isSubtask: boolean): TaskControls {
  const live = kind === 'running' || kind === 'queued';
  return {
    pause: isSubtask && live,
    resume: isSubtask && (kind === 'paused' || kind === 'yourcall'),
    stop: !isEnded(kind),
  };
}
