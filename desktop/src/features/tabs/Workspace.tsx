// The workspace: owns the reducer state and composes the strip, the content card and the dialogs.
// Everything with a lane of its own lives in a sibling file (see ARCHITECTURE.md).
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { Button, Icon, Text, TextInput, type MenuEntry } from '../../components/ui';
import { nativeControls } from '../../design/nativeControls';
import design from '../../design/tokens.json';
import { toasts } from '../../design/toasts';
import type { TabSummary } from '../conversation/tabSummary';
import { mergeBackgroundSummary, useBackgroundSessions } from '../conversation/useBackgroundSessions';
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
import { useOverviewGesture } from './useOverviewGesture';
import { useStructuralUndo } from './undo/useStructuralUndo';
import { TabsApiContext, type TabsApi } from './context';
import type { PaneActions } from './kinds/slots';
import { tabMenuFor } from './hosts/menuHost';
import { NewTabHostContext } from './kinds/newtab/api';
import { useNewTabKeys } from './kinds/newtab/useNewTabKeys';
import { kindDef } from './kinds/registry';
import { newTab } from './helpers';
import { blockingOf, type AttentionItem } from '../chat/world-client';
import { worldStore } from '../chat/world-store';
import { focusedPane, freshWorkspace, panesOf, readWorkspace, visibleTabs, workspaceKey, workspaceReducer, type Pane, type Tab, type WorkspaceState, type WorkspaceAction } from './model';
import { FirstTurnContext, NewConversationPlaceContext, type BeforeFirstTurn } from '../conversation/firstTurn';
import { placeTints, type TintName } from '../places/components/PlaceSwatch';
import { onWorkspaceRequest } from '../places/shell/workspaceBus';
import { useWorkspaceSync } from '../workspace-sync/useWorkspaceSync';
import type { WorkspaceKey } from '../workspace-sync/client';
import { SuggestPill } from './SuggestPill';
import { useUndoKeys } from './undo/useUndoKeys';
import { createUsingClient } from '../places/using-client';
import { usePlacesShell } from '../places/shell/PlacesShell';
import { chatIdFromSessionFile } from '../places/client';
import type { SourceHandoff } from '../places/using-types';
import { openPath } from '../../design/native';
import { publishActiveHome } from '../shell/shellState';
import { PaneGrid } from './PaneGrid';
import { createPreviewStore } from './preview/previewStore';
import { TabOverview } from './TabOverview';
import { requestNextUp, TabStrip, type StripBack, type StripFrame } from './TabStrip';
import { bannerVisible, type NextUpBannerItem } from '../nextup/Banner';
import { queueRowOf } from '../nextup/QueuePopover';
import { useNextUp } from '../nextup/useNextUp';
import { nextUpWalk, useNextUpWalk, type WalkOrigin } from '../nextup/useNextUpWalk';
import { useNextUpKeys } from '../nextup/useNextUpKeys';
import type { FocusEntry } from '../focus-history/model';
import { useFocusWireFor, useOptionalFocusWire } from '../focus-history/useFocusHistory';
import { routeTask } from './view-state';
import { namedScrollSpots, useScrollMemory } from './scroll/memoryStore';
import { questionFocus } from '../conversation/questionFocus';
import { useTerminalTabs } from '../terminal/useTerminalTabs';
import { useWorkspaceWeb } from '../web/useWorkspaceWeb';
import { useGroupKeys } from './useGroupKeys';
import { useDesktopTabActions, useTabKeys, type Switcher } from './useTabKeys';
import { useWindowHandoff } from './useWindowHandoff';
import { useTabLinks } from './links/useTabLinks';
import './workspace.css';

/** Per window, so two windows do not share a back stack (P-24). The browser's one window is `main`. The seam spec duplicates the `main` key because it cannot import this module. */
export const focusHistoryStorageKey = (label: string) => `codeaf.desktop.focus.v1.${label}`;

/** A place tint the swatch knows. Anything else is graphite, the neutral, rather than a colour the feed invented. */
function swatchTint(value: string | undefined): TintName {
  return (placeTints as readonly string[]).includes(value ?? '') ? value as TintName : 'graphite';
}

/**
 * The banner's second line: the conversation the feed named, then the work the question holds up.
 * One named task is spelled; several become a count. Nothing named leaves the line off.
 */
function bannerDetail(item: AttentionItem): string | undefined {
  const conversation = typeof item.title === 'string' ? item.title.trim() : '';
  const named = (item.holdingUp ?? []).map(name => name.trim()).filter(name => name !== '');
  const tasks = blockingOf(item).tasks.filter(name => typeof name === 'string' && name.trim() !== '');
  const count = named.length || tasks.length;
  const hold = count > 1 ? `holding up ${count} tasks` : count === 1 ? `holding up ${named[0] || tasks[0]}` : '';
  const line = [conversation, hold].filter(part => part !== '').join(' · ');
  return line || undefined;
}

