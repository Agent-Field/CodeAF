import type { MouseEvent } from 'react';
import { Button, Icon } from '../../components/ui';
import { isMac } from '../../design/keyboard';
import { StateMark } from './StateMark';
import type { TurnItem } from './types';
import './notice.css';

type TaskItem = Extract<TurnItem, { kind: 'task' }>;

export type OpenTask = (taskId: string, background: boolean) => void;

type TaskNoticeProps = {
  item: TaskItem;
  /** The running task's live command, drawn in mono under the title. */
  live?: string;
  open: boolean;
  onToggle: () => void;
  onOpenTask?: OpenTask;
};

function wantsBackground(event: MouseEvent) {
  return isMac ? event.metaKey : event.ctrlKey;
}

export function TaskNotice({ item, live, open, onToggle, onOpenTask }: TaskNoticeProps) {
  const { taskId } = item;
  const opensTask = Boolean(taskId && onOpenTask);
  const bodyId = `${item.id}-body`;

  function activate(event: MouseEvent) {
    if (taskId && onOpenTask) {
      onOpenTask(taskId, wantsBackground(event));
      return;
    }
    onToggle();
  }

  return (
    <div className="task-notice" data-open={open || undefined}>
      <div className="task-notice-line">
        <Button className="task-notice-row" aria-expanded={opensTask ? undefined : open} onClick={activate}>
          <StateMark status={item.status} />
          <span className="task-notice-title">{item.title}</span>
          {item.summary && <span className="task-notice-summary">{item.summary}</span>}
          <Icon name="chevronRight" size="xs" />
        </Button>
        {opensTask && item.body && (
          <Button className="task-notice-report" aria-expanded={open} aria-controls={bodyId} onClick={onToggle}>
            Report
          </Button>
        )}
      </div>
      {live && <p className="task-notice-live">{live}</p>}
      {open && item.body && (
        <div id={bodyId} className="task-notice-body">
          {item.body}
        </div>
      )}
    </div>
  );
}
