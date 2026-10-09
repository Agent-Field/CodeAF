import { Button } from '../../../components/ui';
import { TaskMark } from './TaskMark';
import { rowFlags } from '../taskState';
import type { TaskLink } from '../taskPageTurn';
import './task-links.css';

type Props = {
  label: string;
  links: TaskLink[];
  onOpenTask: (taskId: string, background: boolean) => void;
};

/** Sub-tasks, or the tasks this one waits on: quiet rows that open the task. */
export function TaskLinks({ label, links, onOpenTask }: Props) {
  if (links.length === 0) return null;
  return (
    <section className="task-links" aria-label={label}>
      <h3 className="task-links-label">{label}</h3>
      <ul className="task-links-list">
        {links.map(({ row, depth }) => (
          <li key={row.ID} className="task-links-item" data-depth={Math.min(depth, 3)}>
            <Button className="task-links-row" onClick={(event) => onOpenTask(row.ID, event.metaKey || event.ctrlKey)}>
              <TaskMark status={row.Status} {...rowFlags(row)} />
              <span className="task-links-title">{row.Title}</span>
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
