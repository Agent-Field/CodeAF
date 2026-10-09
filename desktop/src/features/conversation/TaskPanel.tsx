import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { Button, IconButton, Text } from '../../components/ui';
import type { EngineTaskRow } from '../chat/engine-client';
import { StateMark } from './StateMark';
import { buildTaskTree, taskCounts, type TaskNode } from './taskTree';
import './task-panel.css';

export type TaskPanelProps = {
  tasks: EngineTaskRow[];
  planError?: string;
  currentTaskId?: string;
  onOpenTask: (taskId: string, background: boolean) => void;
  onClose: () => void;
  variant: 'column' | 'sheet';
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

function liveStep(row: EngineTaskRow): string {
  return row.Status === 'running' || row.Status === 'claimed' ? (row.Live?.Command ?? '').trim() : '';
}

type BranchProps = {
  node: TaskNode;
  collapsed: Set<string>;
  currentTaskId?: string;
  onToggle: (id: string) => void;
  onOpenTask: TaskPanelProps['onOpenTask'];
};

function Branch({ node, collapsed, currentTaskId, onToggle, onOpenTask }: BranchProps) {
  const { row, children } = node;
  const isParent = children.length > 0;
  const expanded = !collapsed.has(row.ID);
  const step = liveStep(row);
  return (
    <li className="task-panel-item">
      <div className="task-panel-row" data-current={currentTaskId === row.ID || undefined}>
        {isParent ? (
          <IconButton
            className="task-panel-disclosure"
            label={`${expanded ? 'Collapse' : 'Expand'} ${row.Title}`}
            icon={expanded ? 'chevron' : 'chevronRight'}
            iconSize="xs"
            aria-expanded={expanded}
            tabIndex={-1}
            onClick={() => onToggle(row.ID)}
          />
        ) : (
          <span className="task-panel-spacer" aria-hidden="true" />
        )}
        <Button
          className="task-panel-main"
          data-task-id={row.ID}
          data-parent={isParent || undefined}
          data-expanded={isParent ? expanded : undefined}
          aria-current={currentTaskId === row.ID ? 'true' : undefined}
          onClick={(event) => onOpenTask(row.ID, event.metaKey || event.ctrlKey)}
        >
          <StateMark status={row.Status} stopped={row.Stopped} />
          <span className="task-panel-text">
            <span className="task-panel-title">{row.Title}</span>
            {step && <span className="task-panel-step">{step}</span>}
          </span>
        </Button>
      </div>
      {isParent && expanded && (
        <ul className="task-panel-list task-panel-children">
          {children.map((child) => (
            <Branch key={child.row.ID} {...{ node: child, collapsed, currentTaskId, onToggle, onOpenTask }} />
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

export function TaskPanel({ tasks, planError, currentTaskId, onOpenTask, onClose, variant }: TaskPanelProps) {
  const panel = useRef<HTMLElement>(null);
  const tree = useMemo(() => buildTaskTree(tasks), [tasks]);
  const { done, total } = taskCounts(tasks);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
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

  return (
    <aside ref={panel} className="task-panel" data-variant={variant} aria-label="Tasks" tabIndex={-1} onKeyDown={onKeyDown}>
      <header className="task-panel-header">
        <span className="task-panel-heading">Tasks</span>
        {total > 0 && <span className="task-panel-count">{done} of {total}</span>}
        <IconButton className="task-panel-close" label="Close tasks" icon="close" iconSize="sm" onClick={onClose} />
      </header>
      {planError && <Text className="task-panel-error">{planError}</Text>}
      <ul className="task-panel-list task-panel-scroll">
        {tree.map((node) => (
          <Branch key={node.row.ID} {...{ node, collapsed, currentTaskId, onToggle: toggle, onOpenTask }} />
        ))}
      </ul>
    </aside>
  );
}
