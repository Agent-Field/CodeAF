import type { EngineTaskPage, EngineTaskRow } from '../chat/engine-client';
import type { ToolStep, Turn, TurnItem } from './types';

type TaskItem = Extract<TurnItem, { kind: 'task' }>;
type PageStep = NonNullable<EngineTaskPage['Steps']>[number];

export type TaskPageModel = {
  title: string;
  status: string;
  stopped: boolean;
  /** One muted fact beside the state word: step count while running, duration once finished. */
  detail: string;
  /** The recorded steps as one tools item; absent when there are none. */
  worked?: Extract<TurnItem, { kind: 'tools' }>;
  result: string;
  /** The engine's worker brief, kept for the quiet "Instructions" disclosure. */
  instructions: string;
  children: TaskItem[];
  checks: string[];
  notes: string[];
};

const RUNNING = new Set(['running', 'claimed']);

export function turnStateOf(row: EngineTaskPage['Row']): Turn['state'] {
  if (row.Stopped || row.Interrupted || row.Status === 'cancelled') return 'stopped';
  if (row.Status === 'failed') return 'failed';
  return row.Status === 'done' ? 'done' : 'working';
}

/** True while the engine is still changing the page, so it is worth refreshing. */
export function isTaskRunning(page: EngineTaskPage | undefined): boolean {
  return page ? RUNNING.has(page.Row.Status) && turnStateOf(page.Row) === 'working' : false;
}

// Every recorded worker step is a shell command (session.PlanStep's `kind` is
// always "step"), so the tool family is the terminal one.
const STEP_TOOL = 'bash';

function stepOf(step: PageStep, index: number): ToolStep {
  const command = step.command ?? '';
  return {
    id: `step-${step.step ?? index}`,
    tool: STEP_TOOL,
    hint: command,
    args: command,
    output: step.observation ?? '',
    state: step.refused ? 'failed' : 'done',
  };
}

/** A not-run step without a refusal is the harness correcting a reply's form: no step a person reads. */
const isReadable = (step: PageStep) => !step.not_run || Boolean(step.refused);

function liveStepOf(live: NonNullable<EngineTaskPage['Live']>, index: number): ToolStep {
  const command = live.Command ?? '';
  return { id: `step-live-${live.Step ?? index}`, tool: STEP_TOOL, hint: command, args: command, output: '', state: 'running' };
}

function stepsOf(page: EngineTaskPage): ToolStep[] {
  const done = (page.Steps ?? []).filter(isReadable).map(stepOf);
  const live = page.Live?.Command ? [liveStepOf(page.Live, done.length)] : [];
  return [...done, ...live];
}

function childOf(row: EngineTaskRow): TaskItem {
  return { kind: 'task', id: `child-${row.ID}`, taskId: row.ID, title: row.Title, status: row.Status, summary: '', body: '' };
}

const SECOND = 1000;

/** "12s", "3m 4s", "1h 5m"; empty when either end is missing or unreadable. */
export function durationText(started?: string, ended?: string): string {
  const from = Date.parse(started ?? '');
  const to = Date.parse(ended ?? '');
  if (!Number.isFinite(from) || !Number.isFinite(to) || to < from) return '';
  const seconds = Math.round((to - from) / SECOND);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

function detailOf(page: EngineTaskPage, steps: ToolStep[]): string {
  if (turnStateOf(page.Row) === 'working') {
    const count = steps.length;
    return count ? `${count} ${count === 1 ? 'step' : 'steps'}` : '';
  }
  return durationText(page.Row.Started, page.Row.Ended);
}

/** A result belongs to a finished task; on an unfinished one it is left over from an earlier run. */
function resultOf(page: EngineTaskPage): string {
  return turnStateOf(page.Row) === 'working' ? '' : (page.Result ?? '');
}

/** The engine also records the result as a note; drawing both says everything twice. */
function notesOf(page: EngineTaskPage): string[] {
  const result = (page.Result ?? '').trim();
  const bodies = (page.Notes ?? []).map((note) => note.Body).filter(Boolean);
  return bodies.filter((body) => !result || !body.includes(result));
}

export function taskPageModel(page: EngineTaskPage): TaskPageModel {
  const steps = stepsOf(page);
  return {
    title: page.Row.Title,
    status: page.Row.Status,
    stopped: Boolean(page.Row.Stopped || page.Row.Interrupted),
    detail: detailOf(page, steps),
    worked: steps.length ? { kind: 'tools', id: `steps:${page.Row.ID}`, steps } : undefined,
    result: resultOf(page),
    instructions: page.Description ?? '',
    children: (page.Children ?? []).map(childOf),
    checks: [...(page.Checks ?? [])],
    notes: notesOf(page),
  };
}
