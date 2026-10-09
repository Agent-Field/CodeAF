import { Icon, type IconName } from '../../../components/ui';
import { taskMark, type TaskFlags, type TaskKind } from '../taskState';
import './task-mark.css';

type Glyph = 'dot' | 'ring' | IconName;

/** The tree's marks: colour sits on the 6px dot, everything else is a still, quiet glyph. */
const glyphs: Record<TaskKind, Glyph> = {
  running: 'dot',
  yourcall: 'dot',
  incomplete: 'dot',
  queued: 'ring',
  done: 'check',
  paused: 'pause',
  stopped: 'cancelled',
  interrupted: 'cancelled',
};

type TaskMarkProps = TaskFlags & { status: string };

/** One still mark per task; the label is spoken, the shape and colour only reinforce it. */
export function TaskMark({ status, ...flags }: TaskMarkProps) {
  const mark = taskMark(status, flags);
  const glyph = glyphs[mark.kind];
  const shape = glyph === 'dot' || glyph === 'ring' ? glyph : 'icon';
  return (
    <span className="task-mark" data-kind={mark.kind} data-shape={shape} role="img" aria-label={mark.label}>
      {shape === 'icon' && <Icon name={glyph as IconName} size="xs" />}
    </span>
  );
}
