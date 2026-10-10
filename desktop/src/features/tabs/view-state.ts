// Per-tab view state: which session the tab shows, where it is inside that
// session, and what the reader folded. Persisted with the tab and validated on
// read; an invalid field is dropped rather than discarding the whole tab.

export type TasksFilter = 'all' | 'needs' | 'running' | 'done';

export type TabRoute = { taskId?: string; back: string[]; forward: string[] };

/** What a file or diff tab shows: a workspace-relative path, and which view of it (Changes or File). */
export type FileTarget = { path: string; view?: 'changes' | 'file' };

export type TabView = {
  /** The file a file or diff tab shows. It reads through the session in `sessionFile`. */
  file?: FileTarget;
  sessionFile?: string;
  /** A file tab's workspace-relative path (opened from the new-tab field). */
  path?: string;
  route?: TabRoute;
  folded?: Record<string, boolean>;
  open?: Record<string, boolean>;
  tasksClosed?: boolean;
  /** The row chosen in the expanded tasks view. */
  tasksSelected?: string;
  /** The expanded Tasks filter, retained with this conversation tab. */
  tasksFilter?: TasksFilter;
  /** What a file, diff, terminal or web tab points at; set by that kind's lane and read by its hover preview. */
  target?: PaneTarget;
  /** A Home tab's place: a design-graph place id (`pl_…`) or `root` for All places. Never a filesystem folder. */
  place?: string;
  /** A web tab's address. Only http and https survive validation; anything else drops this field, never the tab. */
  web?: { url: string };
  /** The job a job tab follows. */
  job?: { jobId: string };
};

/** The design-graph place ids a Home tab may show: `root` (All places) or a placegraph id. */
export const isHomePlace = (value: unknown): value is string => typeof value === 'string' && (value === 'root' || /^pl_[0-9a-f]{16}$/.test(value));

/**
 * Where a file, diff, terminal or web tab points. Every field is optional and validated on read; the preview
 * reads it and a kind lane writes it. `sessionId` falls back to the session the tab's summary came from.
 */
export type PaneTarget = {
  sessionId?: string;
  /** Workspace-relative path of a file or diff tab. */
  path?: string;
  /** The terminal or job id of a terminal tab. */
  terminalId?: string;
  /** A web tab's address, and a screenshot of it when the browser surface has one. */
  url?: string;
  shot?: string;
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

const TARGET_FIELDS = ['sessionId', 'path', 'terminalId', 'url', 'shot'] as const;

/** Keeps the string fields of a target; anything else is dropped, and an empty result is no target. */
function cleanTarget(value: unknown): PaneTarget | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const source = value as Record<string, unknown>;
  const target: PaneTarget = {};
  for (const field of TARGET_FIELDS) if (typeof source[field] === 'string' && source[field]) target[field] = source[field] as string;
  return Object.keys(target).length ? target : undefined;
}

function isFileTarget(value: unknown): value is FileTarget {
  if (!value || typeof value !== 'object') return false;
  const file = value as Partial<FileTarget>;
  return typeof file.path === 'string' && file.path !== '' && (file.view === undefined || file.view === 'changes' || file.view === 'file');
}

/** A web address the browser surface may load: http or https only, so a saved `javascript:` url is never navigated to. */
function cleanWeb(value: unknown): { url: string } | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const url = (value as { url?: unknown }).url;
  if (typeof url !== 'string') return undefined;
  try {
    const { protocol } = new URL(url);
    return protocol === 'http:' || protocol === 'https:' ? { url } : undefined;
  } catch {
    return undefined;
  }
}

function cleanJob(value: unknown): { jobId: string } | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const jobId = (value as { jobId?: unknown }).jobId;
  return typeof jobId === 'string' && jobId ? { jobId } : undefined;
}

/** Keeps only the view fields that validate. */
export function cleanView(value: Record<string, unknown>): TabView {
  const view: TabView = {};
  const target = cleanTarget(value.target);
  if (target) view.target = target;
  if (typeof value.sessionFile === 'string' && value.sessionFile) view.sessionFile = value.sessionFile;
  if (typeof value.path === 'string' && value.path) view.path = value.path;
  if (isRoute(value.route)) view.route = value.route;
  if (isFileTarget(value.file)) view.file = { path: value.file.path, ...(value.file.view ? { view: value.file.view } : {}) };
  if (isFlagMap(value.folded)) view.folded = value.folded;
  if (isFlagMap(value.open)) view.open = value.open;
  if (typeof value.tasksClosed === 'boolean') view.tasksClosed = value.tasksClosed;
  if (typeof value.tasksSelected === 'string' && value.tasksSelected) view.tasksSelected = value.tasksSelected;
  if (['all', 'needs', 'running', 'done'].includes(value.tasksFilter as string)) view.tasksFilter = value.tasksFilter as TasksFilter;
  if (isHomePlace(value.place)) view.place = value.place;
  const web = cleanWeb(value.web);
  if (web) view.web = web;
  const job = cleanJob(value.job);
  if (job) view.job = job;
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
