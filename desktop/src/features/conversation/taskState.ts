// Pure mapping from a canonical plan status to its still mark. Shared by the
// task notice and the task panel so both read the same words and tones.

import type { IconName } from '../../components/ui/Icon';

export type TaskMark = {
  icon: IconName | 'indicator';
  label: string;
  tone: 'muted' | 'normal' | 'warning';
};

const done: TaskMark = { icon: 'check', label: 'Done', tone: 'muted' };
const running: TaskMark = { icon: 'indicator', label: 'Running', tone: 'normal' };
const queued: TaskMark = { icon: 'queued', label: 'Queued', tone: 'muted' };
const failed: TaskMark = { icon: 'failed', label: 'Failed', tone: 'warning' };
const cancelled: TaskMark = { icon: 'cancelled', label: 'Cancelled', tone: 'muted' };
const stopped: TaskMark = { icon: 'cancelled', label: 'Stopped', tone: 'muted' };

const byStatus: Record<string, TaskMark> = {
  done,
  running,
  claimed: running,
  pending: queued,
  ready: queued,
  paused: queued,
  failed,
  incomplete: failed,
  cancelled,
  stopped,
};

/** An unknown status reads as queued: quiet, and never claims progress. */
export function taskMark(status: string, wasStopped = false): TaskMark {
  if (wasStopped) {
    return stopped;
  }
  return byStatus[status.trim().toLowerCase()] ?? queued;
}
