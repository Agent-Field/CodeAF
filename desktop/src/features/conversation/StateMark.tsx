import { Icon } from '../../components/ui';
import { taskMark } from './taskState';

type StateMarkProps = { status: string; stopped?: boolean };

/** A still state mark; the label is spoken, colour only reinforces it. */
export function StateMark({ status, stopped }: StateMarkProps) {
  const mark = taskMark(status, stopped);
  return (
    <span className="state-mark" data-tone={mark.tone} role="img" aria-label={mark.label}>
      <Icon name={mark.icon} size="xs" />
    </span>
  );
}
