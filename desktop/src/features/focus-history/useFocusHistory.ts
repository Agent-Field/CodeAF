// The wire between the window and the focus-history stack (model.ts). The controller is plain TypeScript so a node
// test can drive it with a fake clock-free store; the hook below is only the React seam. It subscribes to where
// focus is (place, tab, drill path) and never edits the tab reducers: restoring a step is a callback the shell
// supplies, so this module does not know how a place switch or a tab select is dispatched.
//
// I2.4: tab changes, place switches and drill-ins are steps; a background open and the window putting its
// saved tabs back on relaunch are not. I2.5: the back chip shows only when the step we are on was not self-started.
import { createContext, createElement, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import {
  backFocus, canBackFocus, canForwardFocus, currentFocus, forwardFocus, recordFocus, restoreFocusHistory,
  type FocusCause, type FocusEntry, type FocusHistory, type FocusMove, type FocusScrollSpot,
} from './model.ts';

/** Where focus is right now, as the workspace sees it. Scroll and draft are optional: unknown restores nothing. */
export type FocusPosition = {
  windowPlace: string;
  tabId: string;
  drillPath?: readonly string[];
  scroll?: readonly FocusScrollSpot[];
  draftKey?: string | null;
};

/** What a component can read: the cursor's capabilities and the chip's step. Rebuilt only when one of them changes. */
export type FocusSnapshot = {
  history: FocusHistory;
  canBack: boolean;
  canForward: boolean;
  /** The step to go back to while the current step was not self-started; undefined once the person acts. */
  chip: FocusEntry | undefined;
};

export type FocusWireOptions = {
  /** Reads the saved stack for this window. Anything that is not a stack is no history. */
  load: () => unknown;
  save: (history: FocusHistory) => void;
  /** Puts focus back on an entry: switch the place if needed, select the tab, open the drill path. */
  navigate: (entry: FocusEntry) => void;
};

export type FocusWire = ReturnType<typeof createFocusWire>;

const samePath = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((step, index) => step === b[index]);
const sameSpot = (entry: FocusEntry, position: FocusPosition) =>
  entry.windowPlace === position.windowPlace && entry.tabId === position.tabId && samePath(entry.drillPath, position.drillPath ?? []);

export function createFocusWire({ load, save, navigate }: FocusWireOptions) {
  let history = restoreFocusHistory(load());
  /** The step a Back or Forward is travelling to. Positions on the way there are not the person's moves. */
  let pending: FocusEntry | undefined;
  let last: FocusPosition | undefined;
  let cause: FocusCause = 'own';
  let chipDismissed = false;
  /** Tab ids each place reported this load. A place not reported yet counts every tab of its entries as open. */
  const tabsByPlace = new Map<string, ReadonlySet<string>>();
  let snapshot: FocusSnapshot | undefined;
  const listeners = new Set<() => void>();

  const aliveTabs = (): Set<string> => {
    const alive = new Set<string>();
    for (const ids of tabsByPlace.values()) ids.forEach(id => alive.add(id));
    for (const entry of history.entries) if (!tabsByPlace.has(entry.windowPlace)) alive.add(entry.tabId);
    return alive;
  };

  const commit = (next: FocusHistory) => {
    if (next === history) return;
    history = next;
    chipDismissed = false;
    save(history);
    changed();
  };

  const changed = () => { snapshot = undefined; listeners.forEach(listener => listener()); };

  const build = (): FocusSnapshot => {
    const alive = aliveTabs();
    const here = currentFocus(history);
    const before = history.cursor > 0 ? history.entries[history.cursor - 1] : undefined;
    return {
      history,
      canBack: canBackFocus(history, alive),
      canForward: canForwardFocus(history, alive),
      chip: here && !here.selfStarted && !chipDismissed ? before : undefined,
    };
  };

  const travel = (next: FocusHistory) => {
    if (next === history) return false;
    const target = currentFocus(next);
    if (!target) return false;
    pending = target;
    history = next;
    save(history);
    changed();
    navigate(target);
    return true;
  };

  return {
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    getSnapshot(): FocusSnapshot { return snapshot ??= build(); },

    /** The next observed move was not the person's own (Next up, a notification, a cross-place link). Consumed by that move. */
    markCause(next: FocusCause) { cause = next; },

    /** Which tabs are open in a place. A closed tab's steps stay in the stack and are skipped. */
    setTabs(place: string, ids: readonly string[]) {
      tabsByPlace.set(place, new Set(ids));
      changed();
    },

    /**
     * Reports where focus is. A repeat of the current step refreshes its scroll and draft; a different place, tab or
     * drill path is a step. The first report of a load that finds saved history is the relaunch restore, not a step.
     */
    observe(position: FocusPosition) {
      const before = last;
      last = position;
      if (pending) {
        // On the way to a restored step the shell may pass through a place's own saved tab, or the old place, first.
        // Only the first report after a place change is that passage; any later mismatch is the person moving on.
        const passing = !before || before.windowPlace !== position.windowPlace || position.windowPlace !== pending.windowPlace;
        if (sameSpot(pending, position)) pending = undefined;
        else if (passing) return;
        else pending = undefined;
      }
      // An empty window has no saved steps to put back, so its first position seeds the stack as an ordinary step.
      const reason: FocusMove['reason'] = !before ? (history.entries.length > 0 ? 'relaunch' : 'tab')
        : before.windowPlace !== position.windowPlace ? 'place'
        : before.tabId !== position.tabId ? 'tab'
        : 'drill';
      commit(recordFocus(history, { ...position, reason, cause }));
      cause = 'own';
    },

    /** A tab opened beside the current one never took focus, so nothing is recorded. */
    observeBackground(_position: FocusPosition) { /* Deliberately empty: background opens are not steps (I2.4). */ },

    back: () => travel(backFocus(history, aliveTabs())),
    forward: () => travel(forwardFocus(history, aliveTabs())),
    dismissChip() { if (!chipDismissed) { chipDismissed = true; changed(); } },
    /** Replaces the stack, as a relaunch does. Re-applies the last reported position so a late load keeps the live step. */
    hydrate(saved: unknown) {
      history = restoreFocusHistory(saved);
      if (last) history = recordFocus(history, { ...last, reason: history.entries.length ? 'relaunch' : 'tab', cause: 'own' });
      changed();
    },
  };
}

const FocusHistoryContext = createContext<FocusWire | null>(null);

/** The one provider the shell mounts. The shell supplies the stack's storage and how a step is restored. */
export function FocusHistoryProvider({ wire, children }: { wire: FocusWire; children: ReactNode }) {
  return createElement(FocusHistoryContext.Provider, { value: wire }, children);
}

/** Standalone specimens retain local routing until the shell supplies window history. */
export function useOptionalFocusWire(): FocusWire | null {
  return useContext(FocusHistoryContext);
}

/** The window's wire, for components that restore (the back chip, the shortcut) or report. */
export function useFocusWire(): FocusWire {
  const wire = useContext(FocusHistoryContext);
  if (!wire) throw new Error('useFocusWire needs a FocusHistoryProvider');
  return wire;
}

/** The snapshot of the wire, re-rendering only when the cursor, its reach or the chip changed. */
export function useFocusHistory(): FocusSnapshot & { back: () => boolean; forward: () => boolean; dismissChip: () => void } {
  const wire = useFocusWire();
  const snapshot = useSyncExternalStore(wire.subscribe, wire.getSnapshot);
  return { ...snapshot, back: wire.back, forward: wire.forward, dismissChip: wire.dismissChip };
}

/**
 * Reports a workspace's focus to the wire: its tab ids always, and its position whenever place, tab or drill path
 * change. A render that changes nothing reports nothing.
 */
export function useFocusObserver(position: FocusPosition | undefined, tabIds: readonly string[]) {
  const wire = useFocusWire();
  const place = position?.windowPlace;
  const idsKey = tabIds.join('\u0000');
  useEffect(() => { if (place) wire.setTabs(place, tabIds); }, [wire, place, idsKey]);
  const pathKey = (position?.drillPath ?? []).join('\u0000');
  useEffect(() => { if (position) wire.observe(position); }, [wire, position?.windowPlace, position?.tabId, pathKey]);
}

/** Builds the window's wire once, over any storage that offers getItem and setItem. */
export function useFocusWireFor(storageKey: string | undefined, navigate: (entry: FocusEntry) => void, storage: Pick<Storage, 'getItem' | 'setItem'> | undefined = typeof localStorage === 'undefined' ? undefined : localStorage): FocusWire {
  const latest = useRef(navigate);
  latest.current = navigate;
  const keyRef = useRef(storageKey);
  keyRef.current = storageKey;
  const [wire] = useState(() => createFocusWire({
    load: () => { try { return keyRef.current && storage ? JSON.parse(storage.getItem(keyRef.current) ?? 'null') : null; } catch { return null; } },
    save: history => { try { if (keyRef.current && storage) storage.setItem(keyRef.current, JSON.stringify(history)); } catch { /* A full or blocked store only costs the relaunch restore. */ } },
    navigate: entry => latest.current(entry),
  }));
  // A native window learns its label after first paint; the key then arrives and the saved stack is installed.
  useEffect(() => {
    if (!storageKey || !storage) return;
    try { wire.hydrate(JSON.parse(storage.getItem(storageKey) ?? 'null')); } catch { wire.hydrate(null); }
  }, [wire, storageKey]);
  return useMemo(() => wire, [wire]);
}
