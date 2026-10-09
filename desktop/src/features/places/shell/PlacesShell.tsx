// The Places shell controller: which place this window shows, how the person moves between places, and the one
// write path that turns every structural change into an undoable receipt. App owns one; the rail, the Go to
// chooser, every Home pane, the dialogs and the keys reach it through usePlacesShell().
//
// What is canonical stays with the engine: the graph, memberships, pins and the Open list are read from the Places
// routes, and every change is one of their writes. The window keeps only what is genuinely the window's: which place
// it shows, and which places the person closed in it (Places 10a "Closing ... takes it off the rail").

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useToasts, type ToastModel } from '../../../components/ui';
import { nativeControls, placeFromSearch, type NativeControls } from '../../../design/nativeControls';
import { PlacesError, type Mutation, type PlacesClient } from '../client';
import type { ChooserMode, UndoEntry, WindowPlace } from './contracts';
import { indexPlaces, type PlaceIndex } from './selectors';
import { placesStore, usePlaces, type PlacesState } from './placesStore';
import { requestWorkspace } from './workspaceBus';

export type DialogRequest =
  | { kind: 'instructions'; placeId: string }
  | { kind: 'sources'; placeId: string }
  | { kind: 'rename'; placeId: string }
  | { kind: 'create'; parent?: string; name?: string };

export type PlacesShell = {
  /** The place this window shows. */
  place: WindowPlace;
  places: PlacesState;
  index?: PlaceIndex;
  client: PlacesClient;
  native: NativeControls;
  /** Moves on every Go to, so the strip focuses its Home even when the place did not change (Places 8g "Clicking a place"). */
  arrival: number;
  /** Places closed in this window, with the instant they were closed (ISO). Going to one again clears it. */
  closed: ReadonlyMap<string, string>;
  /** 'now', 'root' (All places, a tab here) or a place id. Rejects with a sentence when the place cannot be gone to. */
  goTo: (id: string) => Promise<void>;
  goToInNewWindow: (id: string) => Promise<void>;
  openAllPlaces: (background?: boolean) => void;
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
  toasts: readonly ToastModel[];
  dismissToast: (id: string) => void;
  /** Shows a failure that has no page of its own to land on (the rail, a key, the window). */
  warn: (failure: unknown) => void;
  refresh: () => Promise<void>;
};

const PlacesShellContext = createContext<PlacesShell | undefined>(undefined);
export const PlacesShellProvider = PlacesShellContext.Provider;
/** The shell, or undefined outside one (a specimen). A part that needs it draws nothing without it. */
export const usePlacesShell = () => useContext(PlacesShellContext);

const UNDO_LIMIT = 20;
const closedKey = 'codeaf.desktop.places.closed';
const windowPlaceKey = (label: string) => `codeaf.desktop.window.${label}.place`;
export const isWindowPlace = (value: unknown): value is WindowPlace => value === 'now' || (typeof value === 'string' && /^pl_[0-9a-f]{16}$/.test(value));
const message = (failure: unknown) => (failure instanceof Error && failure.message ? failure.message : 'That did not work. Nothing was changed.');

function readClosed(): Map<string, string> {
  try {
    const saved = JSON.parse(localStorage.getItem(closedKey) ?? '{}') as Record<string, unknown>;
    return new Map(Object.entries(saved).filter((entry): entry is [string, string] => isWindowPlace(entry[0]) && typeof entry[1] === 'string'));
  } catch { return new Map(); }
}

/** Where this window starts: the address a new window was opened with, else the place this window last showed. */
function startingPlace(): { place: WindowPlace; allPlaces: boolean } {
  const asked = new URLSearchParams(location.search).get('place');
  if (asked) {
    const key = placeFromSearch(location.search);
    return key === 'root' ? { place: 'now', allPlaces: true } : { place: key as WindowPlace, allPlaces: false };
  }
  try {
    const saved = localStorage.getItem(windowPlaceKey('main'));
    if (isWindowPlace(saved)) return { place: saved, allPlaces: false };
  } catch { /* An unavailable store starts in Now. */ }
  return { place: 'now', allPlaces: false };
}

