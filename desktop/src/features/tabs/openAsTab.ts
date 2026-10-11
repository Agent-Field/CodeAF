// One seam for "open this view as a tab or a split pane".
// When the pane is already drilled into that task, the new view takes the drill and the
// source pops back to the route underneath. Opening any other task leaves the source.
import { routeTask, TASKS_VIEW, type TabRoute } from './view-state.ts';
import type { Pane } from './types.ts';

const CONVERSATION = '';

/**
 * A task tab sitting on its own task, with only the conversation under it, is not a drill.
 * A child drill pushes that task's id onto back, which is the view a hand-over pops.
 */
function ownTaskRoot(pane: Pick<Pane, 'kind' | 'route'>, route: TabRoute): boolean {
  return pane.kind === 'task' && route.back.length > 0 && route.back.every(id => id === CONVERSATION);
}

/**
 * The task this pane is drilled into, when there is a route underneath to return to.
 * Absent for the conversation, the tasks table, a task tab that already is this task,
 * and a route with nothing under it.
 */
export function drilledTaskId(pane: Pick<Pane, 'kind' | 'route'> | undefined): string | undefined {
  const route = pane?.route;
  const taskId = route ? routeTask(route) : undefined;
  if (!pane || !route || !taskId || route.back.length === 0 || ownTaskRoot(pane, route)) return undefined;
  return taskId;
}

/** Back and forward the new tab may keep: every stop except the conversation itself. */
export function routeBelowConversation(route: TabRoute): TabRoute {
  return { taskId: route.taskId, back: route.back.filter(id => id !== CONVERSATION), forward: route.forward.filter(id => id !== CONVERSATION) };
}

/**
 * The route this pane had before the current drill. Forward is cleared: those steps belonged
 * to the drill that is leaving, and the forward stack from before the drill was already discarded.
 */
export function routeBeforeDrill(route: TabRoute): TabRoute {
  if (!route.back.length) return { back: [], forward: [] };
  const target = route.back[route.back.length - 1];
  return { taskId: target || undefined, back: route.back.slice(0, -1), forward: [] };
}

/** The conversation root is stored as no route, so the pane draws the transcript and no breadcrumb. */
export function releaseRoute(route: TabRoute): TabRoute | undefined {
  if (!route.taskId && route.back.length === 0 && route.forward.length === 0) return undefined;
  return route;
}

export type PlacedView = {
  /** The route the new tab or pane shows. */
  route: TabRoute;
  /** Set when the source was showing this task and must pop back. */
  release?: TabRoute;
};

/**
 * Opening `taskId` from `pane`. A match on the current drill hands that route over
 * (history below the conversation) and tells the caller to pop the source. Anything else
 * opens a fresh task route and leaves the source alone.
 */
export function openAsTab(pane: Pick<Pane, 'kind' | 'route'> | undefined, taskId: string): PlacedView {
  const fresh: PlacedView = { route: { taskId, back: [CONVERSATION], forward: [] } };
  if (!taskId || taskId === TASKS_VIEW) return fresh;
  if (!pane?.route || drilledTaskId(pane) !== taskId) return fresh;
  return { route: routeBelowConversation(pane.route), release: routeBeforeDrill(pane.route) };
}

/**
 * Panes joining a split. A single pane drilled into a task pops back, and the drill
 * becomes the task pane after it, so the drop does not leave both sides on that task.
 * Any other guest joins unchanged.
 */
export function panesOpeningIntoSplit(panes: Pane[], id: () => string): Pane[] {
  if (panes.length !== 1) return panes;
  const pane = panes[0];
  const taskId = drilledTaskId(pane);
  if (!taskId) return panes;
  const placed = openAsTab(pane, taskId);
  if (!placed.release) return panes;
  const task: Pane = { id: id(), kind: 'task', title: '', draft: pane.draft, sessionFile: pane.sessionFile, route: placed.route };
  return [{ ...pane, route: releaseRoute(placed.release) }, task];
}
