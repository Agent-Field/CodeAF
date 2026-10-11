// The pure reading of one organising run. The wire is ARCHITECTURE §14
// (GET /places/organize, the placesOrganizing world record, one receipt).
// Person-facing sentences live here so the view cannot invent a second wording.

/** How the next run treats places that are already there. add-on is the default. */
export type OrganizeMode = 'add-on' | 'refresh';

export const ORGANIZE_MODES: { value: OrganizeMode; label: string }[] = [
  { value: 'add-on', label: "Add to what's there" },
  { value: 'refresh', label: 'Start fresh' },
];

/** One progress event. Done and total are that stage's own units. */
export type OrganizeProgress = {
  run: string;
  stage: string;
  done: number;
  total: number;
  cancellable: boolean;
};

/** What Settings can say about the previous run. An empty summary draws nothing. */
export type OrganizeLast = {
  summary: string;
  outcome: string;
};

/** One proposed change. why is the engine's sentence; name is only a fallback. */
export type OrganizeOp = {
  kind: string;
  why: string;
  name: string;
  proposed: boolean;
};

export type OrganizePlan = {
  id: string;
  summary: string;
  ops: OrganizeOp[];
  provisional: boolean;
};

export type OrganizeSnapshot = {
  running: boolean;
  progress?: OrganizeProgress;
  last?: OrganizeLast;
  plan?: OrganizePlan;
};

export type OrganizePlanView = {
  id: string;
  summary: string;
  rows: { index: number; line: string }[];
};

/** What the page draws. Empty strings are lines that are not drawn. */
export type OrganizeScreen = {
  unavailable: string;
  running: boolean;
  progress: string;
  cancel: boolean;
  last: string;
  plan: OrganizePlanView | null;
};

const READING = new Set(['dedup', 'kind', 'anchors', 'vectors']);
const GROUPING = new Set(['neighbours', 'communities', 'nest', 'audit', 'park']);
const NAMING = new Set(['name', 'review']);
const CHECKING = new Set(['link', 'check']);

const count = (value: number) => value.toLocaleString('en-US');

/**
 * The one progress sentence. Reading uses the stage total (the chat count).
 * Naming uses done of total. An unknown stage, or a zero total, omits the number
 * rather than drawing a count the engine did not give.
 */
export function progressLine(progress: OrganizeProgress | undefined): string {
  if (!progress?.stage) return '';
  if (READING.has(progress.stage)) return progress.total > 0 ? `Reading ${count(progress.total)} chats` : 'Reading';
  if (GROUPING.has(progress.stage)) return 'Grouping';
  if (NAMING.has(progress.stage)) return progress.total > 0 ? `Naming (${count(progress.done)} of ${count(progress.total)})` : 'Naming';
  if (CHECKING.has(progress.stage)) return 'Checking';
  return '';
}

/** The previous run's sentence. Cancelled with no summary is the one outcome that still has words. */
export function lastRunLine(last: OrganizeLast | undefined): string {
  if (!last) return '';
  const summary = last.summary.trim();
  if (summary) return summary;
  return last.outcome === 'cancelled' ? 'Cancelled' : '';
}

const named = (label: string, name: string) => {
  const trimmed = name.trim();
  return trimmed ? `${label} “${trimmed}”` : label;
};

// A line with no why. Ids are never interpolated: an id is not a name.
const OP_LINE: Record<string, (op: OrganizeOp) => string> = {
  birth: op => named('New place', op.name),
  rename: op => named('Rename to', op.name),
  name: op => named('Name', op.name),
  merge: () => 'Merge',
  dissolve: () => 'This place would go',
  file: () => 'File a chat',
  park: () => 'Leave a chat in a broader place',
  inbox: () => 'Leave a chat in Now',
  link: () => 'Also in another place',
  lens: op => named('View', op.name),
  split: () => 'Split a place',
  collapse: () => 'Fold a place into its parent',
  interpose: op => named('Group places as', op.name),
  demote: () => 'Turn a place into a view',
  constraint: () => 'A rule from your corrections',
  axis: () => 'Set how a place is arranged',
  exemplar: () => 'Remember a correction',
};

/** The row's words. The engine's why wins. A kind with no sentence is not shown. */
export function opLine(op: OrganizeOp): string {
  const why = op.why.trim();
  if (why) return why;
  return OP_LINE[op.kind]?.(op) ?? '';
}

export function planView(plan: OrganizePlan | undefined): OrganizePlanView | null {
  if (!plan) return null;
  const rows = plan.ops.flatMap((op, index) => {
    const line = opLine(op);
    return line ? [{ index, line }] : [];
  });
  return { id: plan.id, summary: plan.summary.trim(), rows };
}

/** Every op starts included, including one with no row, so a hidden op is not silently skipped. */
export function initialIncluded(ops: readonly OrganizeOp[]): Set<number> {
  return new Set(ops.map((_, index) => index));
}

/** Indexes the person unchecked, in plan order. Accept sends these as skip. */
export function skipOf(ops: readonly OrganizeOp[], included: ReadonlySet<number>): number[] {
  const skip: number[] = [];
  ops.forEach((_, index) => { if (!included.has(index)) skip.push(index); });
  return skip;
}

/**
 * While a run is going, the world-feed event is the line. A feed from a different
 * run is ignored so a finished run cannot rename the one that is going.
 */
export function liveProgress(running: boolean, snapshot?: OrganizeProgress, feed?: OrganizeProgress): OrganizeProgress | undefined {
  if (!running) return undefined;
  if (feed && (!snapshot?.run || feed.run === snapshot.run)) return feed;
  return snapshot;
}

