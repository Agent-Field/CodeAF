import type { MouseEvent } from 'react';
import { Button, Icon } from '../../components/ui';
import { isMac } from '../../design/keyboard';
import { taskMark } from './taskState';
import type { TurnItem } from './types';
import './task-notice.css';

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

/** The notice's own mark: a 6px dot while it is running or waits on you, the quiet icon otherwise. */
function NoticeMark({ status }: { status: string }) {
  const mark = taskMark(status);
  const dot = mark.kind === 'running' || mark.kind === 'yourcall';
  return (
    <span className="task-notice-mark" data-kind={mark.kind} role="img" aria-label={mark.label}>
      {dot ? <span className="task-notice-dot" /> : <Icon name={mark.icon} size="xs" />}
    </span>
  );
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
    <div className="task-notice" data-open={open || undefined} data-live={live ? true : undefined}>
      <div className="task-notice-line">
        <Button className="task-notice-row" aria-expanded={opensTask ? undefined : open} onClick={activate}>
          <NoticeMark status={item.status} />
          <span className="task-notice-main">
            <span className="task-notice-head">
              <span className="task-notice-title">{item.title}</span>
              {item.summary && <span className="task-notice-summary">{item.summary}</span>}
            </span>
            {live && <span className="task-notice-live">{live}</span>}
          </span>
          <span className="task-notice-chevron"><Icon name="chevronRight" size="xs" /></span>
        </Button>
        {opensTask && item.body && (
          <Button className="task-notice-report" aria-expanded={open} aria-controls={bodyId} onClick={onToggle}>
            Report
          </Button>
        )}
      </div>
      {open && item.body && (
        <div id={bodyId} className="task-notice-body">
          {item.body}
        </div>
      )}
    </div>
  );
}
