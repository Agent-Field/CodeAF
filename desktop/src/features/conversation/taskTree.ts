import type { EngineTaskRow } from '../chat/engine-client';

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

/** Archived rows belong to a previous run and never show. A row whose parent is
 * missing becomes a root; a corrupt cycle is surfaced as roots, never dropped. */
export function buildTaskTree(rows: EngineTaskRow[]): TaskNode[] {
  const live = rows.filter((row) => !row.Archived);
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
  return roots;
}

function leaves(nodes: TaskNode[]): EngineTaskRow[] {
  return nodes.flatMap((node) => (node.children.length ? leaves(node.children) : [node.row]));
}

/** Leaves only, so a parent never counts twice. */
export function taskCounts(rows: EngineTaskRow[]): { done: number; total: number } {
  const all = leaves(buildTaskTree(rows));
  return { done: all.filter((row) => row.Status === 'done').length, total: all.length };
}
