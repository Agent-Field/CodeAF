import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { Button, Icon, IconButton, Text } from '../../components/ui';
import type { EngineTaskRow } from '../chat/engine-client';
import type { TaskCommands } from './tasks/taskMenu';
import { TaskProgressStrip } from './tasks/TaskProgress';
import { TaskTree } from './tasks/TaskTree';
import { useNow } from './tasks/useNow';
import { useScrollEdges } from './tasks/useScrollEdges';
import { buildTaskTree, foldDefault, holdsTask, splitFinished, taskProgress } from './taskTree';
import './task-panel.css';

export type TaskPanelProps = {
  tasks: EngineTaskRow[];
  planError?: string;
  currentTaskId?: string;
  onOpenTask: (taskId: string, background: boolean) => void;
  onClose: () => void;
  /** Opens the tasks as a full table; the control is absent until the app has one. */
  onExpand?: () => void;
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

function moveFocus(list: HTMLElement, from: HTMLElement, step: number) {
  const rows = Array.from(list.querySelectorAll<HTMLElement>(rowSelector));
  rows[rows.indexOf(from) + step]?.focus();
}

function Header({ tasks, onClose, onExpand }: Pick<TaskPanelProps, 'tasks' | 'onClose' | 'onExpand'>) {
  const progress = taskProgress(tasks);
  return (
    <header className="task-panel-header">
      <div className="task-panel-headline">
        <span className="task-panel-heading">Tasks</span>
        {progress.total > 0 && <span className="task-panel-count">{progress.done} of {progress.total}</span>}
        <span className="task-panel-controls">
          {onExpand && <IconButton label="Expand tasks" icon="expand" iconSize="xs" onClick={onExpand} />}
          <IconButton className="task-panel-close" label="Close tasks" icon="close" iconSize="xs" onClick={onClose} />
        </span>
      </div>
      <TaskProgressStrip progress={progress} />
    </header>
  );
}

type FoldProps = { count: number; open: boolean; onToggle: () => void };

/** Finished families wait behind one quiet line at the foot of the panel. */
function FinishedFold({ count, open, onToggle }: FoldProps) {
  return (
    <Button className="task-panel-fold" aria-expanded={open} onClick={onToggle}>
      <Icon name={open ? 'chevron' : 'chevronRight'} size="xs" />
      <span>Finished</span>
      <span className="task-panel-fold-count">{count}</span>
    </Button>
  );
}

export function TaskPanel(props: TaskPanelProps) {
  const { tasks, planError, currentTaskId, onOpenTask, onClose, variant } = props;
  const panel = useRef<HTMLElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const { live, finished, finishedCount } = useMemo(() => splitFinished(buildTaskTree(tasks)), [tasks]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [heldAtOpen] = useState(() => finished.some((node) => holdsTask(node, currentTaskId)));
  const [foldChoice, setFoldChoice] = useState<boolean | undefined>(undefined);
  // With nothing live the tree would be an empty middle, so the finished work is the panel.
  const foldOpen = foldChoice ?? foldDefault(live.length, heldAtOpen);
  const now = useNow(taskProgress(tasks).running > 0, props.now);
  const edges = useScrollEdges(scroller);
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
  const tree = { rows: tasks, now, collapsed, currentTaskId, onToggle: toggle, commands };
  return (
    <aside ref={panel} className="task-panel" data-variant={variant} aria-label="Tasks" tabIndex={-1} onKeyDown={onKeyDown}>
      <Header tasks={tasks} onClose={onClose} onExpand={props.onExpand} />
      {planError && <Text className="task-panel-error">{planError}</Text>}
      <div ref={scroller} className="task-panel-scroll" data-scroll-key="task-panel" data-above={edges.above || undefined} data-below={edges.below || undefined}>
        <div className="task-panel-body">
          <TaskTree {...tree} nodes={live} />
          {foldOpen && <TaskTree {...tree} nodes={finished} />}
        </div>
      </div>
      {finishedCount > 0 && <FinishedFold count={finishedCount} open={foldOpen} onToggle={() => setFoldChoice(!foldOpen)} />}
    </aside>
  );
}
