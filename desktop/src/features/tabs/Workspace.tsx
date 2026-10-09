// The workspace: owns the reducer state and composes the strip, the content card and the dialogs.
// Everything with a lane of its own lives in a sibling file (see ARCHITECTURE.md).
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Icon, Text, TextInput, type MenuEntry } from '../../components/ui';
import { nativeControls } from '../../design/nativeControls';
import design from '../../design/tokens.json';
import { toasts } from '../../design/toasts';
import { summarize, type TabSummary } from '../conversation/tabSummary';
import { useBackgroundSessions } from '../conversation/useBackgroundSessions';
import { fileTab, findFileTab, type FileTabKind } from '../files/fileTarget';
import { ArchiveToast } from '../history/ArchiveToast';
import { HistoryHostContext, restoreArchived, useAutoArchive, useHistoryWorkspace } from '../history/host';
import { openKindAction } from '../shell/openKind';
import { createTabActions } from './actions';
import { observedClosedPanes } from './closing/background';
import { useCloseStopKey } from './closing/useCloseStopKey';
import { inboxFocus } from './closing/inboxFocus';
import { useBackground } from './closing/useBackground';
import { useClosing } from './closing/useClosing';
import { TabsApiContext, type TabsApi } from './context';
import type { PaneActions } from './kinds/slots';
import { tabMenuFor } from './hosts/menuHost';
import { NewTabHostContext } from './kinds/newtab/api';
import { kindDef } from './kinds/registry';
import { newTab } from './helpers';
import { worldStore } from '../chat/world-store';
import { focusedPane, freshWorkspace, panesOf, readWorkspace, visibleTabs, workspaceKey, workspaceReducer, type Pane, type Tab, type WorkspaceState, type WorkspaceAction } from './model';
import { FirstTurnContext, type BeforeFirstTurn } from '../conversation/firstTurn';
import type { TintName } from '../places/components/PlaceSwatch';
import { onWorkspaceRequest } from '../places/shell/workspaceBus';
import { useWorkspaceSync } from '../workspace-sync/useWorkspaceSync';
import type { WorkspaceKey } from '../workspace-sync/client';
import { GroupOffer } from './GroupOffer';
import { createUsingClient } from '../places/using-client';
import { usePlacesShell } from '../places/shell/PlacesShell';
import { chatIdFromSessionFile } from '../places/client';
import type { SourceHandoff } from '../places/using-types';
import { openPath } from '../../design/native';
import { publishActiveHome } from '../shell/shellState';
import { PaneGrid } from './PaneGrid';
import { createPreviewStore } from './preview/previewStore';
import { TabOverview } from './TabOverview';
import { TabStrip } from './TabStrip';
import { useTerminalTabs } from '../terminal/useTerminalTabs';
import { useWorkspaceWeb } from '../web/useWorkspaceWeb';
import { useDesktopTabActions, useTabKeys, type Switcher } from './useTabKeys';
import { useWindowHandoff } from './useWindowHandoff';
import './workspace.css';

type Props = {
  enabled: boolean; onActivate: () => void; leading?: ReactNode;
  /** The window's place: `now` or a design-graph place id. Each has its own saved tab set (model.ts workspaceKey). */
  place?: string;
  /** The place's name and tint for its Home tab; absent until the graph has been read. */
  placeTitle?: string; placeTint?: TintName;
  /** Moves on every Go to: the strip then focuses its Home. */
  arrival?: number;
  /** Files a new chat in this place between its creation and its first turn (conversation/firstTurn.ts). */
  firstTurn?: BeforeFirstTurn;
  /** The menu of this strip's Home tab. */
  placeMenu?: MenuEntry[];
  /** While the rail is put away, the Home tab is the place switcher. */
  placeSwitcher?: { items: MenuEntry[]; alert?: string };
  /** Opens a conversation by its chat id. Absent, the strip opens it itself from the world feed (`openChatHere`). */
  onOpenChat?: (chatId: string) => void;
};

/** The saved tabs of a place, with its Home pinned first when it is a design-graph place. */
function initialFor(place: string, title: string): WorkspaceState {
  const home = place === 'now' ? undefined : { id: place, title };
  const state = readWorkspace(workspaceKey(place), () => freshWorkspace(home));
  return home ? workspaceReducer(state, { type: 'home-ensure', place: home.id, title }) : state;
}

