import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { IconButton, Text } from '../../components/ui';
import type { EngineTaskRow } from '../chat/engine-client';
import { tokensText } from './tasks/rowText';
import type { TaskCommands } from './tasks/taskMenu';
import { TaskProgressStrip } from './tasks/TaskProgress';
import { TaskRow } from './tasks/TaskRow';
import { useNow } from './tasks/useNow';
import { buildTaskTree, taskProgress, type TaskNode } from './taskTree';
import './task-panel.css';

export type TaskPanelProps = {
  tasks: EngineTaskRow[];
  planError?: string;
  currentTaskId?: string;
  onOpenTask: (taskId: string, background: boolean) => void;
  onClose: () => void;
  variant: 'column' | 'sheet';
  /** Row ages are measured to this clock; when absent the panel keeps its own, ticking while work runs. */
  now?: number;
  onPause?: (taskId: string) => void;
  onResume?: (taskId: string) => void;
  onStop?: (taskId: string) => void;
};

const rowSelector = '.task-panel-main';

/** A sheet takes focus when it opens and gives it back when it closes. */
function useSheetFocus(variant: TaskPanelProps['variant'], panel: RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (variant !== 'sheet') return;
    const opener = document.activeElement as HTMLElement | null;
    const first = panel.current?.querySelector<HTMLElement>(rowSelector);
    (first ?? panel.current)?.focus();
    return () => opener?.focus?.();
  }, [variant, panel]);
}

type BranchProps = {
  node: TaskNode;
  rows: EngineTaskRow[];
  now: number;
  collapsed: Set<string>;
  currentTaskId?: string;
  onToggle: (id: string) => void;
  commands: TaskCommands;
};

function Branch(props: BranchProps) {
  const { node, collapsed, currentTaskId } = props;
  const { row, children } = node;
  const isParent = children.length > 0;
  const expanded = !collapsed.has(row.ID);
  return (
    <li className="task-panel-item">
      <TaskRow
        row={row}
        rows={props.rows}
        now={props.now}
        current={currentTaskId === row.ID}
        expanded={isParent ? expanded : undefined}
        onToggle={props.onToggle}
        commands={props.commands}
      />
      {isParent && expanded && (
        <ul className="task-panel-list task-panel-children">
          {children.map((child) => (
            <Branch key={child.row.ID} {...props} node={child} />
          ))}
        </ul>
      )}
    </li>
  );
}

function moveFocus(list: HTMLElement, from: HTMLElement, step: number) {
  const rows = Array.from(list.querySelectorAll<HTMLElement>(rowSelector));
  rows[rows.indexOf(from) + step]?.focus();
}

function Header({ tasks, onClose }: Pick<TaskPanelProps, 'tasks' | 'onClose'>) {
  const progress = taskProgress(tasks);
  const tokens = tokensText(progress.tokens);
  return (
    <header className="task-panel-header">
      <div className="task-panel-headline">
        <span className="task-panel-heading">Tasks</span>
        {progress.total > 0 && <span className="task-panel-count">{progress.done} of {progress.total}</span>}
        <IconButton className="task-panel-close" label="Close tasks" icon="close" iconSize="sm" onClick={onClose} />
      </div>
      <TaskProgressStrip progress={progress} />
      {tokens && <span className="task-panel-tokens">{tokens} tokens</span>}
    </header>
  );
}

export function TaskPanel(props: TaskPanelProps) {
  const { tasks, planError, currentTaskId, onOpenTask, onClose, variant } = props;
  const panel = useRef<HTMLElement>(null);
  const tree = useMemo(() => buildTaskTree(tasks), [tasks]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const now = useNow(taskProgress(tasks).running > 0, props.now);
  useSheetFocus(variant, panel);

  const toggle = (id: string) =>
    setCollapsed((before) => {
      const next = new Set(before);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  function onKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key === 'Escape' && variant === 'sheet') {
      event.stopPropagation();
      onClose();
      return;
    }
    const row = (event.target as HTMLElement).closest<HTMLElement>(rowSelector);
    if (!row) return;
    const steps: Record<string, number> = { ArrowDown: 1, ArrowUp: -1 };
    if (event.key in steps) {
      event.preventDefault();
      moveFocus(panel.current as HTMLElement, row, steps[event.key]);
    }
    const open = event.key === 'ArrowRight';
    if ((open || event.key === 'ArrowLeft') && row.dataset.parent && (row.dataset.expanded === 'true') !== open) {
      event.preventDefault();
      toggle(row.dataset.taskId as string);
    }
  }

  const commands: TaskCommands = { onOpenTask, onPause: props.onPause, onResume: props.onResume, onStop: props.onStop };
  return (
    <aside ref={panel} className="task-panel" data-variant={variant} aria-label="Tasks" tabIndex={-1} onKeyDown={onKeyDown}>
      <Header tasks={tasks} onClose={onClose} />
      {planError && <Text className="task-panel-error">{planError}</Text>}
      <ul className="task-panel-list task-panel-scroll">
        {tree.map((node) => (
          <Branch key={node.row.ID} {...{ node, rows: tasks, now, collapsed, currentTaskId, onToggle: toggle, commands }} />
        ))}
      </ul>
    </aside>
  );
}
