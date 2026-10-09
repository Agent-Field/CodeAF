import type { EngineTaskRow } from '../chat/engine-client';
import { rowKind, type TaskKind } from './taskState.ts';

export type TaskNode = { row: EngineTaskRow; children: TaskNode[] };

function isRoot(row: EngineTaskRow, ids: Set<string>): boolean {
  return !row.Parent || row.Parent === row.ID || !ids.has(row.Parent);
}

function childrenByParent(rows: EngineTaskRow[], ids: Set<string>) {
  const byParent = new Map<string, EngineTaskRow[]>();
  for (const row of rows) {
    if (isRoot(row, ids)) continue;
    const siblings = byParent.get(row.Parent as string) ?? [];
    siblings.push(row);
    byParent.set(row.Parent as string, siblings);
  }
  return byParent;
}

/** The conversation's tasks, each id once. Rows arrive oldest run first and the
 * live run last, so a later row with the same id is the newer record of it. A task
 * from an ended run stays listed: it is still this conversation's work. */
export function visibleRows(rows: EngineTaskRow[]): EngineTaskRow[] {
  const byId = new Map<string, EngineTaskRow>();
  for (const row of rows) {
    byId.delete(row.ID);
    byId.set(row.ID, row);
  }
  return [...byId.values()];
}

const hasRunning = (node: TaskNode): boolean => rowKind(node.row) === 'running' || node.children.some(hasRunning);

/** Families with running work first, at every level; otherwise the store's order. */
function runningFirst(nodes: TaskNode[]): TaskNode[] {
  const ordered = nodes.map((node) => ({ ...node, children: runningFirst(node.children) }));
  return [...ordered.filter(hasRunning), ...ordered.filter((node) => !hasRunning(node))];
}

/** A row whose parent is missing becomes a root; a corrupt cycle is surfaced as
 * roots, never dropped. */
export function buildTaskTree(rows: EngineTaskRow[]): TaskNode[] {
  const live = visibleRows(rows);
  const ids = new Set(live.map((row) => row.ID));
  const byParent = childrenByParent(live, ids);
  const seen = new Set<string>();
  const grow = (row: EngineTaskRow): TaskNode => {
    seen.add(row.ID);
    const kids = (byParent.get(row.ID) ?? []).filter((child) => !seen.has(child.ID));
    return { row, children: kids.map(grow) };
  };
  const roots = live.filter((row) => isRoot(row, ids)).map(grow);
  for (const row of live) {
    if (!seen.has(row.ID)) roots.push(grow(row));
  }
  return runningFirst(roots);
}

function flatten(nodes: TaskNode[]): EngineTaskRow[] {
  return nodes.flatMap((node) => [node.row, ...flatten(node.children)]);
}

/** Every row the person sees in the panel counts once, parents included: "1 of 2"
 * must match the two rows on screen. A parent is done when the engine says so. */
export function taskCounts(rows: EngineTaskRow[]): { done: number; total: number } {
  const all = flatten(buildTaskTree(rows));
  return { done: all.filter((row) => row.Status === 'done').length, total: all.length };
}

/** The task and its ancestors, outermost first; a cycle stops the walk. */
export function taskTrail(rows: EngineTaskRow[], taskId: string): EngineTaskRow[] {
  const byId = new Map(visibleRows(rows).map((row) => [row.ID, row]));
  const trail: EngineTaskRow[] = [];
  let row = byId.get(taskId);
  while (row && !trail.includes(row)) {
    trail.unshift(row);
    row = row.Parent ? byId.get(row.Parent) : undefined;
  }
  return trail;
}

export type TaskProgress = { done: number; running: number; queued: number; failed: number; total: number; tokens: number };

const FAILED: ReadonlySet<TaskKind> = new Set(['incomplete', 'stopped', 'interrupted']);

/** The strip's four groups. A task waiting on a person counts as queued: it has not finished. */
export function taskProgress(rows: EngineTaskRow[]): TaskProgress {
  const all = flatten(buildTaskTree(rows));
  const kinds = all.map(rowKind);
  const count = (test: (kind: TaskKind) => boolean) => kinds.filter(test).length;
  const done = count((kind) => kind === 'done');
  const running = count((kind) => kind === 'running');
  const failed = count((kind) => FAILED.has(kind));
  const tokens = all.reduce((sum, row) => sum + (row.Tokens ?? 0), 0);
  return { done, running, failed, queued: all.length - done - running - failed, total: all.length, tokens };
}

/** Titles of the tasks a row still waits on; finished or unknown ones are left out. */
export function waitTitles(rows: EngineTaskRow[], row: EngineTaskRow): string[] {
  const byId = new Map(visibleRows(rows).map((candidate) => [candidate.ID, candidate]));
  return (row.Waits ?? [])
    .map((id) => byId.get(id))
    .filter((dep): dep is EngineTaskRow => Boolean(dep) && rowKind(dep as EngineTaskRow) !== 'done')
    .map((dep) => dep.Title);
}
