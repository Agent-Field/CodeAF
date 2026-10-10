// The words, counts and filters of the expanded tasks table. Pure, so each rule
// is a unit test. A figure is empty when the engine has not said it.

import type { EngineTaskRow } from '../../chat/engine-client';
import { buildTaskTree, type TaskNode } from '../taskTree.ts';
import { rowKind, taskMark, type TaskKind } from '../taskState.ts';

export type { TasksFilter as TableFilter } from '../../tabs/view-state';
import type { TasksFilter as TableFilter } from '../../tabs/view-state';

export const FILTERS: readonly { id: TableFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'needs', label: 'Needs you' },
  { id: 'running', label: 'Running' },
  { id: 'done', label: 'Done' },
];

const KIND_OF: Record<Exclude<TableFilter, 'all'>, TaskKind> = { needs: 'yourcall', running: 'running', done: 'done' };

const FAILED: ReadonlySet<TaskKind> = new Set(['incomplete', 'stopped', 'interrupted']);

export function flatten(nodes: TaskNode[]): EngineTaskRow[] {
  return nodes.flatMap((node) => [node.row, ...flatten(node.children)]);
}

export const matchesFilter = (row: EngineTaskRow, filter: TableFilter): boolean =>
  filter === 'all' || rowKind(row) === KIND_OF[filter];

const matchesQuery = (row: EngineTaskRow, query: string): boolean =>
  !query.trim() || row.Title.toLowerCase().includes(query.trim().toLowerCase());

/** Keeps a row that matches, and the ancestors of a row that matches, so a hit keeps its group. */
export function filterTree(nodes: TaskNode[], filter: TableFilter, query: string): TaskNode[] {
  return nodes.flatMap((node) => {
    const children = filterTree(node.children, filter, query);
    const hit = matchesFilter(node.row, filter) && matchesQuery(node.row, query);
    return hit || children.length > 0 ? [{ row: node.row, children }] : [];
  });
}

export type TabCounts = Record<TableFilter, number>;

export function tabCounts(rows: EngineTaskRow[]): TabCounts {
  const all = flatten(buildTaskTree(rows));
  const count = (filter: TableFilter) => all.filter((row) => matchesFilter(row, filter)).length;
  return { all: all.length, needs: count('needs'), running: count('running'), done: count('done') };
}

export type StripCounts = { done: number; running: number; needs: number; failed: number; queued: number };

/** The progress strip's five groups, in the order they are drawn. */
export function stripCounts(rows: EngineTaskRow[]): StripCounts {
  const kinds = flatten(buildTaskTree(rows)).map(rowKind);
  const count = (test: (kind: TaskKind) => boolean) => kinds.filter(test).length;
  const done = count((kind) => kind === 'done');
  const running = count((kind) => kind === 'running');
  const needs = count((kind) => kind === 'yourcall');
  const failed = count((kind) => FAILED.has(kind));
  return { done, running, needs, failed, queued: kinds.length - done - running - needs - failed };
}

/** Cost and steps over every task; zero when the engine reported none. */
export function totals(rows: EngineTaskRow[]): { usd: number; steps: number } {
  const all = flatten(buildTaskTree(rows));
  return { usd: all.reduce((sum, row) => sum + (row.USD ?? 0), 0), steps: all.reduce((sum, row) => sum + (row.Steps ?? 0), 0) };
}

/** "2 of 7" over a group's tasks, the group head excluded. */
export function groupCount(node: TaskNode): string {
  const tasks = flatten(node.children);
  return `${tasks.filter((row) => row.Status === 'done').length} of ${tasks.length}`;
}

export const costText = (usd: number): string => (usd > 0 ? `$${usd.toFixed(2)}` : '');

export const stepsText = (steps: number): string => (steps > 0 ? `${steps} ${steps === 1 ? 'step' : 'steps'}` : '');

const modelName = (model?: string): string => (model ?? '').split('/').pop() ?? '';

/** Model, steps, cost: only the ones the engine provided, in that order. */
export function metaParts(row: EngineTaskRow): string[] {
  return [modelName(row.Model), stepsText(row.Steps ?? 0), costText(row.USD ?? 0)].filter(Boolean);
}

/** "Waits on Parse config": the reason a queued row is not running. */
export const waitReason = (text: string): string => (text ? `${text[0].toUpperCase()}${text.slice(1)}` : '');

/** The state word column. A task waiting on a person says what the filter says. */
export const stateWord = (row: EngineTaskRow): string => {
  const kind = rowKind(row);
  return kind === 'yourcall' ? 'Needs you' : taskMark(row.Status, { stopped: row.Stopped, interrupted: row.Interrupted, hold: row.Hold, paused: row.Paused }).label;
};
