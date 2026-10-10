// Closing running work (design 3l). Closing detaches the view and keeps the engine work going; stopping is a
// separate, explicit act (the stop square, "Close and stop", or "Stop it" in the toast). Nothing here stops work implicitly.
import { useRef, useState, type Dispatch } from 'react';
import { toasts } from '../../../design/toasts';
import { tabCloseToastKey } from '../undo/structuralUndo';
import { panesOf, workspaceReducer, type Tab, type TabGroup, type WorkspaceAction, type WorkspaceState } from '../model';
import { paneRunning, tabRunning, type Summaries } from './running';
import { stopPanes } from './stopWork';

type Place = { before?: string; after?: string; group?: TabGroup; since: number };

type Options = { state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>; summaries: Summaries };

/** Where the strip's focus goes once a focused tab is gone: the tab that became active. */
const restoreStripFocus = () => requestAnimationFrame(() => document.querySelector<HTMLElement>('.workspace-tabstrip [aria-selected="true"]')?.focus());

export function useClosing(options: Options) {
  // The key handlers hold the functions from the render they were registered in, so these read the latest state.
  const latest = useRef(options);
  latest.current = options;
  const places = useRef(new Map<string, Place>());
  // Closed tabs whose stop was asked for leave the Inbox at once; the list would otherwise show them until the next read.
  const [stopping, setStopping] = useState<ReadonlySet<string>>(new Set());
  const mark = (id: string, on: boolean) => setStopping(current => { const next = new Set(current); if (on) next.add(id); else next.delete(id); return next; });

  const isRunning = (tab: Tab) => tabRunning(tab, latest.current.summaries);

  /**
   * Asks the engine to stop a closed tab's work. A failure is NEVER swallowed: the work is still running, so the tab
   * goes back on the Inbox's running list at once and a toast says so, with Try again and Undo (which reopens the tab).
   */
  async function stop(tab: Tab) {
    mark(tab.id, true);
    const ok = await stopPanes(panesOf(tab).filter(pane => paneRunning(latest.current.summaries, pane.id)));
    if (ok) return;
    mark(tab.id, false);
    toasts.show({
      key: tabCloseToastKey,
      message: ['Could not stop ', { strong: tab.title }, '. It is still running.'], tone: 'danger',
      actions: [{ label: 'Try again', onSelect: () => void stop(tab) }],
      undo: () => reopenClosed(tab.id),
    });
  }

  function close(id: string, stopWork: boolean) {
    const { state, dispatch } = latest.current;
    const tab = state.tabs.find(t => t.id === id);
    if (!tab) return;
    const restore = !!document.activeElement?.closest('.workspace-tab');
    const group = state.groups.find(g => g.id === tab.groupId);
    const alone = !!group && state.tabs.filter(t => t.groupId === group.id).length === 1;
    const index = state.tabs.indexOf(tab);
    places.current.set(id, { before: state.tabs[index + 1]?.id, after: state.tabs[index - 1]?.id, group: alone ? group : undefined, since: Date.now() });
    const running = isRunning(tab);
    dispatch({ type: 'close', id });
    if (restore) restoreStripFocus();
    if (!running) return;
    if (stopWork) void stop(tab);
    else toasts.show({ key: tabCloseToastKey, message: [{ strong: tab.title }, ' closed and still running'], actions: [{ label: 'Stop it', onSelect: () => void stop(tab) }], undo: () => reopenClosed(id) });
  }

  /**
   * Closes several tabs at once (the rest of the strip, the tabs to the right, a whole group) behind one structural Undo.
   * The restore point is taken BEFORE the close from the tabs themselves, so every pane of every split, the group's title
   * and collapsed state, and each tab's place come back exactly; it does not lean on the bounded Reopen list.
   * Work still running in the closed tabs goes on (closing detaches); the message says so.
   */
  function closeMany(action: Extract<WorkspaceAction, { type: 'close-others' | 'close-right' | 'close-group' }>) {
    const { state, dispatch } = latest.current;
    const remaining = new Set(workspaceReducer(state, action).tabs.map(t => t.id));
    const gone = state.tabs.filter(t => !remaining.has(t.id));
    if (!gone.length) return;
    const restore = !!document.activeElement?.closest('.workspace-tab');
    const point = { tabs: gone, order: state.tabs.map(t => t.id), groups: state.groups.map(g => ({ ...g })), activeId: state.activeId };
    const alive = gone.filter(isRunning).length;
    const now = Date.now();
    for (const tab of gone) places.current.set(tab.id, { since: now });
    dispatch(action);
    if (restore) restoreStripFocus();
    const count = gone.length === 1 ? '1 tab' : `${gone.length} tabs`;
    toasts.show({
      key: tabCloseToastKey,
      message: ['Closed ', { strong: count }, ...(alive ? [`, ${alive} still running`] : [])],
      undo: () => dispatch({ type: 'restore-closed', ...point }),
    });
  }

  /** Puts a closed tab back where it was and makes it active. */
  function reopenClosed(id: string) {
    const place = places.current.get(id);
    latest.current.dispatch({ type: 'reopen-id', id, before: place?.before, after: place?.after, group: place?.group });
  }

  return {
    stopping,
    closeTab: (id: string) => close(id, false),
    closeAndStop: (id: string) => close(id, true),
    closeMany,
    isRunning,
    reopenClosed,
    /** When this window first saw the tab close, for the Inbox's age column. Unknown after a reload. */
    sinceOf: (id: string) => places.current.get(id)?.since,
  };
}
