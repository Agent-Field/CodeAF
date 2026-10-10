import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Button, CodeText, Icon, IconButton, TextInput } from '../../../components/ui';
import type { EngineTaskRow } from '../../chat/engine-client';
import { rowFlags, rowKind } from '../taskState';
import { TaskMark } from './TaskMark';
import { buildTaskTree, taskProgress, waitTitles, type TaskNode } from '../taskTree';
import { liveStep, rowAge, waitsText } from './rowText';
import {
  costText, FILTERS, filterTree, groupCount, metaParts, stateWord, stepsText, stripCounts, tabCounts, totals, waitReason,
  type StripCounts, type TableFilter,
} from './tasksTableModel';
import { useNow } from './useNow';
import './tasks-table.css';

export type TasksTableProps = {
  tasks: EngineTaskRow[];
  filter?: TableFilter;
  onFilter?: (filter: TableFilter) => void;
  selectedId?: string;
  onSelect: (taskId: string) => void;
  /** Leave the expanded view (the arrow at the head). */
  onClose: () => void;
  /** Why a task waits on the person ("Allow 3 git actions?"), by task id; absent when the engine has not said. */
  reasons?: Readonly<Record<string, string>>;
  /** The detail pane's place, to the right of the table. */
  detail?: ReactNode;
  /** Row ages are measured to this clock; when absent the table keeps its own. */
  now?: number;
};

type Shared = Pick<TasksTableProps, 'selectedId' | 'onSelect' | 'reasons'> & {
  rows: EngineTaskRow[];
  now: number;
  collapsed: Set<string>;
  onToggle: (id: string) => void;
};

function Head({ tasks, onClose }: Pick<TasksTableProps, 'tasks' | 'onClose'>) {
  const { usd, steps } = totals(tasks);
  return (
    <header className="tasks-table-head">
      <IconButton label="Back to the conversation" icon="back" iconSize="sm" onClick={onClose} />
      <span className="tasks-table-heading">Tasks</span>
      <span className="tasks-table-totals">
        {usd > 0 && <span>{costText(usd)} spent</span>}
        {steps > 0 && <span>{stepsText(steps)}</span>}
      </span>
      <IconButton className="tasks-table-collapse" label="Collapse tasks" icon="shrink" iconSize="xs" onClick={onClose} />
    </header>
  );
}

type FiltersProps = {
  counts: Record<TableFilter, number>;
  filter: TableFilter;
  onFilter: (filter: TableFilter) => void;
  query: string;
  onQuery: (query: string) => void;
};

function Search({ query, onQuery }: Pick<FiltersProps, 'query' | 'onQuery'>) {
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (open) input.current?.focus();
  }, [open]);
  if (!open) return <IconButton className="tasks-table-search-open" label="Search tasks" icon="search" iconSize="sm" onClick={() => setOpen(true)} />;
  const close = () => { onQuery(''); setOpen(false); };
  return (
    <span className="tasks-table-search">
      <Icon name="search" size="sm" />
      <TextInput ref={input} aria-label="Search tasks" placeholder="Search tasks" value={query} onChange={(event) => onQuery(event.target.value)} onKeyDown={(event) => event.key === 'Escape' && close()} />
      <IconButton label="Close search" icon="close" iconSize="xs" onClick={close} />
    </span>
  );
}

function Filters(props: FiltersProps) {
  return (
    <div className="tasks-table-filters" role="group" aria-label="Filter tasks">
      {FILTERS.map(({ id, label }) => (
        <Button key={id} className="tasks-table-tab" data-filter={id} aria-pressed={props.filter === id} onClick={() => props.onFilter(id)}>
          {id === 'needs' && <span className="tasks-table-tab-dot" aria-hidden="true" />}
          {label}
          <span className="tasks-table-tab-count">{props.counts[id]}</span>
        </Button>
      ))}
    </div>
  );
}

const SEGMENTS: readonly (keyof StripCounts)[] = ['done', 'running', 'needs', 'failed', 'queued'];

/** One bar with a cell per task, grouped by state, so each state is as wide as its share. */
function Strip({ counts }: { counts: StripCounts }) {
  const summary = SEGMENTS.filter((name) => counts[name] > 0).map((name) => `${counts[name]} ${name}`).join(', ');
  if (!summary) return null;
  return (
    <div className="tasks-table-strip" role="img" aria-label={summary}>
      {SEGMENTS.flatMap((name) =>
        Array.from({ length: counts[name] }, (_, index) => (
          <span key={`${name}-${index}`} className="tasks-table-strip-cell" data-segment={name} data-start={index === 0 || undefined} data-end={index === counts[name] - 1 || undefined} />
        )),
      )}
    </div>
  );
}

function Mark({ row }: { row: EngineTaskRow }) {
  return <TaskMark dense status={row.Status} {...rowFlags(row)} />;
}