function bannerFrom(item: AttentionItem): NextUpBannerItem {
  const blocking = blockingOf(item);
  return {
    id: item.key,
    blocking: blocking.turn || blocking.tasks.some(name => typeof name === 'string' && name !== ''),
    head: typeof item.text === 'string' ? item.text : '',
    detail: bannerDetail(item),
  };
}

/**
 * A banner for a blocking question that arrives after the feed's first reading. Questions already
 * waiting when the window opens stay on the pill and do not slide out.
 */
function useArrivingBanner(elsewhere: readonly AttentionItem[]): NextUpBannerItem | null {
  const world = useSyncExternalStore(worldStore.subscribe, worldStore.getState, worldStore.getState);
  const seeded = useRef(false);
  const known = useRef(new Set<string>());
  const [current, setCurrent] = useState<NextUpBannerItem | null>(null);
  const worldKeys = world.items.map(item => item.key).join('\0');
  const elsewhereKeys = elsewhere.map(item => item.key).join('\0');
  useEffect(() => {
    if (!seeded.current) {
      if (world.status !== 'live' && world.items.length === 0) return;
      seeded.current = true;
      known.current = new Set(world.items.map(item => item.key));
      return;
    }
    const fresh = elsewhere.find(item => !known.current.has(item.key) && bannerVisible(bannerFrom(item)));
    for (const item of world.items) if (item.key) known.current.add(item.key);
    if (fresh) setCurrent(bannerFrom(fresh));
  }, [worldKeys, elsewhereKeys, world.status, world.items, elsewhere]);
  return current;
}

