// What the History tab may do to the workspace, and the one thing History does on its own: the 12-hour
// archive. ⌘Y is the shell's shortcut registry (design/keyboard.ts), handled with the other tab keys. The workspace builds one host and provides it; the pane never reaches for globals.
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type Dispatch } from 'react';
import type { TabSummary } from '../conversation/tabSummary';
import { newTab } from '../tabs/helpers';
import { panesOf, tabHolding, type Tab, type WorkspaceAction, type WorkspaceState } from '../tabs/model';
import { archiveHistory } from './client';
import { historyIdOf, idleTabs, type TabActivity } from './model';
import type { HistoryItem } from './types';

export type HistoryHost = {
  /**
   * Continue: opens the conversation as a live tab. Plain replaces the History tab that asked, or becomes the new-tab field
   * that asked; `newTab` keeps the asker, and `background` (the new-tab field's ⌘-click) also leaves the person where they are.
   */
  continueConversation: (item: HistoryItem, paneId: string, press: { newTab: boolean; background?: boolean }) => void;
  /** Archive removes an open row from the strip; Unarchive clears its mark without focusing it. */
  archiveConversation: (item: HistoryItem) => Promise<void>;
};

export const HistoryHostContext = createContext<HistoryHost | null>(null);
export const useHistoryHost = () => useContext(HistoryHostContext);

/** The host over one workspace's state and reducer. */
export function useHistoryWorkspace(state: WorkspaceState, dispatch: Dispatch<WorkspaceAction>): HistoryHost {
  const latest = useRef(state);
  latest.current = state;
  const continueConversation = useCallback<HistoryHost['continueConversation']>((item, paneId, press) => {
    const current = latest.current;
    // Continuing an archived conversation brings it back out of the archive, as Restore all does.
    if (item.archived) void archiveHistory([item.id], false).catch(() => undefined);
    const held = current.tabs.flatMap(panesOf).find(pane => pane.sessionFile === item.sessionFile);
    if (held) { if (!press.background) dispatch({ type: 'select', id: held.id }); return; }
    const tab = newTab({ kind: 'conversation', title: item.title.trim() || undefined, ...(item.title.trim() ? { titleSource: 'engine' as const } : {}), sessionFile: item.sessionFile });
    const holder = tabHolding(current, paneId);
    const asker = current.tabs.flatMap(panesOf).find(pane => pane.id === paneId);
    // The new-tab field turns into the conversation wherever it sits (a split pane, a pinned tab), the way a file or a question does.
    if (!press.newTab && asker?.kind === 'newtab') dispatch({ type: 'newtab-become', id: paneId, kind: 'conversation', title: tab.title, titleSource: tab.titleSource, sessionFile: item.sessionFile });
    // A plain tab that is not pinned is replaced in place; a split pane, a pinned tab or a command-click opens a new tab.
    else if (!press.newTab && holder && holder.id === paneId && !holder.pinned) dispatch({ type: 'history-replace', id: holder.id, tab });
    else dispatch({ type: 'open', tab, background: !!press.background });
  }, [dispatch]);
  const archiveConversation = useCallback<HistoryHost['archiveConversation']>(async item => {
    // Archiving removes a plain tab from the strip; unarchiving only changes the saved mark.
    const holders = latest.current.tabs.filter(tab => !tab.split && tab.sessionFile === item.sessionFile).map(tab => tab.id);
    if (!item.archived && holders.length) dispatch({ type: 'history-archive', ids: holders });
    await archiveHistory([item.id], !item.archived);
  }, [dispatch]);
  return useMemo(() => ({ continueConversation, archiveConversation }), [continueConversation, archiveConversation]);
}

// ---- the 12-hour archive ----------------------------------------------------

export const activityKey = 'codeaf.desktop.activity.v1';
const ACTIVE_STAMP_MS = 60_000;
type Activity = Record<string, TabActivity>;

export function readActivity(): Activity {
  try {
    const saved = JSON.parse(localStorage.getItem(activityKey) ?? '{}') as Record<string, Partial<TabActivity>>;
    const clean: Activity = {};
    for (const [id, entry] of Object.entries(saved)) if (entry && Number.isFinite(entry.at) && typeof entry.hold === 'boolean') clean[id] = { at: entry.at as number, hold: entry.hold };
    return clean;
  } catch { return {}; }
}
function writeActivity(activity: Activity) {
  try { localStorage.setItem(activityKey, JSON.stringify(activity)); } catch { /* Storage may be full or unavailable; the clock then restarts, which only delays an archive. */ }
}

