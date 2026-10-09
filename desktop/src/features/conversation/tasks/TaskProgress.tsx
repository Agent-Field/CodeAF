import type { TaskProgress as Progress } from '../taskTree';
import './task-progress.css';

/** Left to right: done, running, waiting on you, failed, queued. */
const SEGMENTS = ['done', 'running', 'waiting', 'failed', 'queued'] as const;

/** One cell per task, so a segment is as wide as its share; the cells of a segment join into one rounded bar. */
export function TaskProgressStrip({ progress }: { progress: Progress }) {
  if (progress.total === 0) return null;
  const summary = SEGMENTS.filter((name) => progress[name] > 0)
    .map((name) => `${progress[name]} ${name}`)
    .join(', ');
  return (
    <div className="task-progress" role="img" aria-label={summary}>
      {SEGMENTS.flatMap((name) =>
        Array.from({ length: progress[name] }, (_, index) => (
          <span
            key={`${name}-${index}`}
            className="task-progress-cell"
            data-segment={name}
            data-start={index === 0 || undefined}
            data-end={index === progress[name] - 1 || undefined}
          />
        )),
      )}
    </div>
  );
}