export function Workspace({ enabled, onActivate, leading, place = 'now', placeTitle, placeTint, arrival = 0, firstTurn, placeMenu, placeSwitcher, onOpenChat }: Props) {
  const sync = useWorkspaceSync({ key: place as WorkspaceKey, initial: () => initialFor(place, placeTitle ?? 'Home'), focus: new URLSearchParams(window.location.search).get('focusTab') ?? undefined });
  const { state, dispatch: rawDispatch } = sync;
  const dispatch = useCallback((action: WorkspaceAction) => { if (action.type === 'open-inbox') inboxFocus.request(); rawDispatch(action); }, [rawDispatch]);
  const shell = usePlacesShell();
  const [usingApi] = useState(createUsingClient);
  const [summaries, setSummaries] = useState<Record<string, TabSummary>>({});
  const [now, setNow] = useState(Date.now);
  const [previews] = useState(() => createPreviewStore(design.interaction.previewCloseDelay));
  useEffect(() => previews.dispose, [previews]);
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), design.interaction.activityRefreshInterval); return () => window.clearInterval(timer); }, []);
  function receiveSummary(id: string, summary: TabSummary) {
    setSummaries(current => ({ ...current, [id]: summary }));
    if (summary.title) dispatch({ type: 'title', id, title: summary.title, source: 'engine' });
    else if (summary.firstLine) dispatch({ type: 'title', id, title: summary.firstLine, source: 'message' });
  }
  function openTaskTab(source: Pane, taskId: string, title: string) {
    const tab: Tab = { id: crypto.randomUUID(), kind: 'task', title, titleSource: 'manual', pinned: false, draft: '', sessionFile: source.sessionFile, route: { taskId, back: [''], forward: [] } };
    dispatch({ type: 'open-task', tab, background: true, from: source.id });
  }
  /**
   * Opens a saved conversation by its chat id, the way a Home row does: the tab already showing its journal is selected,
   * otherwise a conversation tab reattaches it. The journal comes from the engine-wide world feed; a conversation the
   * engine has not placed on disk cannot be reattached, and that is said rather than an empty tab opened.
   */
  function openChatHere(chatId: string) {
    const row = worldStore.getState().rows.find(candidate => candidate.session === chatId);
    if (!row?.sessionFile) { toasts.show({ message: ['That conversation cannot be opened here: the engine did not say where it is saved.'], tone: 'warning' }); return; }
    const existing = state.tabs.flatMap(tab => panesOf(tab)).find(pane => pane.sessionFile === row.sessionFile);
    dispatch(existing ? { type: 'select', id: existing.id } : { type: 'open', background: false, tab: newTab({ kind: 'conversation', title: row.title || 'Untitled chat', titleSource: 'engine', sessionFile: row.sessionFile }) });
  }
  function openFile(source: Pane, path: string, kind: FileTabKind) {
    const tab = fileTab(source, path, kind, crypto.randomUUID());
    const existing = findFileTab(state.tabs, tab);
    dispatch(existing ? { type: 'select', id: existing } : { type: 'open', tab, background: false });
  }
  const [overviewOpen, setOverviewOpen] = useState(false);
  const [switcher, setSwitcher] = useState<Switcher>(null);
  const switcherRef = useRef<Switcher>(null);
  const switcherFocus = useRef<HTMLDivElement>(null);
  const [rename, setRename] = useState<{ id: string; group: boolean; value: string } | null>(null);
  const renameDialog = useRef<HTMLDialogElement>(null);
  const renameInput = useRef<HTMLInputElement>(null);
  const overviewTrigger = useRef<HTMLButtonElement>(null);
  const active = state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0];
  const visible = visibleTabs(state);

  // Inactive tabs (every pane of them) keep observing their sessions; the active tab's panes stream themselves.
  // Closed tabs whose work goes on keep being read too, so the Inbox follows them to the end.
  const watched = [...state.tabs.filter(tab => tab.id !== active.id).flatMap(tab => panesOf(tab)), ...observedClosedPanes(state.closed, summaries)];
  useBackgroundSessions(watched.filter(pane => pane.sessionFile).map(pane => ({ id: pane.id, sessionFile: pane.sessionFile! })), (id, snapshot) => receiveSummary(id, summarize(snapshot)));
  // The Home tab carries the place's current name; Go to (even to the place already shown) lands on it.
  // A strip mounted by a Go to (arrival already moved) lands on its Home; one mounted by a reload keeps its saved focus.
  const arrived = useRef(arrival > 0 ? -1 : arrival);
  useEffect(() => {
    if (place === 'now' || !placeTitle) return;
    const focus = arrived.current !== arrival;
    arrived.current = arrival;
    dispatch({ type: 'home-ensure', place, title: placeTitle, focus });
  }, [place, placeTitle, arrival]);
  useEffect(() => onWorkspaceRequest(dispatch), [dispatch]);
  const focused = focusedPane(state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0]);
  useEffect(() => publishActiveHome(focused.kind === 'home' ? focused.place : undefined), [focused.kind, focused.place]);

  useEffect(() => {
    if (!switcher) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    switcherFocus.current?.focus();
    return () => {
      if (previous?.closest('.workspace-tab')) document.querySelector<HTMLElement>('.workspace-tabstrip [aria-selected="true"]')?.focus();
      else if (previous?.isConnected) previous.focus();
    };
  }, [!!switcher]);
  useEffect(() => {
    if (!switcher) return;
    switcherFocus.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', behavior: 'instant' });
  }, [switcher]);
  useEffect(() => {
    if (rename) { renameDialog.current?.showModal(); renameInput.current?.focus(); renameInput.current?.select(); }
    else renameDialog.current?.close();
  }, [rename]);

  const closing = useClosing({ state, dispatch, summaries });
  const { closeTab, closeAndStop } = closing;
  const { background, markFailedSeen } = useBackground({ tabs: state.tabs, closed: state.closed, summaries, since: closing.sinceOf, stopping: closing.stopping, now });
  // The Inbox appears the first time work outlives its tab or waits on the person.
  // Failures summon it too: they are bounded (recent, unseen, a few) and leave with Seen.
  const inboxWanted = background.running.length > 0 || background.needsYou.length > 0 || background.failed.length > 0;
  useEffect(() => { if (inboxWanted) dispatch({ type: 'ensure-inbox' }); }, [inboxWanted]);
  useWindowHandoff(dispatch);
  useEffect(() => { if (sync.status.overtaken > 0) toasts.show({ key: `workspace-overtaken-${place}`, message: [`Another window changed ${sync.status.overtaken} of your tab edits. Your latest tab set is shown.`], tone: 'warning', onSettled: sync.acknowledge }); }, [sync.status.overtaken, place]);
  useEffect(() => { if (sync.status.error) toasts.show({ key: `workspace-status-${place}`, message: [sync.status.error], tone: 'warning', actions: [{ label: 'Try again', onSelect: sync.retry }] }); }, [sync.status.error, place]);
  // A tab moves to a new window on THIS strip's place, which a Places window can change after it opened.
  const placeNow = useRef(place);
  placeNow.current = place;
  const syncNow = useRef(sync);
  syncNow.current = sync;
  const latestState = useRef(state);
  latestState.current = state;
  const [actions] = useState(() => createTabActions({ native: nativeControls(), toasts, place: () => placeNow.current, handoffView: id => { syncNow.current.handoff(id); }, stillHere: id => latestState.current.tabs.some(tab => tab.id === id) }));
  useCloseStopKey(enabled, () => closeAndStop(state.activeId));
  function startRename(id: string, group = false) { setRename({ id, group, value: (group ? state.groups : state.tabs).find(item => item.id === id)?.title ?? '' }); }
  useTabKeys({ enabled, state, dispatch, visible, overviewOpen, setOverviewOpen, closeTab, switcherRef, setSwitcher });
  useTerminalTabs({ enabled, state, dispatch });
  // Web tabs: a page's new window and a link's open-in-tab modifier open a web tab, a page can start a conversation, and a closed tab's native view closes.
  useWorkspaceWeb(state.tabs, dispatch);
  useDesktopTabActions({ state, dispatch, visible, renaming: !!rename, onActivate, closeTab, setOverviewOpen });
  const historyHost = useHistoryWorkspace(state, dispatch);
  const [archived, dismissArchived] = useAutoArchive(state, dispatch, summaries);

  const api: TabsApi = { state, dispatch, summaries, now, closeTab, closeAndStop, closeMany: closing.closeMany, isRunning: closing.isRunning, background, markFailedSeen, openChat: onOpenChat ?? openChatHere, canOpenChat: id => !!worldStore.getState().rows.find(row => row.session === id)?.sessionFile, actions, reopenClosed: closing.reopenClosed, startRename, receiveSummary, previews, overlayOpen: !!switcher || overviewOpen || !!rename,
    placeTint: place === 'now' ? undefined : placeTint, placeMenu, placeSwitcher };
  const newTabHost = { state, summaries, dispatch, closeTab };
  const actionsFor = (pane: Pane): PaneActions => ({
    onDraft: draft => dispatch({ type: 'draft', id: pane.id, draft }),
    onView: change => dispatch({ type: 'view', id: pane.id, change }),
    onSummary: summary => receiveSummary(pane.id, summary),
    onOpenTaskTab: (taskId, title) => openTaskTab(pane, taskId, title),
    onOpenFile: (path, kind) => openFile(pane, path, kind),
    usingApi,
    onOpenSource: (source: SourceHandoff) => {
      if (source.kind === 'chat') (onOpenChat ?? openChatHere)(source.chatId);
      else if (source.kind === 'url') dispatch({ type: 'open', background: false, tab: newTab({ kind: 'web', title: source.url, target: { url: source.url } }) });
      else if (source.kind === 'file') openFile(pane, source.path, 'file');
      else void openPath(source.path, source.repoRoot ?? source.path).catch(error => toasts.show({ message: [error instanceof Error ? error.message : 'Could not open that source'], tone: 'warning' }));
    },
    onAddToPlace: shell && pane.sessionFile ? () => { const chatId = chatIdFromSessionFile(pane.sessionFile!); if (chatId) shell.openChooser({ kind: 'file', chatIds: [chatId], chatTitle: pane.title, exclude: [] }); } : undefined,
  });
  return <TabsApiContext.Provider value={api}><section className="tab-workspace" aria-label="Conversation workspace">
    <TabStrip api={api} leading={leading} overviewTrigger={overviewTrigger} onOverview={() => setOverviewOpen(true)}/>
    <FirstTurnContext.Provider value={firstTurn}><NewTabHostContext.Provider value={newTabHost}><HistoryHostContext.Provider value={historyHost}><PaneGrid tab={active} tabs={state.tabs} dispatch={dispatch} actionsFor={actionsFor} retainedPaneIds={state.closed.flatMap(tab => panesOf(tab).map(pane => pane.id))}/></HistoryHostContext.Provider></NewTabHostContext.Provider></FirstTurnContext.Provider>
    <GroupOffer api={api}/>
    {archived && <ArchiveToast count={archived.tabs.length} onDismiss={dismissArchived} onReview={() => { dispatch(openKindAction(state, 'history')); dismissArchived(); }} onRestore={() => { restoreArchived(archived, dispatch); dismissArchived(); }}/>}
    {switcher && <div className="workspace-switcher"><div ref={switcherFocus} className="workspace-switcher-list" role="listbox" tabIndex={0} aria-label="Switch tabs" aria-activedescendant={`switcher-${switcher.ids[switcher.index]}`}>
      {switcher.ids.map((id, index) => { const tab = state.tabs.find(t => t.id === id); return tab ? <Button key={id} id={`switcher-${id}`} className="workspace-switcher-item" role="option" aria-selected={index === switcher.index} tabIndex={-1} onClick={() => { dispatch({ type: 'select', id }); switcherRef.current = null; setSwitcher(null); }}><Icon name={tab.pinned ? 'pin' : kindDef(focusedPane(tab).kind).icon} size="sm"/><span>{tab.title}</span></Button> : null; })}
    </div><Text>Release Ctrl to switch · Escape to cancel</Text>
    </div>}
    <TabOverview summaries={summaries} now={now} returnFocus={overviewTrigger} open={overviewOpen} tabs={state.tabs} groups={state.groups} activeId={state.activeId} onSplitGroup={groupId => dispatch({ type: 'split-group', groupId })} onClose={() => setOverviewOpen(false)} onSelect={id => dispatch({ type: 'select', id })} menuFor={tab => tabMenuFor(api, tab)} onMoveGroup={(id, groupId) => dispatch({ type: 'move-group', id, groupId })} onCloseTab={closeTab}/>
    <dialog ref={renameDialog} className="workspace-rename" aria-label={rename?.group ? 'Rename group' : 'Rename tab'} onCancel={() => setRename(null)} onClose={() => setRename(null)}>
      <form onSubmit={event => { event.preventDefault(); if (rename) dispatch({ type: rename.group ? 'rename-group' : 'rename', id: rename.id, title: rename.value }); setRename(null); }}><TextInput ref={renameInput} aria-label="Name" value={rename?.value ?? ''} maxLength={80} onChange={event => setRename(current => current ? { ...current, value: event.target.value } : null)}/><div className="workspace-rename-actions"><Button onClick={() => setRename(null)}>Cancel</Button><Button type="submit" variant="quiet">Save</Button></div></form>
    </dialog>
  </section></TabsApiContext.Provider>;
}
