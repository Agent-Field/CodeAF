import type { MenuEntry } from '../../../components/ui';
import type { EngineTaskRow } from '../../chat/engine-client';
import { rowKind, taskControls } from '../taskState';

/** What the tree can ask of the rest of the app; a missing handler leaves its entry out. */
export type TaskCommands = {
  onOpenTask: (taskId: string, background: boolean) => void;
  onPause?: (taskId: string) => void;
  onResume?: (taskId: string) => void;
  onStop?: (taskId: string) => void;
};

type Action = Extract<MenuEntry, { kind?: 'action' }>;

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