export function usePlacesShellController(): PlacesShell {
  const places = usePlaces();
  const client = placesStore.client;
  const native = useMemo(() => nativeControls(), []);
  const [start] = useState(startingPlace);
  const [place, setPlace] = useState<WindowPlace>(start.place);
  const [label, setLabel] = useState('main');
  const [arrival, setArrival] = useState(0);
  const [closed, setClosed] = useState(readClosed);
  const [chooser, setChooser] = useState<ChooserMode>();
  const [dialog, setDialog] = useState<DialogRequest>();
  const [lookAt, setLookAt] = useState<string>();
  const undo = useRef<UndoEntry[]>([]);
  const [canUndo, setCanUndo] = useState(false);
  const toasts = useToasts();
  const show = toasts.show;
  const index = useMemo(() => (places.graph ? indexPlaces(places.graph.places) : undefined), [places.graph]);

  // A window opened on All places shows Now with an All places tab (Places 9e "All places doesn't travel with you").
  useEffect(() => { if (start.allPlaces) requestAnimationFrame(() => requestWorkspace({ type: 'home-root' })); }, [start.allPlaces]);
  useEffect(() => { void native.currentWindow().then(context => setLabel(context.label)).catch(() => undefined); }, [native]);
  useEffect(() => { try { localStorage.setItem(windowPlaceKey(label), place); } catch { /* Not remembering the place is not worth an interruption. */ } }, [label, place]);
  useEffect(() => { try { localStorage.setItem(closedKey, JSON.stringify(Object.fromEntries(closed))); } catch { /* As above. */ } }, [closed]);

  const warn = useCallback((failure: unknown) => { show({ text: message(failure), tone: 'warning', actions: [] }); }, [show]);

  // A place that was deleted or archived elsewhere cannot be shown: the window says so and goes to Now.
  const current = place !== 'now' ? index?.byId.get(place) : undefined;
  useEffect(() => {
    if (place === 'now' || places.status !== 'ready' || !index) return;
    if (current && !current.archived) return;
    setPlace('now');
    show({ text: current ? `“${current.name}” is archived, so this window shows Now.` : 'That place no longer exists, so this window shows Now.', tone: 'warning', actions: [] });
  }, [place, places.status, index, current, show]);

  // The native title says where the window is ("Marketing — codeaf").
  const title = place === 'now' ? 'Now' : current?.name ?? '';
  useEffect(() => { void native.setWindowTitle(title).catch(() => undefined); }, [native, title]);

  const refresh = useCallback(() => placesStore.refresh(), []);

  const undoEntry = useCallback(async (entry: UndoEntry) => {
    try {
      await client.undo([...entry.receipts]);
      undo.current = undo.current.filter(other => other.id !== entry.id);
      setCanUndo(undo.current.length > 0);
      show({ text: `Undone: ${entry.text}`, actions: [], durationMs: 3000 });
    } catch (failure) {
      // A graph that moved on since cannot be taken back; the step leaves the stack with the engine's reason.
      if (failure instanceof PlacesError && failure.code === 'cannot_undo') { undo.current = undo.current.filter(other => other.id !== entry.id); setCanUndo(undo.current.length > 0); }
      throw failure;
    } finally {
      void refresh();
    }
  }, [client, refresh, show]);

  const remember = useCallback((text: string, receipts: string[], subject?: string, durationMs?: number) => {
    if (!receipts.length) return;
    const entry: UndoEntry = { id: crypto.randomUUID(), text, subject, receipts };
    undo.current = [...undo.current.slice(-(UNDO_LIMIT - 1)), entry];
    setCanUndo(true);
    show({ text, subject, actions: [{ label: 'Undo', primary: true, onSelect: () => undoEntry(entry) }], durationMs });
  }, [show, undoEntry]);

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
    const entry = undo.current[undo.current.length - 1];
    if (!entry) { show({ text: 'Nothing to undo.', actions: [], durationMs: 3000 }); return; }
    await undoEntry(entry);
  }, [show, undoEntry]);

  const openAllPlaces = useCallback((background = false) => {
    if (!requestWorkspace({ type: 'home-root', background })) show({ text: 'All places opens in the workspace. Go to the workspace first.', tone: 'warning', actions: [] });
  }, [show]);

  const goTo = useCallback(async (id: string) => {
    setChooser(undefined);
    if (id === 'root') { openAllPlaces(); return; }
    if (id === 'now') { setPlace('now'); setArrival(n => n + 1); return; }
    if (!isWindowPlace(id)) throw new Error('That is not a place codeaf knows.');
    const target = index?.byId.get(id);
    if (index && !target) throw new Error('That place no longer exists.');
    if (target?.archived) throw new Error(`“${target.name}” is archived. Restore it from All places first.`);
    setPlace(id);
    setArrival(n => n + 1);
    setClosed(before => { if (!before.has(id)) return before; const next = new Map(before); next.delete(id); return next; });
    // Going there is what puts a place in the rail's Open list; the engine records it and the next read shows it.
    try { await client.visit(id); } catch (failure) { warn(failure); }
    void refresh();
  }, [client, index, openAllPlaces, refresh, warn]);

  const goToInNewWindow = useCallback(async (id: string) => {
    if (id !== 'now' && id !== 'root' && !isWindowPlace(id)) throw new Error('That is not a place codeaf knows.');
    await native.openPlaceWindow(id as 'now' | 'root' | `pl_${string}`);
    if (isWindowPlace(id) && id !== 'now') { try { await client.visit(id); } catch (failure) { warn(failure); } void refresh(); }
  }, [client, native, refresh, warn]);

  const closePlace = useCallback((id: string) => {
    setClosed(before => new Map(before).set(id, new Date().toISOString()));
    // Closing the place this window shows leaves the window in Now; its tabs are kept for when it is opened again.
    if (id === place) { setPlace('now'); setArrival(n => n + 1); }
  }, [place]);

  const closeOpen = useCallback((except?: string) => {
    const open = places.graph?.rail.open.filter(view => view.id !== except && !view.pinned) ?? [];
    if (!open.length) return;
    const at = new Date().toISOString();
    setClosed(before => { const next = new Map(before); for (const view of open) next.set(view.id, at); return next; });
    if (place !== 'now' && place !== except && open.some(view => view.id === place)) { setPlace('now'); setArrival(n => n + 1); }
  }, [places.graph, place]);

  return {
    place, places, index, client, native, arrival, closed,
    goTo, goToInNewWindow, openAllPlaces, closePlace, closeOpen,
    chooser, openChooser: setChooser, closeChooser: () => setChooser(undefined),
    lookAt, quickLook: setLookAt,
    dialog, openDialog: setDialog, closeDialog: () => setDialog(undefined),
    write, undoLast, canUndo,
    toasts: toasts.toasts, dismissToast: toasts.dismiss, warn, refresh,
  };
}
