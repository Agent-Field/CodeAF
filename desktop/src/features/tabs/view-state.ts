// Per-tab view state: which session the tab shows, where it is inside that
// session, and what the reader folded. Persisted with the tab and validated on
// read; an invalid field is dropped rather than discarding the whole tab.

export type TabRoute = { taskId?: string; back: string[]; forward: string[] };

/** What a file or diff tab shows: a workspace-relative path, and which view of it (Changes or File). */
export type FileTarget = { path: string; view?: 'changes' | 'file' };

export type TabView = {
  /** The file a file or diff tab shows. It reads through the session in `sessionFile`. */
  file?: FileTarget;
  sessionFile?: string;
  route?: TabRoute;
  folded?: Record<string, boolean>;
  open?: Record<string, boolean>;
  tasksClosed?: boolean;
  /** The row chosen in the expanded tasks view. */
  tasksSelected?: string;
};

export const rootRoute: TabRoute = { back: [], forward: [] };

/**
 * The expanded tasks view is a stop on the tab's route like a task is, so Back,
 * Forward and reload treat it the same way. No canonical task id starts with '#'.
 */
export const TASKS_VIEW = '#tasks';

/** The task the route shows, or undefined for the conversation and the expanded view. */
export function routeTask(route: TabRoute): string | undefined {
  return route.taskId === TASKS_VIEW ? undefined : route.taskId;
}

const isStringList = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((item) => typeof item === 'string');

function isFlagMap(value: unknown): value is Record<string, boolean> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  return Object.values(value).every((flag) => typeof flag === 'boolean');
}

function isRoute(value: unknown): value is TabRoute {
  if (!value || typeof value !== 'object') return false;
  const route = value as Partial<TabRoute>;
  const taskOk = route.taskId === undefined || (typeof route.taskId === 'string' && route.taskId !== '');
  return taskOk && isStringList(route.back) && isStringList(route.forward);
}

function isFileTarget(value: unknown): value is FileTarget {
  if (!value || typeof value !== 'object') return false;
  const file = value as Partial<FileTarget>;
  return typeof file.path === 'string' && file.path !== '' && (file.view === undefined || file.view === 'changes' || file.view === 'file');
}

/** Keeps only the view fields that validate. */
export function cleanView(value: Record<string, unknown>): TabView {
  const view: TabView = {};
  if (typeof value.sessionFile === 'string' && value.sessionFile) view.sessionFile = value.sessionFile;
  if (isRoute(value.route)) view.route = value.route;
  if (isFileTarget(value.file)) view.file = { path: value.file.path, ...(value.file.view ? { view: value.file.view } : {}) };
  if (isFlagMap(value.folded)) view.folded = value.folded;
  if (isFlagMap(value.open)) view.open = value.open;
  if (typeof value.tasksClosed === 'boolean') view.tasksClosed = value.tasksClosed;
  if (typeof value.tasksSelected === 'string' && value.tasksSelected) view.tasksSelected = value.tasksSelected;
  return view;
}

/** Back and forward stacks hold task ids; '' is the conversation itself. */
export function navigate(route: TabRoute, taskId?: string): TabRoute {
  if ((route.taskId ?? '') === (taskId ?? '')) return route;
  return { taskId, back: [...route.back, route.taskId ?? ''], forward: [] };
}

export function goBack(route: TabRoute): TabRoute {
  if (!route.back.length) return route;
  const target = route.back[route.back.length - 1];
  return { taskId: target || undefined, back: route.back.slice(0, -1), forward: [route.taskId ?? '', ...route.forward] };
}

export function goForward(route: TabRoute): TabRoute {
  if (!route.forward.length) return route;
  const [target, ...rest] = route.forward;
  return { taskId: target || undefined, back: [...route.back, route.taskId ?? ''], forward: rest };
}

/** Flips one flag; only true flags are kept so the persisted map stays small. */
export function toggleFlag(flags: Record<string, boolean> | undefined, id: string): Record<string, boolean> {
  const next = { ...flags };
  if (next[id]) delete next[id];
  else next[id] = true;
  return next;
}
