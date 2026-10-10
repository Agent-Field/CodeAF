// The renderer's one door to native windows: open a place in a new window, bring one forward, list them, move a tab
// out of the strip, name the window, and hear which tab a freshly opened window should show. A window is a view of a
// place, so every call speaks in place keys. Rust (src-tauri/src/windows.rs, tabmove.rs) owns the truth and refuses
// anything malformed; the checks here mirror it so a bad key fails before the bridge instead of after a round trip.
//
// Kept free of React. The bridge is injectable for tests. Outside the desktop app there are no native windows:
// opening a place becomes a browser tab carrying ?place= (dev parity), and everything else does nothing.
import { invoke as tauriInvoke, isTauri } from '@tauri-apps/api/core';
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow';

/** Rust emits this to the one window a moved tab was opened for. */
export const FOCUS_TAB_EVENT = 'window://focus-tab';

/** The place a window shows when its URL names none. */
export const DEFAULT_PLACE = 'now';

export type WindowRow = { label: string; placeKey: string; focused: boolean; title: string };
export type Point = { x: number; y: number };

export type WindowsBridge = {
  desktop: boolean;
  invoke<T>(command: string, args?: Record<string, unknown>): Promise<T>;
  /** Listens for events addressed to THIS window only. */
  listen(event: string, handler: (payload: unknown) => void): Promise<() => void>;
  /** The current URL's query string, including the leading "?". */
  search(): string;
  /** Opens a browser tab; the dev-parity stand-in for a native window. */
  openBrowserTab(url: string): void;
};

function defaultBridge(): WindowsBridge {
  const inBrowser = typeof window !== 'undefined';
  return {
    desktop: isTauri(),
    invoke: (command, args) => tauriInvoke(command, args),
    // A global `listen` would also hear events emitted to other windows; the focus belongs to the window it opened.
    listen: async (event, handler) => getCurrentWebviewWindow().listen(event, (e) => handler(e.payload)),
    search: () => (inBrowser ? window.location.search : ''),
    openBrowserTab: (url) => {
      if (inBrowser) window.open(url, '_blank');
    },
  };
}

/** Mirrors `checked_place` in windows.rs: Now, or `p-` and twelve lowercase hex digits. */
export function isPlaceKey(key: unknown): key is string {
  return typeof key === 'string' && (key === DEFAULT_PLACE || /^p-[0-9a-f]{12}$/.test(key));
}

/** Mirrors `checked_focus_tab`: one to 128 characters of letters, digits, "-" and "_". */
export function isTabId(id: unknown): id is string {
  return typeof id === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(id);
}

/** Mirrors `Point::finite`: a drop point must be a real position on a screen. */
function isPoint(point: Point): boolean {
  return Number.isFinite(point.x) && Number.isFinite(point.y) && Math.abs(point.x) < 1e6 && Math.abs(point.y) < 1e6;
}

function checkedPlace(key: string): string {
  if (!isPlaceKey(key)) throw new Error('That is not a place codeaf knows');
  return key;
}

function checkedTab(id: string): string {
  if (!isTabId(id)) throw new Error('That tab cannot be focused');
  return id;
}

/** True where ⌘N is not a menu accelerator; macOS gets New Window from the native menu instead. */
export function needsKeyboardNewWindow(platform: string): boolean {
  return !/mac|iphone|ipad/i.test(platform);
}

/** The subset of a keydown the helper reads, so tests need no DOM. */
export type NewWindowKey = { key: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean };

export type NativeWindows = ReturnType<typeof createNativeWindows>;

/**
 * True when this process can open another native window. The desktop app reports that; a browser does not,
 * so a menu that asks here leaves "Move to new window" off instead of drawing it disabled.
 */
export function reportsMultiwindow(windows: Pick<NativeWindows, 'desktop'> = createNativeWindows()): boolean {
  return windows.desktop;
}

/**
 * "Move to new window" (Shell 3g). Opens this tab's place in another window and tells only that window to show
 * the tab. The menu passes no drop point, so the window cascades. A browser, a tab id or a place the native
 * door does not accept opens nothing: the caller says so, and Now is not used as a stand-in for another place.
 */
