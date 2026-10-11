// The Places shell controller: which place this window shows, how the person moves between places, and the one
// write path that turns every structural change into an undoable receipt. App owns one; the rail, the Go to
// chooser, every Home pane, the dialogs and the keys reach it through usePlacesShell().
//
// What is canonical stays with the engine: the graph, memberships, pins and the Open list are read from the Places
// routes, and every change is one of their writes. The window keeps only what is genuinely the window's: which place
// it shows, and which places the person closed in it (Places 10a "Closing ... takes it off the rail").

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { useToasts } from '../../../components/ui';
import { nativeControls, type NativeControls } from '../../../design/nativeControls';
import { PlacesError, type Mutation, type PlacesClient } from '../client';
import type { ChooserMode, WindowPlace } from './contracts';
import { indexPlaces, type PlaceIndex } from './selectors';
import { placesStore, usePlaces, type PlacesState } from './placesStore';
import { requestWorkspace } from './workspaceBus';
import { createPlaceNavigation } from '../navigation';
import { createPlaceUndo } from '../undo';
import { windowPlace, windowPlaceStorageKey, type WindowPlace as WindowBoot } from '../../../lib/native/windowPlace';
import { windowStructuralUndo } from '../../tabs/undo/structuralUndo';

export type DialogRequest =
  | { kind: 'instructions'; placeId: string }
  | { kind: 'sources'; placeId: string }
  | { kind: 'rename'; placeId: string }
  | { kind: 'create'; parent?: string; name?: string };

export type PlacesShell = {
  /** The place of the tab in front (`now` when it belongs to none). Places are libraries: this follows the active tab, it is not a workspace. */
  place: WindowPlace;
  /** The place Focus filters the strip to and tints the frame with; undefined when the strip shows every tab. Window-local. */
  focus?: string;
  setFocus: (id: string | undefined) => void;
  /** Focus on the place, or off when it already is. */
  toggleFocus: (id: string) => void;
  /** The workspace reports the place of its active tab. This is the only thing that moves `place`. */
  followTab: (place: string) => void;
  /** The place this window was opened on, until the strip has opened its Home (once, after its tabs have loaded). */
  boot?: { id: string; name: string };
  bootDone: () => void;
  places: PlacesState;
  index?: PlaceIndex;
  client: PlacesClient;
  native: NativeControls;
  /** Places closed in this window, with the instant they were closed (ISO). Going to one again clears it. */
  closed: ReadonlyMap<string, string>;
  /** 'now', 'root' (All places, a tab here) or a place id. Rejects with a sentence when the place cannot be gone to. */
  goTo: (id: string) => Promise<void>;
  goToInNewWindow: (id: string) => Promise<void>;
  openAllPlaces: (background?: boolean) => void;
  up: () => Promise<void>;
  closePlace: (id: string) => void;
  closeOpen: (except?: string) => void;
  chooser?: ChooserMode;
  openChooser: (mode: ChooserMode) => void;
  closeChooser: () => void;
  /** The place a Quick Look sheet is reading from outside a Home (the rail's menu). */
  lookAt?: string;
  quickLook: (id: string | undefined) => void;
  dialog?: DialogRequest;
  openDialog: (request: DialogRequest) => void;
  closeDialog: () => void;
  /**
   * The one write path. Runs `job` (one or more Places writes), refreshes the graph, and when the engine kept receipts
   * pushes an undo step and shows a toast with Undo. A refusal rejects with the engine's sentence; a write that stopped
   * half way keeps what it did undoable and still rejects.
   */
  write: (text: string, job: () => Promise<Mutation | Mutation[]>, options?: { subject?: string; durationMs?: number }) => Promise<Mutation[]>;
  undoLast: () => Promise<void>;
  canUndo: boolean;
  /** Shows a failure that has no page of its own to land on (the rail, a key, the window). */
  warn: (failure: unknown) => void;
  refresh: () => Promise<void>;
};

const PlacesShellContext = createContext<PlacesShell | undefined>(undefined);
export const PlacesShellProvider = PlacesShellContext.Provider;
/** The shell, or undefined outside one (a specimen). A part that needs it draws nothing without it. */
export const usePlacesShell = () => useContext(PlacesShellContext);

