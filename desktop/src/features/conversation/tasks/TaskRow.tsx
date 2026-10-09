import { Button, ContextMenu, DropdownMenu, IconButton, RowActions } from '../../../components/ui';
import type { EngineTaskRow } from '../../chat/engine-client';
import { rowFlags, rowKind } from '../taskState';
import { rowAge, waitPrefix } from './rowText';
import { splitActions, taskActions, type TaskCommands } from './taskMenu';
import { TaskMark } from './TaskMark';
import { waitTitles } from '../taskTree';
import './task-row.css';

export type TaskRowProps = {
  row: EngineTaskRow;
  /** Every row of the conversation, to name what a queued row waits on. */
  rows: EngineTaskRow[];
  now: number;
  current: boolean;
  /** Levels below the panel's top; the top level reads as a heading, deeper ones as work. */
  depth: number;
  /** Set on parents only: whether their children are showing. */
  expanded?: boolean;
  /** Parents only: how many tasks below are done, and how many there are ("2/7"). */
  below?: { done: number; total: number };
  onToggle: (taskId: string) => void;
  commands: TaskCommands;
};

function Disclosure({ row, expanded, onToggle }: Pick<TaskRowProps, 'row' | 'expanded' | 'onToggle'>) {
  if (expanded === undefined) return null;
  return (
    <IconButton
      className="task-row-disclosure"
      label={`${expanded ? 'Collapse' : 'Expand'} ${row.Title}`}
      icon={expanded ? 'chevron' : 'chevronRight'}
      iconSize="xs"
      aria-expanded={expanded}
      tabIndex={-1}
      onClick={() => onToggle(row.ID)}
    />
  );
}

/** The title, led by what a queued task waits on ("Split by loader / Convert env"). */
function Title({ row, rows }: Pick<TaskRowProps, 'row' | 'rows'>) {
  const prefix = waitPrefix(rowKind(row), waitTitles(rows, row));
  return (
    <span className="task-row-title" title={row.Title}>
      {prefix && <span className="task-row-wait">{prefix} </span>}
      {row.Title}
    </span>
  );
}

/** A parent counts what is done below it; any other task says how long it has run or took. */
function Meta({ row, kind, now, below }: Pick<TaskRowProps, 'row' | 'now' | 'below'> & { kind: ReturnType<typeof rowKind> }) {
  const text = below ? `${below.done}/${below.total}` : rowAge(row, kind, now);
  return text ? <span className="task-row-meta">{text}</span> : null;
}

export function TaskRow(props: TaskRowProps) {
  const { row, now, current, commands, below } = props;
  const kind = rowKind(row);
  const actions = taskActions(row, commands);
  const { inline, more } = splitActions(actions);
  return (
    <ContextMenu label={`${row.Title} actions`} items={actions}>
      <div className="task-row" data-actions-host="" data-kind={kind} data-depth={props.depth === 0 ? 0 : 1} data-parent={props.expanded !== undefined || undefined} data-current={current || undefined}>
        <Disclosure row={row} expanded={props.expanded} onToggle={props.onToggle} />
        <Button
          className="task-panel-main"
          data-task-id={row.ID}
          data-parent={props.expanded !== undefined || undefined}
          data-expanded={props.expanded}
          aria-current={current ? 'true' : undefined}
          onClick={(event) => commands.onOpenTask(row.ID, event.metaKey || event.ctrlKey)}
        >
          <TaskMark dense status={row.Status} {...rowFlags(row)} />
          <Title row={row} rows={props.rows} />
        </Button>
        <Meta row={row} kind={kind} now={now} below={below} />
        <RowActions className="task-row-actions">
          {inline.map((action) => (
            <IconButton key={action.id} label={action.label} icon={action.icon ?? 'more'} size="row" iconSize="xs" onClick={action.onSelect} />
          ))}
          {more.length > 0 && (
            <DropdownMenu label={`More actions for ${row.Title}`} items={more}>
              <IconButton label="More actions" icon="more" size="row" iconSize="xs" />
            </DropdownMenu>
          )}
        </RowActions>
      </div>
    </ContextMenu>
  );
}
