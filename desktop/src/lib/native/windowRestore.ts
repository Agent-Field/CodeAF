// Relaunch restores the open windows and their places (IX Flows "Relaunch"). The main window keeps a short list of
// the windows that are open, {label, placeKey}, in the workspace store under the key 'windows' and rewrites it
// whenever a window opens, closes or changes place. On the next launch main reopens every other window on its place
// with windowOpen. The list is advice, never truth: anything that is not a window of ours, a place we know, or that
// repeats is dropped rather than failing the launch.
//
// Free of React. The store and the opener are injected so tests need no DOM and no native shell.
import { isPlaceKey } from './windows.ts';
import { MAIN_WINDOW, isWindowLabel } from './windowPlace.ts';

export type OpenWindow = { label: string; placeKey: string };

/** The one key the list lives under in the workspace store. */
export const WINDOWS_STORE_KEY = 'windows';

/** A sane ceiling, so a corrupted list can never open a hundred windows on launch. */
export const MAX_RESTORED_WINDOWS = 16;

export type WindowListStore = {
  read(key: string): unknown;
  write(key: string, value: unknown): void;
};

export type WindowRestoreDeps = {
  store: WindowListStore;
  /** Opens a place in a new window; resolves with its label when the shell reports one. */
  open(placeKey: string): Promise<string | undefined>;
};

/**
 * Cleans a stored or reported list. Keeps well-formed rows in order, drops a repeated label, and drops a row whose
 * place is not a place key. `main` is kept as a record of where it was; restore skips it because it is already open.
 */
export function reconcileWindows(raw: unknown): OpenWindow[] {
  if (!Array.isArray(raw)) return [];
  const seen = new Set<string>();
  const rows: OpenWindow[] = [];
  for (const item of raw) {
    const row = item as Partial<OpenWindow> | null;
    if (!row || !isWindowLabel(row.label) || !isPlaceKey(row.placeKey) || seen.has(row.label)) continue;
    seen.add(row.label);
    rows.push({ label: row.label, placeKey: row.placeKey });
  }
  return rows.slice(0, MAX_RESTORED_WINDOWS);
}

/** The places to reopen: every saved window except main, and none twice for a label that is already open. */
export function windowsToRestore(saved: unknown, alreadyOpen: readonly string[] = [MAIN_WINDOW]): OpenWindow[] {
  const open = new Set(alreadyOpen);
  return reconcileWindows(saved).filter((row) => !open.has(row.label));
}

/** Records the windows open right now, replacing the previous list. An empty list is stored, not skipped. */
export function recordWindows(store: WindowListStore, open: readonly OpenWindow[]): OpenWindow[] {
  const clean = reconcileWindows(open);
  store.write(WINDOWS_STORE_KEY, clean);
  return clean;
}

/**
 * Reopens the other windows on launch. Returns what was opened. One window failing to open does not stop the rest,
 * because a person who relaunches wants as much of their layout back as can be had.
 */
export async function restoreWindows(deps: WindowRestoreDeps): Promise<OpenWindow[]> {
  const wanted = windowsToRestore(deps.store.read(WINDOWS_STORE_KEY));
  const opened: OpenWindow[] = [];
  for (const row of wanted) {
    try {
      await deps.open(row.placeKey);
      opened.push(row);
    } catch { /* A window that will not open is left out of the layout. */ }
  }
  return opened;
}

/** The default store: the window's own local storage, which holds no secrets, only labels and place keys. */
export function localWindowListStore(storage: Pick<Storage, 'getItem' | 'setItem'> | undefined = safeLocal()): WindowListStore {
  const name = (key: string) => `codeaf.desktop.workspace.${key}`;
  return {
    read(key) {
      try {
        const text = storage?.getItem(name(key));
        return text ? JSON.parse(text) : undefined;
      } catch { return undefined; }
    },
    write(key, value) {
      try { storage?.setItem(name(key), JSON.stringify(value)); } catch { /* Forgetting the layout is not worth an interruption. */ }
    },
  };
}

function safeLocal(): Storage | undefined {
  try { return globalThis.localStorage; } catch { return undefined; }
}
