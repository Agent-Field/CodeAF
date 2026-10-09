// The workspace: owns the reducer state and composes the strip, the content card and the dialogs.
// Everything with a lane of its own lives in a sibling file (see ARCHITECTURE.md).
import { useEffect, useReducer, useRef, useState, type ReactNode } from 'react';
import { Button, Icon, Text, TextInput, type MenuEntry } from '../../components/ui';
import { nativeControls } from '../../design/nativeControls';
import design from '../../design/tokens.json';
import { summarize, type TabSummary } from '../conversation/tabSummary';
import { useBackgroundSessions } from '../conversation/useBackgroundSessions';
import { fileTab, findFileTab, type FileTabKind } from '../files/fileTarget';
import type { TabsApi } from './context';
import type { PaneActions } from './kinds/slots';
import { NewTabHostContext } from './kinds/newtab/api';
import { kindDef } from './kinds/registry';
import { focusedPane, freshWorkspace, panesOf, parseWorkspace, readWorkspace, visibleTabs, workspaceKey, workspaceReducer, type Pane, type Tab, type WorkspaceState } from './model';
import { FirstTurnContext, type BeforeFirstTurn } from '../conversation/firstTurn';
import type { TintName } from '../places/components/PlaceSwatch';
import { onWorkspaceRequest } from '../places/shell/workspaceBus';
import { useWindowHandoffs } from '../places/shell/windowHandoffs';
import { sharedShape } from './reducers/home';
import { publishActiveHome } from '../shell/shellState';
import { PaneGrid } from './PaneGrid';
import { createPreviewStore } from './preview/previewStore';
import { TabOverview } from './TabOverview';
import { TabStrip } from './TabStrip';
import { useTerminalTabs } from '../terminal/useTerminalTabs';
import { useDesktopTabActions, useTabKeys, type Switcher } from './useTabKeys';
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
  /** A failure with no page of its own (a tab that could not move to a new window). */
  onError?: (failure: unknown) => void;
};

/** The saved tabs of a place, with its Home pinned first when it is a design-graph place. */
function initialFor(place: string, title: string): WorkspaceState {
  const home = place === 'now' ? undefined : { id: place, title };
  const state = readWorkspace(workspaceKey(place), () => freshWorkspace(home));
  return home ? workspaceReducer(state, { type: 'home-ensure', place: home.id, title }) : state;
}

