// Window boot: which window this is and which place it opens on, decided once before the workspace mounts so the
// workspace is loaded under the right key the first time (IX Flows "Multiple windows"). A window is a view of a place:
// a new window (`w-*`) opens on the place its address names, else Now; the main window opens on the place it last
// showed. The pieces the shell already owns (key shapes, the saved-place storage key) are reused, not restated.
//
// Free of React. Every outside fact (label, address, storage, document) is injectable so tests need no DOM.
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow';
import { isTauri } from '@tauri-apps/api/core';
import { isPlaceKey, type PlaceKey } from '../../design/nativeControls.ts';

export type WindowPlace = { label: string; placeKey: PlaceKey };

export type WindowPlaceDeps = {
  /** This window's label; a browser has one window, `main`. */
  label(): string;
  /** The current address's query string, including the leading "?". */
  search(): string;
  /** The place the given window last showed, if it was remembered. */
  saved(label: string): string | null;
  remember(label: string, placeKey: string): void;
  setDocumentTitle(title: string): void;
};

export const MAIN_WINDOW = 'main';
const NOW: PlaceKey = 'now';

/** Same key the shell writes whenever a window changes place (PlacesShell.tsx), so the two never disagree. */
export const windowPlaceStorageKey = (label: string) => `codeaf.desktop.window.${label}.place`;

function defaultDeps(): WindowPlaceDeps {
  const inBrowser = typeof window !== 'undefined';
  return {
    label: () => (isTauri() ? getCurrentWebviewWindow().label : MAIN_WINDOW),
    search: () => (inBrowser ? window.location.search : ''),
    saved: (label) => {
      try { return localStorage.getItem(windowPlaceStorageKey(label)); } catch { return null; }
    },
    remember: (label, placeKey) => {
      try { localStorage.setItem(windowPlaceStorageKey(label), placeKey); } catch { /* Forgetting a place is not worth an interruption. */ }
    },
    setDocumentTitle: (title) => { if (typeof document !== 'undefined') document.title = title; },
  };
}

/** True for the labels Rust gives windows: `main` or `w-` and digits. Anything else is not one of ours. */
export function isWindowLabel(label: unknown): label is string {
  return typeof label === 'string' && (label === MAIN_WINDOW || /^w-[0-9]+$/.test(label));
}

/**
 * This window's label and the place it opens on. An address that names a place wins; a new window with none opens on
 * Now; the main window returns to the place it last showed. A remembered or addressed value that is no longer a place
 * key falls back to Now rather than failing the boot.
 */
export function windowPlace(deps: WindowPlaceDeps = defaultDeps()): WindowPlace {
  const label = deps.label();
  const asked = new URLSearchParams(deps.search()).get('place');
  if (isPlaceKey(asked)) return { label, placeKey: asked };
  if (label === MAIN_WINDOW) {
    const saved = deps.saved(label);
    if (isPlaceKey(saved)) return { label, placeKey: saved };
  }
  return { label, placeKey: NOW };
}

/** Moves this window to a place from the rail: remembers it for the next boot and names the window after it. */
export function switchPlace(placeKey: PlaceKey, placeName: string, deps: WindowPlaceDeps = defaultDeps()): WindowPlace {
  if (!isPlaceKey(placeKey)) throw new Error('That is not a place codeaf knows');
  const label = deps.label();
  deps.remember(label, placeKey);
  deps.setDocumentTitle(placeName);
  return { label, placeKey };
}

/**
 * Whether a window-addressed event is for this window. An event with no target is a broadcast and every window takes
 * it; a targeted one belongs to that label alone, so a tab action never lands in two windows.
 */
export function eventIsForWindow(target: unknown, label: string): boolean {
  return target === undefined || target === null || target === label;
}