export async function tabMoveToWindow(tab: { id: string }, placeKey?: string, windows: NativeWindows = createNativeWindows()): Promise<boolean> {
  if (!windows.desktop || !isTabId(tab.id)) return false;
  const place = placeKey === undefined ? windows.currentPlaceKey() : (isPlaceKey(placeKey) ? placeKey : undefined);
  if (!place) return false;
  await windows.moveTabToNewWindow(tab.id, place);
  return true;
}

export function createNativeWindows(bridge: WindowsBridge = defaultBridge()) {
  const api = {
    desktop: bridge.desktop,

    /** The place this window shows, from ?place=; anything that is not a place key is Now. */
    currentPlaceKey(): string {
      const value = new URLSearchParams(bridge.search()).get('place');
      return isPlaceKey(value) ? value : DEFAULT_PLACE;
    },

    /** Opens a place in a new window, optionally focused on one of its tabs. Resolves with the new window's label. */
    async openPlaceWindow(placeKey: string, focusTab?: string): Promise<string | undefined> {
      checkedPlace(placeKey);
      if (focusTab !== undefined) checkedTab(focusTab);
      if (!bridge.desktop) {
        const query = new URLSearchParams({ place: placeKey });
        bridge.openBrowserTab(`?${query.toString()}`);
        return undefined;
      }
      const request: Record<string, unknown> = { placeKey };
      if (focusTab !== undefined) request.focusTab = focusTab;
      const opened = await bridge.invoke<unknown>('window_open', { request });
      return labelOf(opened);
    },

    /** Brings a window forward by label. */
    async focusWindow(label: string): Promise<void> {
      if (!bridge.desktop) return;
      await bridge.invoke('window_focus', { label });
    },

    /** Every open window, or an empty list outside the desktop app. */
    async listWindows(): Promise<WindowRow[]> {
      if (!bridge.desktop) return [];
      const rows = await bridge.invoke<unknown>('window_list');
      return Array.isArray(rows) ? rows.filter(isRow) : [];
    },

    /** Opens the tab's place in a new window focused on that tab; a drag supplies `at`, the menu lets it cascade. */
    async moveTabToNewWindow(tabId: string, placeKey: string, at?: Point): Promise<string | undefined> {
      checkedTab(tabId);
      checkedPlace(placeKey);
      if (at && !isPoint(at)) throw new Error('That position is outside the screen');
      if (!bridge.desktop) return undefined;
      const request: Record<string, unknown> = { tabId, placeKey };
      if (at) request.at = { x: at.x, y: at.y };
      const label = await bridge.invoke<unknown>('tab_move_to_window', { request });
      return typeof label === 'string' ? label : undefined;
    },

    /** Names this window after its place. Rust adds the " — codeaf" suffix; no place name leaves plain "codeaf". */
    async setWindowTitle(placeName?: string): Promise<void> {
      if (!bridge.desktop) return;
      await bridge.invoke('window_set_title', { title: placeName ?? '' });
    },

    /** Hears which tab this window was opened to show. Resolves with the unlisten function. */
    async onFocusTab(callback: (tabId: string) => void): Promise<() => void> {
      if (!bridge.desktop) return () => {};
      return bridge.listen(FOCUS_TAB_EVENT, (payload) => {
        const tabId = (payload as { tabId?: unknown } | null)?.tabId;
        if (isTabId(tabId)) callback(tabId);
      });
    },

    /**
     * Handles ⌘N / Ctrl+N where the menu does not. Returns true when the key was consumed. `platform` is
     * navigator.platform; on macOS this does nothing because the menu accelerator already opened Now in Rust.
     */
    handleNewWindowKey(event: NewWindowKey, platform: string): boolean {
      if (!needsKeyboardNewWindow(platform)) return false;
      if (event.key.toLowerCase() !== 'n' || !event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) {
        return false;
      }
      void api.openPlaceWindow(DEFAULT_PLACE).catch(() => {});
      return true;
    },
  };
  return api;
}

function labelOf(opened: unknown): string | undefined {
  if (typeof opened === 'string') return opened;
  const label = (opened as { label?: unknown } | null)?.label;
  return typeof label === 'string' ? label : undefined;
}

function isRow(row: unknown): row is WindowRow {
  const r = row as Partial<WindowRow> | null;
  return !!r && typeof r.label === 'string' && typeof r.placeKey === 'string' && typeof r.focused === 'boolean' && typeof r.title === 'string';
}