/** What a tab's last known state means for archiving: pinned, running and waiting-on-you tabs are never idle. */
const holds = (tab: Tab, summary?: TabSummary) => tab.pinned || summary?.mark === 'working' || summary?.mark === 'waiting';

export type Archived = { tabs: Tab[] };

/**
 * Remembers when each tab was last live and, once at launch, archives the ones idle for 12 hours (design 4d).
 * Nothing is deleted: an archived conversation stays in History. Returns what was archived, for the toast.
 */
export function useAutoArchive(state: WorkspaceState, dispatch: Dispatch<WorkspaceAction>, summaries: Readonly<Record<string, TabSummary>>, now: () => number = Date.now, ready = true): [Archived | undefined, () => void] {
  const [archived, setArchived] = useState<Archived>();
  const dispatched = useRef(false);
  const owned = useRef(new Set<string>());
  const store = useRef<Activity | undefined>(undefined);
  if (!store.current) store.current = readActivity();

  // At launch: judge the clock the last run left behind, before this run touches it.
  useEffect(() => {
    if (!ready || dispatched.current) return;
    dispatched.current = true;
    // A canonical read may arrive after mount; judge the saved clock only then.
    const seen = readActivity();
    store.current = seen;
    for (const tab of state.tabs) if (holds(tab, summaries[tab.id])) seen[tab.id] = { at: now(), hold: true };
    const candidates = state.tabs.map(tab => ({ id: tab.id, kind: tab.kind, pinned: tab.pinned, hasSession: !!tab.sessionFile && !tab.split }));
    const ids = idleTabs(candidates, state.activeId, seen, now());
    if (!ids.length) return;
    const gone = state.tabs.filter(tab => ids.includes(tab.id));
    dispatch({ type: 'history-archive', ids });
    setArchived({ tabs: gone });
    void archiveHistory([...new Set(gone.map(tab => historyIdOf(tab.sessionFile ?? '')))], true).catch(() => undefined);
  }, [ready, state.tabs, state.activeId, summaries]);

  // While running: the active tab, and any tab whose engine state moved, is live now.
  useEffect(() => {
    if (!ready) return;
    // Other Place windows share this clock. Preserve their records and only
    // retire IDs this hydrated workspace has actually owned.
    const activity = readActivity();
    const stamp = now();
    let changed = false;
    const live = new Set(state.tabs.map(tab => tab.id));
    for (const id of owned.current) if (!live.has(id)) { delete activity[id]; owned.current.delete(id); changed = true; }
    for (const id of live) owned.current.add(id);
    for (const tab of state.tabs) {
      const summary = summaries[tab.id];
      const hold = holds(tab, summary);
      const before = activity[tab.id];
      // The active tab is live now, but the stamp moves at most once a minute so typing does not write storage.
      const live = tab.id === state.activeId && (!before || stamp - before.at >= ACTIVE_STAMP_MS);
      const moved = live || !before || before.hold !== hold || (summary?.updatedAt ?? 0) > before.at;
      if (!moved) continue;
      // A tab that stops running or waiting is idle from that moment, not from the last time it was looked at.
      const released = before?.hold === true && !hold;
      const at = live || released ? stamp : Math.max(before?.at ?? stamp, summary?.updatedAt ?? 0);
      activity[tab.id] = { at, hold };
      changed = true;
    }
    store.current = activity;
    if (changed) writeActivity(activity);
  }, [ready, state.tabs, state.activeId, summaries]);

  return [archived, () => setArchived(undefined)];
}

/** Restore all: every archived tab comes back, in the background, and History forgets it was archived. */
export function restoreArchived(archived: Archived, dispatch: Dispatch<WorkspaceAction>) {
  for (const tab of archived.tabs) dispatch({ type: 'open', tab, background: true });
  void archiveHistory([...new Set(archived.tabs.map(tab => historyIdOf(tab.sessionFile ?? '')))], false).catch(() => undefined);
}
