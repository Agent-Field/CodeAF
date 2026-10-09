import { StatusMark } from '../../../components/ui';
import { statusOf, taskMark, type TaskFlags } from '../taskState';

type TaskMarkProps = TaskFlags & { status: string };

/** One still mark per task, drawn by the shared StatusMark; the label is spoken, shape and colour only reinforce it. */
export function TaskMark({ status, ...flags }: TaskMarkProps) {
  const mark = taskMark(status, flags);
  return <StatusMark status={statusOf(mark.kind)} label={mark.label} />;
}
