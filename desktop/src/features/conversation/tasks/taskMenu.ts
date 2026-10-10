import type { MenuEntry } from '../../../components/ui';
import type { EngineTaskRow } from '../../chat/engine-client';
import { rowKind, taskControls } from '../taskState.ts';

/** What the tree can ask of the rest of the app; a missing handler leaves its entry out. */
export type TaskCommands = {
  onOpenTask: (taskId: string, background: boolean) => void;
  onPause?: (taskId: string) => void;
  onResume?: (taskId: string) => void;
  onStop?: (taskId: string) => void;
};

type Action = Extract<MenuEntry, { kind?: 'action' }>;

/** The newest plan row for an id. Rows arrive oldest first, so a later copy is the live one. */
function newestRow(tasks: readonly EngineTaskRow[], taskId: string): EngineTaskRow | undefined {
  for (let index = tasks.length - 1; index >= 0; index -= 1) {
    if (tasks[index].ID === taskId) return tasks[index];
  }
  return undefined;
}

/**
 * The notice's menu is the row's menu. Until the plan row arrives, only "Open in new tab"
 * is offered: Pause, Resume and Stop are read off that row, and a missing row is not
 * treated as a root that can be stopped.
 */
export function noticeActions(taskId: string, tasks: readonly EngineTaskRow[], commands: TaskCommands): Action[] {
  const row = newestRow(tasks, taskId);
  if (!row) return [{ id: 'open-tab', label: 'Open in new tab', icon: 'arrowUpRight', onSelect: () => commands.onOpenTask(taskId, true) }];
  return taskActions(row, commands);
}

/** One list feeds both the hover buttons and the context menu, so they never disagree. */
export function taskActions(row: EngineTaskRow, commands: TaskCommands): Action[] {
  const controls = taskControls(rowKind(row), Boolean(row.Parent));
  const id = row.ID;
  const entries: (Action | false)[] = [
    { id: 'open-tab', label: 'Open in new tab', icon: 'arrowUpRight', onSelect: () => commands.onOpenTask(id, true) },
    controls.pause && Boolean(commands.onPause) && { id: 'pause', label: 'Pause', icon: 'pause', onSelect: () => commands.onPause?.(id) },
    controls.resume && Boolean(commands.onResume) && { id: 'resume', label: 'Resume', icon: 'play', onSelect: () => commands.onResume?.(id) },
    controls.stop && Boolean(commands.onStop) && { id: 'stop', label: 'Stop', icon: 'stop', danger: true, onSelect: () => commands.onStop?.(id) },
  ];
  return entries.filter((entry): entry is Action => Boolean(entry));
}

/** The hover row shows Open and Pause or Resume (design, Row actions); every other command waits behind the ··· menu. */
const INLINE: ReadonlySet<string> = new Set(['open-tab', 'pause', 'resume']);

export function splitActions(actions: Action[]): { inline: Action[]; more: Action[] } {
  return { inline: actions.filter((action) => INLINE.has(action.id)), more: actions.filter((action) => !INLINE.has(action.id)) };
}
