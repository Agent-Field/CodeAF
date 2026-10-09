import type { EngineTaskPage, EngineTaskRow } from '../chat/engine-client';
import type { ToolStep, Turn, TurnItem } from './types';

type TaskItem = Extract<TurnItem, { kind: 'task' }>;
type PageStep = NonNullable<EngineTaskPage['Steps']>[number];

export type TaskPageModel = {
  turn: Turn;
  children: TaskItem[];
  checks: string[];
  notes: string[];
  userFormat: 'literal' | 'markdown';
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

function stepOf(step: PageStep, index: number): ToolStep {
  const command = step.command ?? '';
  return {
    id: `step-${step.step ?? index}`,
    tool: 'bash',
    hint: command,
    args: command,
    output: step.observation ?? '',
    state: 'done',
  };
}

function liveStepOf(live: NonNullable<EngineTaskPage['Live']>, index: number): ToolStep {
  const command = live.Command ?? '';
  return { id: `step-live-${live.Step ?? index}`, tool: 'bash', hint: command, args: command, output: '', state: 'running' };
}

function stepsOf(page: EngineTaskPage): ToolStep[] {
  const done = (page.Steps ?? []).map(stepOf);
  const live = page.Live?.Command ? [liveStepOf(page.Live, done.length)] : [];
  return [...done, ...live];
}

function childOf(row: EngineTaskRow): TaskItem {
  return { kind: 'task', id: `child-${row.ID}`, taskId: row.ID, title: row.Title, status: row.Status, summary: '', body: '' };
}

function itemsOf(page: EngineTaskPage): TurnItem[] {
  const steps = stepsOf(page);
  const items: TurnItem[] = [];
  if (steps.length) items.push({ kind: 'tools', id: 'steps', steps });
  if (page.Result) items.push({ kind: 'text', id: 'result', text: page.Result, streaming: false });
  return items;
}

export function taskPageModel(page: EngineTaskPage): TaskPageModel {
  const turn: Turn = {
    id: `task:${page.Row.ID}`,
    user: page.Description ?? '',
    items: itemsOf(page),
    state: turnStateOf(page.Row),
    digest: '',
  };
  return {
    turn,
    children: (page.Children ?? []).map(childOf),
    checks: [...(page.Checks ?? [])],
    notes: (page.Notes ?? []).map((note) => note.Body).filter(Boolean),
    userFormat: 'markdown',
  };
}
