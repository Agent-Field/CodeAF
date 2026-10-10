// Finished-job rows for the tab's right-click (Components, "Finished job · tab menu", C-EDGE-6).
// The header's More menu and this menu are built by the same functions from the handlers the pane
// passes to the header, so a later edit cannot add Run again to one and leave it off the other.
import type { MenuEntry } from '../../components/ui/index.ts';
import type { TabsApi } from '../tabs/context.ts';
import { panesOf } from '../tabs/model.ts';
import type { Pane } from '../tabs/types.ts';

/** The actions a finished job can take. Absent means that action cannot work, so the row is left off. */
export type FinishedJobHandlers = {
  /** Present only when a retained log file can be opened. Scrollback in the tab is not that file. */
  onOpenLog?: () => void;
  /** Present only when the same command can be started again. */
  onRerun?: () => void;
  onRemove: () => void;
};

/** What the pane last told the tab menu. `finished` false yields no rows, including while a job is running. */
export type FinishedJobOffer = FinishedJobHandlers & { finished: boolean };

const readers = new Map<string, () => FinishedJobOffer>();

/**
 * The pane publishes the handlers it is about to give the header. The reader is kept after the pane
 * unmounts, so a job you already opened still offers Run again from the strip. A job that finishes
 * while its tab is not mounted is not offered until the pane reads it.
 */
export function offerTerminalTabMenu(paneId: string, read: () => FinishedJobOffer): void {
  readers.set(paneId, read);
}

/** Drops a published reader. Tests use it so one case cannot leak into the next. */
export function clearTerminalTabMenu(paneId: string): void {
  readers.delete(paneId);
}

/** Open log, Run again, then Remove job. Remove job is the danger row; the host places it under Close tab. */
export function finishedJobEntries(handlers: FinishedJobHandlers): MenuEntry[] {
  const rows: MenuEntry[] = [];
  if (handlers.onOpenLog) rows.push({ id: 'log', label: 'Open log', icon: 'scrollText', onSelect: handlers.onOpenLog });
  if (handlers.onRerun) rows.push({ id: 'rerun', label: 'Run again', icon: 'reload', onSelect: handlers.onRerun });
  rows.push({ id: 'remove', label: 'Remove job', danger: true, onSelect: handlers.onRemove });
  return rows;
}

/**
 * The header's finished-job menu: the shared rows, with Close tab between Run again and Remove job.
 * `closeShortcut` is the platform chord the header already shows, so this does not invent a second one.
 */
export function finishedHeaderMenu(handlers: FinishedJobHandlers & { onClose: () => void }, closeShortcut: string): MenuEntry[] {
  const extra = finishedJobEntries(handlers);
  const before = extra.filter(entry => entry.kind === 'separator' || entry.kind === 'submenu' || entry.id !== 'remove');
  const remove = extra.filter(entry => entry.kind !== 'separator' && entry.kind !== 'submenu' && entry.id === 'remove');
  return [
    ...before,
    { kind: 'separator', id: 'sep' },
    { id: 'close', label: 'Close tab', shortcut: closeShortcut, onSelect: handlers.onClose },
    ...remove,
  ];
}

const isOpen = (api: TabsApi, paneId: string) => api.state.tabs.some(tab => panesOf(tab).some(pane => pane.id === paneId));

/**
 * Kind slot for a terminal tab. A finished job contributes Open log (when a log can be opened), Run again,
 * and Remove job. A running job, a shell, and a pane that has not reported yet contribute nothing.
 */
export function terminalTabMenuItems(pane: Pane, api: TabsApi): MenuEntry[] {
  // A pane the strip no longer holds must not keep acting: its handlers would close a tab that is gone.
  if (!isOpen(api, pane.id)) return [];
  const offer = readers.get(pane.id)?.();
  if (!offer?.finished) return [];
  return finishedJobEntries(offer);
}

const isRemove = (entry: MenuEntry) => entry.kind !== 'separator' && entry.kind !== 'submenu' && entry.id === 'remove';

/**
 * Open log and Run again sit above the close separator (the design draws that line between Run again and
 * Close tab). Remove job is the next row under Close tab, and it is the danger row.
 */
export function placeFinishedJobItems(menu: readonly MenuEntry[], extra: readonly MenuEntry[]): MenuEntry[] {
  if (!extra.length) return [...menu];
  const closeAt = menu.findIndex(entry => entry.kind !== 'separator' && entry.kind !== 'submenu' && entry.id === 'close');
  const before = extra.filter(entry => !isRemove(entry));
  const after = extra.filter(isRemove);
  if (closeAt < 0) return [...before, ...menu, ...after];
  const separatorAt = closeAt > 0 && menu[closeAt - 1].kind === 'separator' ? closeAt - 1 : closeAt;
  return [...menu.slice(0, separatorAt), ...before, ...menu.slice(separatorAt, closeAt + 1), ...after, ...menu.slice(closeAt + 1)];
}
