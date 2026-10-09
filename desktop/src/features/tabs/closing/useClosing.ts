// Closing running work (design 3l). Closing detaches the view and keeps the engine work going; stopping is a
// separate, explicit act (the stop square, "Close and stop", or "Stop it" in the toast). Nothing here stops work implicitly.
import { useRef, useState, type Dispatch } from 'react';
import { panesOf, type Tab, type TabGroup, type WorkspaceAction, type WorkspaceState } from '../model';
import type { ClosingToastModel } from './ClosingToast';
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
  const [toast, setToast] = useState<ClosingToastModel | null>(null);
  const toastId = useRef(0);
  // Closed tabs whose stop was asked for leave the Inbox at once; the list would otherwise show them until the next read.
  const [stopping, setStopping] = useState<ReadonlySet<string>>(new Set());
  const mark = (id: string, on: boolean) => setStopping(current => { const next = new Set(current); if (on) next.add(id); else next.delete(id); return next; });

  const isRunning = (tab: Tab) => tabRunning(tab, latest.current.summaries);

  async function stop(tab: Tab) {
    mark(tab.id, true);
    const ok = await stopPanes(panesOf(tab).filter(pane => paneRunning(latest.current.summaries, pane.id)));
    if (!ok) mark(tab.id, false);
    if (!ok) setToast({ id: ++toastId.current, tabId: tab.id, title: tab.title, kind: 'stop-failed' });
  }

  function close(id: string, stopWork: boolean) {
    const { state, dispatch } = latest.current;
    const tab = state.tabs.find(t => t.id === id);
    if (!tab || tab.kind === 'inbox') return;
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
    else setToast({ id: ++toastId.current, tabId: id, title: tab.title, kind: 'closed' });
  }

  /** Puts a closed tab back where it was and makes it active. */
  function reopenClosed(id: string) {
    const place = places.current.get(id);
    latest.current.dispatch({ type: 'reopen-id', id, before: place?.before, after: place?.after, group: place?.group });
  }

  const closedTab = (id: string) => latest.current.state.closed.find(t => t.id === id);
  return {
    toast,
    stopping,
    dismissToast: () => setToast(null),
    closeTab: (id: string) => close(id, false),
    closeAndStop: (id: string) => close(id, true),
    isRunning,
    reopenClosed,
    /** "Stop it" (or "Try again") on the toast. */
    stopFromToast() {
      const tab = toast && closedTab(toast.tabId);
      setToast(null);
      if (tab) void stop(tab);
    },
    undoFromToast() {
      if (toast) reopenClosed(toast.tabId);
      setToast(null);
    },
    /** When this window first saw the tab close, for the Inbox's age column. Unknown after a reload. */
    sinceOf: (id: string) => places.current.get(id)?.since,
  };
}
