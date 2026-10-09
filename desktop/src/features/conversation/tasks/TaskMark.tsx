import { StatusMark } from '../../../components/ui';
import { statusOf, taskMark, type TaskFlags } from '../taskState';

/** `dense` is for rows of work: the mark takes a dot's width of the line. */
type TaskMarkProps = TaskFlags & { status: string; dense?: boolean };

/** One still mark per task, drawn by the shared StatusMark; the label is spoken, shape and colour only reinforce it. */
export function TaskMark({ status, dense, ...flags }: TaskMarkProps) {
  const mark = taskMark(status, flags);
  return <StatusMark status={statusOf(mark.kind)} label={mark.label} dense={dense} />;
}
