import type { TaskProgress as Progress } from '../taskTree';
import './task-progress.css';

const SEGMENTS = ['done', 'running', 'queued', 'failed'] as const;

/** One cell per task, grouped done, running, queued, failed: failures sit last. */
export function TaskProgressStrip({ progress }: { progress: Progress }) {
  if (progress.total === 0) return null;
  const summary = SEGMENTS.filter((name) => progress[name] > 0)
    .map((name) => `${progress[name]} ${name}`)
    .join(', ');
  return (
    <div className="task-progress" role="img" aria-label={summary}>
      {SEGMENTS.flatMap((name) =>
        Array.from({ length: progress[name] }, (_, index) => <span key={`${name}-${index}`} className="task-progress-cell" data-segment={name} />),
      )}
    </div>
  );
}
