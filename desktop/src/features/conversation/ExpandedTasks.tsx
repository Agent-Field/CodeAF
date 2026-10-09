import { useEffect, useState, type KeyboardEvent } from 'react';
import { readTaskPage, type EngineAnswer, type EngineQuestion, type EngineTaskRow } from '../chat/engine-client';
import { TaskDetailPane } from './detail/TaskDetailPane';
import { questionKey } from './model/entry';
import { TasksTable } from './tasks/TasksTable';
import { useNow } from './tasks/useNow';
import { taskProgress } from './taskTree';
import type { OpenTask } from './TaskNotice';

export type ExpandedTasksProps = {
  sessionId?: string;
  tasks: EngineTaskRow[];
  questions: EngineQuestion[];
  /** The row the reader last chose; the first waiting row, else the first row, when absent or gone. */
  selectedId?: string;
  onSelect: (taskId: string) => void;
  onClose: () => void;
  onOpenTask: OpenTask;
  busyKey: string | null;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
  onHold: (question: EngineQuestion) => void;
};

/** The first question each task is blocked on, by task id: the engine's own words, never composed. */
export function questionsByTask(questions: readonly EngineQuestion[]): Map<string, EngineQuestion> {
  const byTask = new Map<string, EngineQuestion>();
  for (const question of questions) {
    if (question.withdrawn) continue;
    for (const id of question.blocking?.tasks ?? []) if (!byTask.has(id)) byTask.set(id, question);
  }
  return byTask;
}

function pickRow(tasks: EngineTaskRow[], waiting: Map<string, EngineQuestion>, selectedId?: string): EngineTaskRow | undefined {
  return tasks.find((row) => row.ID === selectedId) ?? tasks.find((row) => waiting.has(row.ID)) ?? tasks[0];
}

/** The selected task's brief, read from its page; absent until the read lands or when it fails. */
function useInstructions(sessionId: string | undefined, taskId: string | undefined): string | undefined {
  const [brief, setBrief] = useState<{ id: string; text: string }>();
  useEffect(() => {
    if (!sessionId || !taskId) return;
    let live = true;
    readTaskPage(sessionId, taskId)
      .then((page) => live && setBrief({ id: taskId, text: (page as { Description?: string }).Description ?? '' }))
      .catch(() => undefined);
    return () => { live = false; };
  }, [sessionId, taskId]);
  return brief && brief.id === taskId ? brief.text || undefined : undefined;
}

/** The task panel expanded to the whole pane (design 1d): the table, and the chosen task beside it. */
export function ExpandedTasks(props: ExpandedTasksProps) {
  const { tasks, questions, onClose } = props;
  const waiting = questionsByTask(questions);
  const row = pickRow(tasks, waiting, props.selectedId);
  const now = useNow(taskProgress(tasks).running > 0);
  const instructions = useInstructions(props.sessionId, row?.ID);
  const reasons = Object.fromEntries([...waiting].map(([id, question]) => [id, question.head]));
  const question = row ? waiting.get(row.ID) : undefined;
  const parent = row?.Parent ? tasks.find((candidate) => candidate.ID === row.Parent) : undefined;

  // Escape leaves the view, except where a field or menu inside it already used the key.
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    if ((event.target as HTMLElement).closest('input, textarea, [role="menu"]')) return;
    event.preventDefault();
    onClose();
  }

  const detail = row && (
    <TaskDetailPane
      key={row.ID}
      row={row}
      parentTitle={parent?.Title}
      instructions={instructions}
      question={question && {
        question,
        busy: props.busyKey === questionKey(question),
        held: false,
        onAnswer: props.onAnswer,
        onHold: () => props.onHold(question),
      }}
      now={now}
      onOpenTask={props.onOpenTask}
    />
  );
  return (
    <div className="expanded-tasks" onKeyDown={onKeyDown}>
      <TasksTable tasks={tasks} selectedId={row?.ID} onSelect={props.onSelect} onClose={onClose} reasons={reasons} detail={detail} now={now} />
    </div>
  );
}