const closedKey = 'codeaf.desktop.places.closed';
export const isWindowPlace = (value: unknown): value is WindowPlace => value === 'now' || (typeof value === 'string' && /^pl_[0-9a-f]{16}$/.test(value));
const message = (failure: unknown) => (failure instanceof Error && failure.message ? failure.message : 'That did not work. Nothing was changed.');

function readClosed(): Map<string, string> {
  try {
    const saved = JSON.parse(localStorage.getItem(closedKey) ?? '{}') as Record<string, unknown>;
    return new Map(Object.entries(saved).filter((entry): entry is [string, string] => isWindowPlace(entry[0]) && typeof entry[1] === 'string'));
  } catch { return new Map(); }
}

/** App passes its boot before Workspace mounts; standalone shell callers use the same native boot door. */
export function usePlacesShellController(boot?: WindowBoot): PlacesShell {
  const places = usePlaces();
  const client = placesStore.client;
  const native = useMemo(() => nativeControls(), []);
  const [start] = useState(() => boot ?? windowPlace());
  const [place, setPlace] = useState<WindowPlace>(start.placeKey === 'root' ? 'now' : start.placeKey);
  const label = start.label;
  const [focus, setFocus] = useState<string>();
  const [closed, setClosed] = useState(readClosed);
  const [chooser, setChooser] = useState<ChooserMode>();
  const [dialog, setDialog] = useState<DialogRequest>();
  const [lookAt, setLookAt] = useState<string>();
  const placeUndo = useMemo(() => createPlaceUndo(client), [client]);
  const undoSize = useSyncExternalStore(windowStructuralUndo.subscribe, windowStructuralUndo.getSnapshot);
  const canUndo = undoSize > 0;
  const toasts = useToasts();
  const show = toasts.show;
  const index = useMemo(() => (places.graph ? indexPlaces(places.graph.places) : undefined), [places.graph]);

  // A window opened on All places shows an All places tab (Places 9e "All places doesn't travel with you"); one opened on
  // a place shows that place's Home as a tab, which the strip opens once its tabs have loaded (`boot`).
  useEffect(() => { if (start.placeKey === 'root') requestAnimationFrame(() => requestWorkspace({ type: 'home-root' })); }, [start.placeKey]);
  const [booted, setBooted] = useState(false);
  useEffect(() => { try { localStorage.setItem(windowPlaceStorageKey(label), place); } catch { /* Not remembering the place is not worth an interruption. */ } }, [label, place]);
  useEffect(() => { try { localStorage.setItem(closedKey, JSON.stringify(Object.fromEntries(closed))); } catch { /* As above. */ } }, [closed]);

  const warn = useCallback((failure: unknown) => { show({ text: message(failure), tone: 'warning', actions: [] }); }, [show]);

  const bootPlace = start.placeKey === 'root' || start.placeKey === 'now' || booted ? undefined : index?.byId.get(start.placeKey);
  const bootHome = useMemo(() => (bootPlace ? { id: bootPlace.id, name: bootPlace.name } : undefined), [bootPlace?.id, bootPlace?.name]);
  const bootDone = useCallback(() => setBooted(true), []);

  // A place that was deleted or archived elsewhere cannot stay in Focus: the window says so and shows every tab again.
  const current = place !== 'now' ? index?.byId.get(place) : undefined;
  const focused = focus ? index?.byId.get(focus) : undefined;
  useEffect(() => {
    if (!focus || places.status !== 'ready' || !index) return;
    if (focused && !focused.archived) return;
    setFocus(undefined);
    show({ text: focused ? `“${focused.name}” is archived, so Focus is off.` : 'That place no longer exists, so Focus is off.', tone: 'warning', actions: [] });
  }, [focus, places.status, index, focused, show]);

  // The native title says where the window is ("Marketing — codeaf").
  const title = place === 'now' ? 'Now' : current?.name ?? '';
  useEffect(() => { void native.setWindowTitle(title).catch(() => undefined); }, [native, title]);

  const refresh = useCallback(() => placesStore.refresh(), []);

  const remember = useCallback((text: string, receipts: string[], subject?: string, durationMs?: number) => {
    const entry = placeUndo.register({ receipts }, async () => {
      show({ text: `Undone: ${text}`, actions: [], durationMs: 3000 });
      await refresh();
    });
    if (entry) show({ text, subject, undo: entry.undo, durationMs });
  }, [show, placeUndo, refresh]);

  const write = useCallback<PlacesShell['write']>(async (text, job, options = {}) => {
    try {
      const result = await job();
      const list = Array.isArray(result) ? result : [result];
      remember(text, list.flatMap(mutation => mutation.undo), options.subject, options.durationMs);
      return list;
    } catch (failure) {
      if (failure instanceof PlacesError && failure.applied.length) remember(`Part of that was saved: ${text}`, failure.applied.map(receipt => receipt.id), options.subject);
      throw failure;
    } finally {
      void refresh();
    }
  }, [remember, refresh]);

  const undoLast = useCallback(async () => {
    await windowStructuralUndo.undoExternal();
  }, []);

  const openAllPlaces = useCallback((background = false) => {
    // All places is in no place, so a Focus would hide the tab that was just asked for.
    if (!background) setFocus(undefined);
    if (!requestWorkspace({ type: 'home-root', background })) show({ text: 'All places opens in the workspace. Go to the workspace first.', tone: 'warning', actions: [] });
  }, [show]);

  // Async navigation reads the latest window and graph rather than a render captured before an engine write.
  const navigationState = useRef({ place, focus, graph: places.graph });
  navigationState.current = { place, focus, graph: places.graph };
  const navigation = useMemo(() => createPlaceNavigation({
    client: { graph: client.graph, visit: (id, generation) => client.visit(id, generation), railOp: async operation => {
      const result = await client.railOp(operation);
      if (operation.op === 'close' && operation.place) {
        const id = operation.place;
        const name = navigationState.current.graph?.places.find(item => item.id === id)?.name;
        const text = name ? `Closed “${name}”` : 'Closed place';
        const entry = placeUndo.registerClose(async () => {
          await client.railOp({ op: 'visit', place: id });
          setClosed(before => { const next = new Map(before); next.delete(id); return next; });
          show({ text: `Undone: ${text}`, actions: [], durationMs: 3000 });
          void refresh();
        });
        show({ text, subject: name, undo: entry.undo });
      }
      return result;
    } }, windows: native,
    graph: () => navigationState.current.graph,
    current: () => navigationState.current.place,
    openHome: (key, title) => { requestWorkspace({ type: 'home-open', place: key, title }); },
    focused: () => navigationState.current.focus,
    setFocus,
    openRoot: openAllPlaces,
    setClosed: (key, isClosed) => setClosed(before => {
      if (key === 'now' || key === 'root') return before;
      const next = new Map(before);
      if (isClosed) next.set(key, new Date().toISOString()); else next.delete(key);
      return next;
    }),
    refresh, warn,
  }), [client, native, openAllPlaces, refresh, warn, placeUndo, show]);
  const goTo = useCallback(async (id: string) => { setChooser(undefined); await navigation.goTo(id); }, [navigation]);
  const goToInNewWindow = navigation.openInNewWindow;
  const closePlace = useCallback((id: string) => { void navigation.closePlace(id).catch(warn); }, [navigation, warn]);
  const closeOpen = useCallback((except?: string) => { void navigation.closeAllOthers(except).catch(warn); }, [navigation, warn]);

  const followTab = useCallback((key: string) => { if (isWindowPlace(key)) setPlace(key); }, []);
  const toggleFocus = useCallback((id: string) => setFocus(before => (before === id ? undefined : id)), []);

  return {
    place, focus, setFocus, toggleFocus, followTab, boot: bootHome, bootDone, places, index, client, native, closed,
    goTo, goToInNewWindow, openAllPlaces, closePlace, closeOpen, up: navigation.up,
    chooser, openChooser: setChooser, closeChooser: () => setChooser(undefined),
    lookAt, quickLook: setLookAt,
    dialog, openDialog: setDialog, closeDialog: () => setDialog(undefined),
    write, undoLast, canUndo,
    warn, refresh,
  };
}