/** Where Back returns. Another place is named as a place; a tab in this place is named by its title. Unknown is nothing. */
function backTargetName(entry: FocusEntry, tabs: readonly Tab[], place: string, placeName: (id: string) => string | undefined): string {
  if (entry.windowPlace !== place) {
    if (entry.windowPlace === 'now') return 'Now';
    if (entry.windowPlace === 'root') return 'All places';
    return placeName(entry.windowPlace) ?? '';
  }
  return tabs.find(tab => tab.id === entry.tabId)?.title ?? '';
}

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
  const windowQuery = new URLSearchParams(window.location.search);
  const sync = useWorkspaceSync({ key: place as WorkspaceKey, initial: () => initialFor(place, placeTitle ?? 'Home'), focus: windowQuery.get('tab') ?? windowQuery.get('focusTab') ?? undefined });
  const { state } = sync;
  const { dispatch: rawDispatch, undo } = useStructuralUndo({ state, dispatch: sync.dispatch, enabled, keys: false });
  useUndoKeys({ undo }, enabled);
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
  const gestureRoot = useRef<HTMLElement>(null);
  const openOverview = useCallback(() => setOverviewOpen(true), []);
  useOverviewGesture(gestureRoot, enabled && !overviewOpen, openOverview);
  const [switcher, setSwitcher] = useState<Switcher>(null);
  const switcherRef = useRef<Switcher>(null);
  const switcherFocus = useRef<HTMLDivElement>(null);
  const [rename, setRename] = useState<{ id: string; group: boolean; value: string } | null>(null);
  const renameDialog = useRef<HTMLDialogElement>(null);
  const renameInput = useRef<HTMLInputElement>(null);
  const overviewTrigger = useRef<HTMLButtonElement>(null);
  const active = state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0];
  const visible = visibleTabs(state);

  // Inactive tabs take running, needs-you and title from the world stream. Only the open tab holds a
  // conversation stream. A world row has no transcript, so a reply already read is kept.
  const summariesNow = useRef(summaries);
  summariesNow.current = summaries;
  const watched = [...state.tabs.filter(tab => tab.id !== active.id).flatMap(tab => panesOf(tab)), ...observedClosedPanes(state.closed, summaries)];
  useBackgroundSessions(watched.filter(pane => pane.sessionFile).map(pane => ({ id: pane.id, sessionFile: pane.sessionFile! })), (id, snapshot) => {
    const summary = mergeBackgroundSummary(summariesNow.current[id], snapshot);
    setSummaries(current => ({ ...current, [id]: summary }));
    if (snapshot.title.trim()) dispatch({ type: 'title', id, title: snapshot.title.trim(), source: 'engine' });
  });
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
  const activeTab = state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0];
  const focused = focusedPane(activeTab);
  // P-3: the conversation you're in is the focused pane's. A task or file on that session is the same conversation.
  const conversationKey = focused.sessionFile ? chatIdFromSessionFile(focused.sessionFile) : '';
  const queue = useNextUp(conversationKey);
  const banner = useArrivingBanner(queue.items);
  const tintOf = (placeId: string | undefined): TintName => swatchTint(placeId ? shell?.index?.byId.get(placeId)?.effectiveTint : undefined);
  const frame: StripFrame = {
    count: queue.count,
    rows: queue.items.map(item => queueRowOf(item, tintOf)),
    acceptable: queue.acceptable.length,
    banner,
    onOpen: () => requestNextUp(),
    onJump: itemKey => requestNextUp({ itemKey }),
    onStart: () => requestNextUp(),
    onAccept: () => { void queue.accept(); },
  };
  // The workspace keeps window history and prefers the outer wire when the shell supplies one.
  const outerFocus = useOptionalFocusWire();
  const [windowLabel, setWindowLabel] = useState('main');
  const focusNavigate = useRef<(entry: FocusEntry) => void>(() => {});
  focusNavigate.current = entry => {
    if (entry.windowPlace !== place) void shell?.goTo(entry.windowPlace);
    dispatch({ type: 'select', id: entry.tabId });
  };
  const localFocus = useFocusWireFor(outerFocus ? undefined : focusHistoryStorageKey(windowLabel), entry => focusNavigate.current(entry));
  const focusWire = outerFocus ?? localFocus;
  const focusSnap = useSyncExternalStore(focusWire.subscribe, focusWire.getSnapshot, focusWire.getSnapshot);
  const scrollMemory = useScrollMemory();
  const walkOrigin = (): WalkOrigin => ({ place, tabId: activeTab.id, paneId: focused.id, label: placeTitle ?? activeTab.title, conversation: conversationKey, draft: focused.draft, route: focused.route, scroll: scrollMemory?.get(focused.id) });
  const walk = useNextUpWalk({
    enabled, place,
    origin: walkOrigin,
    goTo: id => shell ? shell.goTo(id) : Promise.reject(new Error('That place is unavailable.')),
    warn: reason => shell?.warn(reason),
    open: item => {
      const row = worldStore.getState().rows.find(row => row.session === item.session);
      if (!row?.sessionFile) { shell?.warn(new Error('The engine did not say where this conversation is saved.')); return; }
      focusWire.markCause('next-up');
      const existing = state.tabs.flatMap(tab => panesOf(tab)).find(pane => pane.kind === 'conversation' && pane.sessionFile === row.sessionFile);
      const pane = existing ?? newTab({ kind: 'conversation', title: row.title || 'Untitled chat', titleSource: 'engine', sessionFile: row.sessionFile });
      dispatch(existing ? { type: 'select', id: pane.id } : { type: 'open', background: false, tab: pane as Tab });
      if (existing?.route) dispatch({ type: 'view', id: pane.id, change: { route: undefined } });
      if (item.id !== undefined) questionFocus.request(item.session, { kind: item.kind, id: item.id });
    },
    restore: origin => {
      for (const [key, spot] of origin.scroll ?? []) scrollMemory?.set(origin.paneId, key, spot);
      dispatch({ type: 'select', id: origin.paneId });
      dispatch({ type: 'view', id: origin.paneId, change: { route: origin.route } });
      if (origin.draft !== undefined) dispatch({ type: 'draft', id: origin.paneId, draft: origin.draft });
    },
  });
  useNextUpKeys({ enabled, origin: walkOrigin, wire: focusWire });
  const backEntry = focusSnap.chip;
  const backName = backEntry ? backTargetName(backEntry, state.tabs, place, id => shell?.index?.byId.get(id)?.name) : '';
  const back: StripBack | undefined = walk.origin && walk.phase !== 'idle' ? {
    key: `walk:${walk.origin.place}:${walk.origin.tabId}`, parentName: walk.origin.label,
    onBack: () => nextUpWalk.exit(), onDismiss: () => {},
  } : backEntry && backName ? {
    key: `${focusSnap.history.cursor}:${backEntry.windowPlace}:${backEntry.tabId}:${backEntry.drillPath.join('/')}`,
    parentName: backName,
    onBack: () => { focusWire.back(); },
    onDismiss: () => { focusWire.dismissChip(); },
  } : undefined;
  useEffect(() => {
    let live = true;
    void nativeControls().currentWindow().then(info => { if (live && info.label) setWindowLabel(info.label); });
    return () => { live = false; };
  }, []);
  // The tab id is the step, not the pane inside a split. The first report after a saved stack is a relaunch, so a chip that was already there stays.
  // Scroll and the draft ride on that step: switching places prunes every pane that is not on the strip being shown,
  // and ⌘[ would otherwise come back to the tab at the bottom with no way to know where the reader had left it.
  const noteFocus = () => {
    if (!activeTab) return;
    // A place's Home on the way to a question is navigation in progress, not another focus step.
    if (walk.phase === 'walking' && (focused.kind !== 'conversation' || conversationKey !== walk.item?.session)) return;
    if (walk.phase === 'returning' && focused.id !== walk.origin?.paneId) return;
    if (walk.phase === 'walking') focusWire.markCause('next-up');
    const task = focused.route ? routeTask(focused.route) : undefined;
    const scroll = namedScrollSpots(focused.id);
    focusWire.observe({
      windowPlace: place, tabId: activeTab.id, drillPath: task ? [task] : [],
      // Nothing read yet is not a position. Omitting it keeps spots the step already stored.
      ...(scroll.length ? { scroll } : {}),
      draftKey: focused.draft ? focused.id : null,
    });
  };
  const noteFocusNow = useRef(noteFocus);
  noteFocusNow.current = noteFocus;
  useEffect(() => {
    focusWire.setTabs(place, state.tabs.map(tab => tab.id));
    noteFocusNow.current();
  }, [focusWire, place, state.tabs, activeTab, focused.route, focused.draft, focused.id, walk.phase, walk.item?.key]);
  // Scroll does not bubble. Capture on the workspace sees it after the pane's own frame has stored the spot.
  useEffect(() => {
    const root = gestureRoot.current;
    if (!root) return;
    let waiting = 0;
    const note = () => {
      if (waiting) return;
      waiting = window.setTimeout(() => { waiting = 0; noteFocusNow.current(); }, 0);
    };
    root.addEventListener('scroll', note, true);
    return () => {
      root.removeEventListener('scroll', note, true);
      if (waiting) window.clearTimeout(waiting);
    };
  }, []);
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
  // Incoming durable links reuse canonical dispatch and never start work.
  useTabLinks({ enabled, state, dispatch, actions });
  useCloseStopKey(enabled, () => closeAndStop(state.activeId));
  function startRename(id: string, group = false) { setRename({ id, group, value: (group ? state.groups : state.tabs).find(item => item.id === id)?.title ?? '' }); }
  const groupSelected = useGroupKeys({ enabled, state, dispatch, startRename });
  useTabKeys({ enabled, state, dispatch, visible, overviewOpen, setOverviewOpen, closeTab, switcherRef, setSwitcher });
  useTerminalTabs({ enabled, state, dispatch });
  // Web tabs: a page's new window and a link's open-in-tab modifier open a web tab, a page can start a conversation, and a closed tab's native view closes.
  useWorkspaceWeb(state.tabs, dispatch);
  useDesktopTabActions({ state, dispatch, visible, renaming: !!rename, onActivate, closeTab, closeAndStop, setOverviewOpen });
  const historyHost = useHistoryWorkspace(state, dispatch);
  const [archived, dismissArchived] = useAutoArchive(state, dispatch, summaries, Date.now, sync.status.phase === 'saved' || sync.status.phase === 'saving');

  const api: TabsApi = { workspaceKey: place, state, dispatch, summaries, now, closeTab, closeAndStop, closeMany: closing.closeMany, isRunning: closing.isRunning, background, markFailedSeen, openChat: onOpenChat ?? openChatHere, canOpenChat: id => !!worldStore.getState().rows.find(row => row.session === id)?.sessionFile, actions, reopenClosed: closing.reopenClosed, startRename, groupSelected, receiveSummary, previews, overlayOpen: !!switcher || overviewOpen || !!rename,
    placeTint: place === 'now' ? undefined : placeTint, placeMenu, placeSwitcher };
  useNewTabKeys(api);
  const newTabHost = { state, summaries, dispatch, closeTab, receiveTransfer: sync.receiveTransfer };
  const actionsFor = (pane: Pane): PaneActions => ({
    onDraft: draft => dispatch({ type: 'draft', id: pane.id, draft }),
    onView: change => dispatch({ type: 'view', id: pane.id, change }),
    onSummary: summary => receiveSummary(pane.id, summary),
    onOpenTaskTab: (taskId, title) => openTaskTab(pane, taskId, title),
    onOpenFile: (path, kind) => openFile(pane, path, kind),
    onRename: title => dispatch({ type: 'rename', id: pane.id, title }),
    // The tab model has no anchor slot yet (see DESIGN-QUESTIONS), so the new tab opens at the session's end.
    onOpenConversationTab: (sessionFile, _anchor, background = false) => dispatch({ type: 'open', background, tab: newTab({ kind: 'conversation', title: pane.title, titleSource: pane.titleSource, sessionFile }) }),
    usingApi,
    onOpenSource: (source: SourceHandoff) => {
      if (source.kind === 'chat') (onOpenChat ?? openChatHere)(source.chatId);
      else if (source.kind === 'url') dispatch({ type: 'open', background: false, tab: newTab({ kind: 'web', title: source.url, target: { url: source.url } }) });
      else if (source.kind === 'file') openFile(pane, source.path, 'file');
      else void openPath(source.path, source.repoRoot ?? source.path).catch(error => toasts.show({ message: [error instanceof Error ? error.message : 'Could not open that source'], tone: 'warning' }));
    },
    onAddToPlace: shell && pane.sessionFile ? () => { const chatId = chatIdFromSessionFile(pane.sessionFile!); if (chatId) shell.openChooser({ kind: 'file', chatIds: [chatId], chatTitle: pane.title, exclude: [] }); } : undefined,
  });
  return <TabsApiContext.Provider value={api}><section ref={gestureRoot} className="tab-workspace" aria-label="Conversation workspace" data-nextup-walk={walk.phase !== 'idle' || undefined}>
    <TabStrip api={api} leading={leading} back={back} frame={frame} overviewTrigger={overviewTrigger} onOverview={openOverview}/>
    <NewConversationPlaceContext.Provider value={place === 'now' || place === 'root' ? undefined : place}><FirstTurnContext.Provider value={firstTurn}><NewTabHostContext.Provider value={newTabHost}><HistoryHostContext.Provider value={historyHost}><PaneGrid overlay={!api.overlayOpen && <SuggestPill api={api}/>} tab={active} tabs={state.tabs} dispatch={dispatch} actionsFor={actionsFor} retainedPaneIds={state.closed.flatMap(tab => panesOf(tab).map(pane => pane.id))}/></HistoryHostContext.Provider></NewTabHostContext.Provider></FirstTurnContext.Provider></NewConversationPlaceContext.Provider>
    {archived && <ArchiveToast count={archived.tabs.length} onDismiss={dismissArchived} onReview={() => { dispatch(openKindAction(state, 'history')); dismissArchived(); }} onRestore={() => { restoreArchived(archived, dispatch); dismissArchived(); }}/>}
    {switcher && <div className="workspace-switcher"><div ref={switcherFocus} className="workspace-switcher-list" role="listbox" tabIndex={0} aria-label="Switch tabs" aria-activedescendant={`switcher-${switcher.ids[switcher.index]}`}>
      {switcher.ids.map((id, index) => { const tab = state.tabs.find(t => t.id === id); return tab ? <Button key={id} id={`switcher-${id}`} className="workspace-switcher-item" role="option" aria-selected={index === switcher.index} tabIndex={-1} onClick={() => { dispatch({ type: 'select', id }); switcherRef.current = null; setSwitcher(null); }}><Icon name={tab.pinned ? 'pin' : kindDef(focusedPane(tab).kind).icon} size="sm"/><span>{tab.title}</span></Button> : null; })}
    </div><Text>Release Ctrl to switch · Escape to cancel</Text>
    </div>}
    <TabOverview summaries={summaries} now={now} returnFocus={overviewTrigger} open={overviewOpen} tabs={state.tabs} groups={state.groups} activeId={state.activeId} onSplitGroup={groupId => dispatch({ type: 'split-group', groupId })} onClose={() => setOverviewOpen(false)} onSelect={id => dispatch({ type: 'select', id })} menuFor={tab => tabMenuFor(api, tab)} onMoveGroup={(id, groupId, beside) => dispatch({ type: 'move-group', id, groupId, beside })} onCloseTab={closeTab}/>
    <dialog ref={renameDialog} className="workspace-rename" aria-label={rename?.group ? 'Rename group' : 'Rename tab'} onCancel={() => setRename(null)} onClose={() => setRename(null)}>
      <form onSubmit={event => { event.preventDefault(); if (rename) dispatch({ type: rename.group ? 'rename-group' : 'rename', id: rename.id, title: rename.value }); setRename(null); }}><TextInput ref={renameInput} aria-label="Name" value={rename?.value ?? ''} maxLength={80} onChange={event => setRename(current => current ? { ...current, value: event.target.value } : null)}/><div className="workspace-rename-actions"><Button onClick={() => setRename(null)}>Cancel</Button><Button type="submit" variant="quiet">Save</Button></div></form>
    </dialog>
  </section></TabsApiContext.Provider>;
}
