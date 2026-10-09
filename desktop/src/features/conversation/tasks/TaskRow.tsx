import { Button, CodeText, ContextMenu, IconButton } from '../../../components/ui';
import type { EngineTaskRow } from '../../chat/engine-client';
import { StateMark } from '../StateMark';
import { rowFlags, rowKind } from '../taskState';
import { liveStep, rowAge, waitsText } from './rowText';
import { taskActions, type TaskCommands } from './taskMenu';
import { waitTitles } from '../taskTree';
import './task-row.css';

export type TaskRowProps = {
  row: EngineTaskRow;
  /** Every row of the conversation, to name what a queued row waits on. */
  rows: EngineTaskRow[];
  now: number;
  current: boolean;
  /** Set on parents only: whether their children are showing. */
  expanded?: boolean;
  onToggle: (taskId: string) => void;
  commands: TaskCommands;
};

function Disclosure({ row, expanded, onToggle }: Pick<TaskRowProps, 'row' | 'expanded' | 'onToggle'>) {
  if (expanded === undefined) return <span className="task-row-spacer" aria-hidden="true" />;
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

function SecondLine({ row, rows, now }: Pick<TaskRowProps, 'row' | 'rows' | 'now'>) {
  const kind = rowKind(row);
  const step = liveStep(row, kind, now);
  const waits = waitsText(kind, waitTitles(rows, row));
  if (step) {
    return (
      <span className="task-row-step">
        <CodeText className="task-row-command">{step.command}</CodeText>
        {step.clock && <span className="task-row-clock">{step.clock}</span>}
      </span>
    );
  }
  return waits ? <span className="task-row-step">{waits}</span> : null;
}

export function TaskRow(props: TaskRowProps) {
  const { row, now, current, commands } = props;
  const kind = rowKind(row);
  const actions = taskActions(row, commands);
  return (
    <ContextMenu label={`${row.Title} actions`} items={actions}>
      <div className="task-row" data-kind={kind} data-current={current || undefined}>
        <Disclosure row={row} expanded={props.expanded} onToggle={props.onToggle} />
        <Button
          className="task-panel-main"
          data-task-id={row.ID}
          data-parent={props.expanded !== undefined || undefined}
          data-expanded={props.expanded}
          aria-current={current ? 'true' : undefined}
          onClick={(event) => commands.onOpenTask(row.ID, event.metaKey || event.ctrlKey)}
        >
          <StateMark status={row.Status} {...rowFlags(row)} />
          <span className="task-row-text">
            <span className="task-row-line">
              <span className="task-row-title" title={row.Title}>{row.Title}</span>
              <span className="task-row-age">{rowAge(row, kind, now)}</span>
            </span>
            <SecondLine row={row} rows={props.rows} now={now} />
          </span>
        </Button>
        <span className="task-row-actions">
          {actions.map((action) => (
            <IconButton key={action.id} label={action.label} icon={action.icon ?? 'more'} iconSize="xs" onClick={action.onSelect} />
          ))}
        </span>
      </div>
    </ContextMenu>
  );
}
