import { Button, Icon, PageHeading, Text } from '../../../components/ui';
import type { EngineTaskRow } from '../../chat/engine-client';
import { rowFlags, rowKind, taskMark } from '../taskState';
import { QuestionCardV2, type QuestionCardProps } from '../tray/QuestionCardV2';
import { factsOf, progressText } from './taskDetail';
import { TaskMark } from '../tasks/TaskMark';
import './task-detail.css';

/** The pending question and the handlers its card needs; the tray owns the card itself. */
export type DetailQuestion = Omit<QuestionCardProps, 'now'>;

export type TaskDetailPaneProps = {
  row: EngineTaskRow;
  /** The parent task's title, when it has a parent. */
  parentTitle?: string;
  /** The brief the task was given (the task page's Description); absent until loaded. */
  instructions?: string;
  question?: DetailQuestion;
  now: number;
  onOpenTask: (taskId: string, background: boolean) => void;
};

/** "● Your call  step 12 · 2m": the state word, then only the progress the engine knows. */
function StateLine({ row, now }: Pick<TaskDetailPaneProps, 'row' | 'now'>) {
  const kind = rowKind(row);
  const progress = progressText(row, kind, now);
  return (
    <span className="task-detail-state">
      <TaskMark status={row.Status} {...rowFlags(row)} />
      <span className="task-detail-state-word">{taskMark(row.Status, rowFlags(row)).label}</span>
      {progress && <span>{progress}</span>}
    </span>
  );
}

function Facts({ row }: { row: EngineTaskRow }) {
  const facts = factsOf(row);
  if (facts.length === 0) return null;
  return (
    <dl className="task-detail-facts">
      {facts.map(({ label, value }) => (
        <div key={label} className="task-detail-fact">
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** The right half of the expanded tasks view: one task, its question, its facts, its brief. */
export function TaskDetailPane({ row, parentTitle, instructions, question, now, onOpenTask }: TaskDetailPaneProps) {
  return (
    <aside className="task-detail" aria-label={`Task: ${row.Title}`}>
      <header className="task-detail-head">
        {parentTitle && <Text className="task-detail-crumb">{parentTitle}</Text>}
        <PageHeading className="task-detail-title">{row.Title}</PageHeading>
        <StateLine row={row} now={now} />
      </header>
      {question && (
        <div className="task-detail-question">
          <QuestionCardV2 {...question} now={now} />
        </div>
      )}
      <Facts row={row} />
      {instructions && (
        <section className="task-detail-brief" aria-label="Instructions">
          <h3 className="task-detail-label">Instructions</h3>
          <Text className="task-detail-text">{instructions}</Text>
        </section>
      )}
      <Button className="task-detail-open" onClick={(event) => onOpenTask(row.ID, event.metaKey || event.ctrlKey)}>
        Open task
        <Icon name="arrow" size="xs" />
      </Button>
    </aside>
  );
}
