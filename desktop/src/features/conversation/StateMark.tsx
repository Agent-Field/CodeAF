import { Icon } from '../../components/ui';
import { taskMark, type TaskFlags } from './taskState';

type StateMarkProps = TaskFlags & { status: string };

/** A still state mark; the label is spoken, colour only reinforces it. */
export function StateMark({ status, ...flags }: StateMarkProps) {
  const mark = taskMark(status, flags);
  return (
    <span className="state-mark" data-tone={mark.tone} role="img" aria-label={mark.label}>
      <Icon name={mark.icon} size="xs" />
    </span>
  );
}