export function organizeScreen(snapshot: OrganizeSnapshot | undefined, feed?: OrganizeProgress, unavailable = ''): OrganizeScreen {
  if (unavailable) return { unavailable, running: false, progress: '', cancel: false, last: '', plan: null };
  const running = snapshot?.running === true;
  const progress = liveProgress(running, snapshot?.progress, feed);
  return {
    unavailable: '',
    running,
    progress: progressLine(progress),
    // No progress object yet still offers Cancel: the run can be stopped from the first moment.
    cancel: running && progress?.cancellable !== false,
    last: lastRunLine(snapshot?.last),
    // A run that is still going has no review yet. The plan arrives when it finishes.
    plan: running ? null : planView(snapshot?.plan),
  };
}

const isObject = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const isInt = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;

function progressOf(value: unknown): OrganizeProgress | undefined {
  if (!isObject(value)) throw new Error('The engine returned an invalid organizing progress.');
  if (value.run === '') return undefined;
  if (typeof value.run !== 'string' || typeof value.stage !== 'string' || !isInt(value.done) || !isInt(value.total)) throw new Error('The engine returned an invalid organizing progress.');
  return { run: value.run, stage: value.stage, done: value.done, total: value.total, cancellable: value.cancellable !== false };
}

function lastOf(value: unknown): OrganizeLast | undefined {
  if (value == null) return undefined;
  if (!isObject(value)) throw new Error('The engine returned an invalid organizing summary.');
  return { summary: typeof value.summary === 'string' ? value.summary : '', outcome: typeof value.outcome === 'string' ? value.outcome : '' };
}

function opOf(value: unknown): OrganizeOp {
  if (!isObject(value) || typeof value.kind !== 'string' || !value.kind) throw new Error('The engine returned an invalid organizing plan.');
  return {
    kind: value.kind,
    why: typeof value.why === 'string' ? value.why : '',
    name: typeof value.name === 'string' ? value.name : '',
    proposed: value.proposed === true,
  };
}

function planOf(value: unknown): OrganizePlan | undefined {
  if (value == null) return undefined;
  if (!isObject(value) || typeof value.id !== 'string' || !value.id || !Array.isArray(value.ops)) throw new Error('The engine returned an invalid organizing plan.');
  return {
    id: value.id,
    summary: typeof value.summary === 'string' ? value.summary : '',
    ops: value.ops.map(opOf),
    provisional: value.provisional === true,
  };
}

/** GET /places/organize. A zero progress (empty run) is the same as no progress. */
export function parseOrganizeSnapshot(value: unknown): OrganizeSnapshot {
  if (!isObject(value) || typeof value.running !== 'boolean') throw new Error('The engine returned an invalid organizing status.');
  const progress = value.progress == null ? undefined : progressOf(value.progress);
  return { running: value.running, progress, last: lastOf(value.last), plan: planOf(value.plan) };
}

function pushId(ids: string[], value: unknown) {
  if (typeof value === 'string' && value) ids.push(value);
  else if (isObject(value) && typeof value.id === 'string' && value.id) ids.push(value.id);
}

/**
 * Accept answers with exactly one receipt. Several ids, or none, are a refusal:
 * Undo would otherwise take back the wrong change.
 */
export function oneReceiptId(value: unknown): string {
  if (!isObject(value)) throw new Error('The engine returned an invalid receipt.');
  const ids: string[] = [];
  pushId(ids, value.receipt);
  if (Array.isArray(value.receipts)) for (const row of value.receipts) pushId(ids, row);
  if (Array.isArray(value.undo)) for (const row of value.undo) pushId(ids, row);
  const unique = [...new Set(ids)];
  const id = unique[0];
  if (unique.length !== 1 || !id) throw new Error('The engine returned an invalid receipt.');
  return id;
}

export type OrganizeFixture = {
  snapshot?: OrganizeSnapshot;
  feed?: OrganizeProgress;
  unavailable?: string;
};

const progress = (stage: string, done: number, total: number, run = 'run-1'): OrganizeProgress => ({ run, stage, done, total, cancellable: true });

/** Fixture snapshots the specimen and the node tests share. Nothing here is live data. */
export const organizeFixtures: Record<string, OrganizeFixture> = {
  'idle-empty': { snapshot: { running: false } },
  'idle-summary': { snapshot: { running: false, last: { summary: '12 new places, 340 chats filed', outcome: 'offered' } } },
  'running-reading': { snapshot: { running: true, progress: progress('vectors', 100, 1240) } },
  'running-grouping': { snapshot: { running: true, progress: progress('communities', 0, 0) } },
  'running-naming': {
    snapshot: { running: true, progress: progress('vectors', 0, 10) },
    feed: progress('name', 12, 30),
  },
  'running-checking': { snapshot: { running: true, progress: progress('check', 1, 1) } },
  'running-quiet': { snapshot: { running: true }, feed: progress('later', 1, 2) },
  plan: {
    snapshot: {
      running: false,
      plan: {
        id: 'plan-1',
        summary: '3 places would go, 2 would be renamed, 410 chats would move',
        provisional: false,
        ops: [
          { kind: 'dissolve', why: 'This place would go', name: '', proposed: true },
          { kind: 'rename', why: '', name: 'Desk', proposed: true },
          { kind: 'file', why: '', name: '', proposed: false },
        ],
      },
    },
  },
  cancelled: { snapshot: { running: false, last: { summary: '', outcome: 'cancelled' } } },
  accepted: { snapshot: { running: false, last: { summary: '12 new places, 340 chats filed', outcome: 'applied' } } },
  unavailable: { unavailable: 'Organizing places is not available from this engine yet.' },
};

export function screenOf(fixture: OrganizeFixture): OrganizeScreen {
  return organizeScreen(fixture.snapshot, fixture.feed, fixture.unavailable ?? '');
}
