import type { MouseEvent } from 'react';
import { Button, ContextMenu, Icon } from '../../components/ui';
import type { EngineTaskRow } from '../chat/engine-client';
import { isMac } from '../../design/keyboard';
import type { TurnItem } from './types';
import './task-notice.css';
import { TaskMark } from './tasks/TaskMark';
import { noticeActions } from './tasks/taskMenu';

type TaskItem = Extract<TurnItem, { kind: 'task' }>;

export type OpenTask = (taskId: string, background: boolean) => void;

type TaskNoticeProps = {
  item: TaskItem;
  /** The running task's live command, drawn in mono under the title. */
  live?: string;
  open: boolean;
  onToggle: () => void;
  onOpenTask?: OpenTask;
  /** Plan rows for this conversation. The menu reads the newest row for this notice. */
  tasks?: readonly EngineTaskRow[];
  onPause?: (taskId: string) => void;
  onResume?: (taskId: string) => void;
  onStop?: (taskId: string) => void;
};

function wantsBackground(event: MouseEvent) {
  return isMac ? event.metaKey : event.ctrlKey;
}

/** Chromium can paste the PRIMARY selection into the focused composer after a middle click. Swallow only that paste. */
function swallowMiddlePaste(event: MouseEvent) {
  const doc = event.currentTarget.ownerDocument;
  if (!doc) return;
  const preventPaste = (paste: Event) => paste.preventDefault();
  doc.addEventListener('paste', preventPaste, { capture: true, once: true });
  doc.defaultView?.setTimeout(() => doc.removeEventListener('paste', preventPaste, true), 0);
}

/** The notice's own mark: the shared task mark, centred on the notice's first line. */
function NoticeMark({ status }: { status: string }) {
  return (
    <span className="task-notice-mark">
      <TaskMark status={status} />
    </span>
  );
}

export function TaskNotice({ item, live, open, onToggle, onOpenTask, tasks, onPause, onResume, onStop }: TaskNoticeProps) {
  const { taskId } = item;
  const opensTask = Boolean(taskId && onOpenTask);
  const bodyId = `${item.id}-body`;
  const menu = taskId && onOpenTask ? noticeActions(taskId, tasks ?? [], { onOpenTask, onPause, onResume, onStop }) : [];

  function activate(event: MouseEvent) {
    if (taskId && onOpenTask) {
      onOpenTask(taskId, wantsBackground(event));
      return;
    }
    onToggle();
  }

  // Middle-click is the same background open as ⌘-click, on the line that opens the task.
  // preventDefault on mousedown stops the browser's middle-button autoscroll.
  function openInBackground(event: MouseEvent) {
    if (event.button !== 1 || !taskId || !onOpenTask) return;
    event.preventDefault();
    swallowMiddlePaste(event);
    onOpenTask(taskId, true);
  }

  const notice = (
    <div className="task-notice" data-open={open || undefined} data-live={live ? true : undefined}>
      <div className="task-notice-line">
        <Button
          className="task-notice-row"
          aria-expanded={opensTask ? undefined : open}
          onClick={activate}
          onAuxClick={openInBackground}
          onMouseDown={(event) => { if (event.button === 1) event.preventDefault(); }}
        >
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

  return menu.length > 0 ? <ContextMenu label={`${item.title} actions`} items={menu}>{notice}</ContextMenu> : notice;
}