export function Workspace({ enabled, onActivate, leading, place = 'now', placeTitle, placeTint, arrival = 0, firstTurn, placeMenu, placeSwitcher, onError }: Props) {
  const key = workspaceKey(place);
  const native = nativeControls();
  const [state, dispatch] = useReducer(workspaceReducer, undefined, () => initialFor(place, placeTitle ?? 'Home'));
  // A tab set adopted from another window is not written back: that window already saved it.
  const adopted = useRef<string | undefined>(undefined);
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
    dispatch({ type: 'open', tab, background: true });
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
  useBackgroundSessions(state.tabs.filter(tab => tab.id !== active.id).flatMap(tab => panesOf(tab)).filter(pane => pane.sessionFile).map(pane => ({ id: pane.id, sessionFile: pane.sessionFile! })), (id, snapshot) => receiveSummary(id, summarize(snapshot)));
  useEffect(() => {
    if (adopted.current === sharedShape(state)) return;
    try { localStorage.setItem(key, JSON.stringify(state)); } catch { /* A full or unavailable store must not interrupt local tab navigation. */ }
  }, [state, key]);
  // Two windows on one place show one tab set (Places 6d): another window's save arrives as a storage event.
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key !== key) return;
      const incoming = parseWorkspace(event.newValue);
      if (!incoming) return;
      adopted.current = sharedShape(incoming);
      dispatch({ type: 'adopt', state: incoming });
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [key]);
  // The Home tab carries the place's current name; Go to (even to the place already shown) lands on it.
  // A strip mounted by a Go to (arrival already moved) lands on its Home; one mounted by a reload keeps its saved focus.
  const arrived = useRef(arrival > 0 ? -1 : arrival);
  useEffect(() => {
    if (place === 'now' || !placeTitle) return;
    const focus = arrived.current !== arrival;
    arrived.current = arrival;
    dispatch({ type: 'home-ensure', place, title: placeTitle, focus });
  }, [place, placeTitle, arrival]);
  useEffect(() => onWorkspaceRequest(dispatch), []);
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

  function closeTab(id: string) {
    const restore = !!document.activeElement?.closest('.workspace-tab');
    dispatch({ type: 'close', id });
    if (restore) requestAnimationFrame(() => document.querySelector<HTMLElement>('.workspace-tabstrip [aria-selected="true"]')?.focus());
  }
  function startRename(id: string, group = false) { setRename({ id, group, value: (group ? state.groups : state.tabs).find(item => item.id === id)?.title ?? '' }); }
  useTabKeys({ enabled, state, dispatch, visible, overviewOpen, setOverviewOpen, closeTab, switcherRef, setSwitcher });
  useTerminalTabs({ enabled, state, dispatch });
  useDesktopTabActions({ state, dispatch, visible, renaming: !!rename, onActivate, closeTab, setOverviewOpen });
  useWindowHandoffs(dispatch, closeTab);

  const api: TabsApi = { state, dispatch, summaries, now, closeTab, startRename, receiveSummary, previews, overlayOpen: !!switcher || overviewOpen || !!rename, placeTint: place === 'now' ? undefined : placeTint, placeMenu, placeSwitcher,
    moveToNewWindow: native.desktop ? tab => { void native.openPlaceWindow(place === 'now' ? 'now' : place as `pl_${string}`, { pane: focusedPane(tab) }).catch(failure => onError?.(failure)); } : undefined };
  const newTabHost = { state, summaries, dispatch, closeTab };
  const actionsFor = (pane: Pane): PaneActions => ({
    onDraft: draft => dispatch({ type: 'draft', id: pane.id, draft }),
    onView: change => dispatch({ type: 'view', id: pane.id, change }),
    onSummary: summary => receiveSummary(pane.id, summary),
    onOpenTaskTab: (taskId, title) => openTaskTab(pane, taskId, title),
    onOpenFile: (path, kind) => openFile(pane, path, kind),
  });
  return <section className="tab-workspace" aria-label="Conversation workspace">
    <TabStrip api={api} leading={leading} overviewTrigger={overviewTrigger} onOverview={() => setOverviewOpen(true)}/>
    <FirstTurnContext.Provider value={firstTurn}><NewTabHostContext.Provider value={newTabHost}><PaneGrid tab={active} tabs={state.tabs} dispatch={dispatch} actionsFor={actionsFor}/></NewTabHostContext.Provider></FirstTurnContext.Provider>
    {switcher && <div className="workspace-switcher"><div ref={switcherFocus} className="workspace-switcher-list" role="listbox" tabIndex={0} aria-label="Switch tabs" aria-activedescendant={`switcher-${switcher.ids[switcher.index]}`}>
      {switcher.ids.map((id, index) => { const tab = state.tabs.find(t => t.id === id); return tab ? <Button key={id} id={`switcher-${id}`} className="workspace-switcher-item" role="option" aria-selected={index === switcher.index} tabIndex={-1} onClick={() => { dispatch({ type: 'select', id }); switcherRef.current = null; setSwitcher(null); }}><Icon name={tab.pinned ? 'pin' : kindDef(focusedPane(tab).kind).icon} size="sm"/><span>{tab.title}</span></Button> : null; })}
    </div><Text>Release Ctrl to switch · Escape to cancel</Text>
    </div>}
    <TabOverview summaries={summaries} now={now} returnFocus={overviewTrigger} open={overviewOpen} tabs={state.tabs} groups={state.groups} activeId={state.activeId} onSplitGroup={groupId => dispatch({ type: 'split-group', groupId })} onClose={() => setOverviewOpen(false)} onSelect={id => dispatch({ type: 'select', id })} onPin={id => dispatch({ type: 'pin', id })} onMoveGroup={(id, groupId) => dispatch({ type: 'move-group', id, groupId })} onCreateGroup={id => dispatch({ type: 'group', id })} onCloseTab={id => dispatch({ type: 'close', id })}/>
    <dialog ref={renameDialog} className="workspace-rename" aria-label={rename?.group ? 'Rename group' : 'Rename tab'} onCancel={() => setRename(null)} onClose={() => setRename(null)}>
      <form onSubmit={event => { event.preventDefault(); if (rename) dispatch({ type: rename.group ? 'rename-group' : 'rename', id: rename.id, title: rename.value }); setRename(null); }}><TextInput ref={renameInput} aria-label="Name" value={rename?.value ?? ''} maxLength={80} onChange={event => setRename(current => current ? { ...current, value: event.target.value } : null)}/><div className="workspace-rename-actions"><Button onClick={() => setRename(null)}>Cancel</Button><Button type="submit" variant="quiet">Save</Button></div></form>
    </dialog>
  </section>;
}