/** The row's second line: the live command, or the reason it waits; model, steps and cost only as the engine gave them. */
function Detail({ row, shared }: { row: EngineTaskRow; shared: Shared }) {
  const kind = rowKind(row);
  const step = liveStep(row, kind, shared.now);
  const reason = kind === 'yourcall' ? shared.reasons?.[row.ID] ?? '' : waitReason(waitsText(kind, waitTitles(shared.rows, row)));
  const meta = metaParts(row);
  const lead = step ? <CodeText className="tasks-table-command">{step.command}</CodeText> : reason;
  if (!lead && meta.length === 0) return null;
  return (
    <span className="tasks-table-detail">
      {lead}
      {lead && meta.length > 0 && ' · '}
      {meta.join(' · ')}
    </span>
  );
}

function TaskLine({ row, shared }: { row: EngineTaskRow; shared: Shared }) {
  const kind = rowKind(row);
  return (
    <Button
      className="tasks-table-row"
      data-kind={kind}
      data-task-id={row.ID}
      aria-current={shared.selectedId === row.ID ? 'true' : undefined}
      onClick={() => shared.onSelect(row.ID)}
    >
      <span className="tasks-table-title-cell">
        <Mark row={row} />
        <span className="tasks-table-text">
          <span className="tasks-table-title" title={row.Title}>{row.Title}</span>
          <Detail row={row} shared={shared} />
        </span>
      </span>
      <span className="tasks-table-state">{stateWord(row)}</span>
      <span className="tasks-table-age">{rowAge(row, kind, shared.now)}</span>
    </Button>
  );
}

function GroupLine({ node, shared, open }: { node: TaskNode; shared: Shared; open: boolean }) {
  const { row } = node;
  return (
    <Button className="tasks-table-group" data-task-id={row.ID} aria-expanded={open} onClick={() => shared.onToggle(row.ID)}>
      <span className="tasks-table-group-title">
        <Icon name={open ? 'chevron' : 'chevronRight'} size="xs" />
        <span className="tasks-table-title" title={row.Title}>{row.Title}</span>
      </span>
      <span className="tasks-table-group-count">{groupCount(node)}</span>
      <span className="tasks-table-age">{rowAge(row, rowKind(row), shared.now)}</span>
    </Button>
  );
}

/** A top-level task with subtasks is a group head; any other task is a row, with its own subtasks guided beneath it. */
function Branch({ node, shared, depth }: { node: TaskNode; shared: Shared; depth: number }) {
  const { row, children } = node;
  const group = depth === 0 && children.length > 0;
  const open = !shared.collapsed.has(row.ID);
  return (
    <li className="tasks-table-item" data-group={group || undefined}>
      {group ? <GroupLine node={node} shared={shared} open={open} /> : <TaskLine row={row} shared={shared} />}
      {children.length > 0 && open && (
        <ul className="tasks-table-list tasks-table-children">
          {children.map((child) => <Branch key={child.row.ID} node={child} shared={shared} depth={depth + 1} />)}
        </ul>
      )}
    </li>
  );
}

/** True while the list has more beneath what shows, so the fade appears only on an edge with content beyond it. */
function useMoreBelow(deps: readonly unknown[]) {
  const list = useRef<HTMLDivElement>(null);
  const [more, setMore] = useState(false);
  const measure = () => {
    const el = list.current;
    if (el) setMore(el.scrollHeight - el.scrollTop - el.clientHeight > 1);
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(measure, deps);
  useEffect(() => {
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, []);
  return { list, more, measure };
}

export function TasksTable(props: TasksTableProps) {
  const { tasks, selectedId, onSelect, onClose, reasons, detail } = props;
  const [localFilter, setLocalFilter] = useState<TableFilter>('all');
  const filter = props.filter ?? localFilter;
  const setFilter = props.onFilter ?? setLocalFilter;
  const [query, setQuery] = useState('');
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const now = useNow(taskProgress(tasks).running > 0, props.now);
  const tree = useMemo(() => filterTree(buildTaskTree(tasks), filter, query), [tasks, filter, query]);
  const { list, more, measure } = useMoreBelow([tree, collapsed]);

  const toggle = (id: string) =>
    setCollapsed((before) => {
      const next = new Set(before);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const shared: Shared = { rows: tasks, now, selectedId, onSelect, reasons, collapsed, onToggle: toggle };

  return (
    <section className="tasks-table" aria-label="Tasks">
      <Head tasks={tasks} onClose={onClose} />
      <div className="tasks-table-body">
        <div className="tasks-table-main">
          <div className="tasks-table-bar">
            <Filters counts={tabCounts(tasks)} filter={filter} onFilter={setFilter} query={query} onQuery={setQuery} />
            <Search query={query} onQuery={setQuery} />
          </div>
          <Strip counts={stripCounts(tasks)} />
          <div ref={list} className="tasks-table-scroll" data-scroll-key="tasks-table" data-more={more || undefined} onScroll={measure}>
            <ul className="tasks-table-list">
              {tree.map((node) => <Branch key={node.row.ID} node={node} shared={shared} depth={0} />)}
            </ul>
          </div>
        </div>
        {detail}
      </div>
    </section>
  );
}
