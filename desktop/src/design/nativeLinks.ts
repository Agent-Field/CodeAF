// The renderer's one door to codeaf links that arrive from OUTSIDE the app: a `codeaf://` address opened from another
// program, the browser or a terminal. Rust (src-tauri/src/links.rs) receives it from the operating system, refuses
// anything that is not a codeaf link, queues it for one codeaf window and brings that window forward; this adapter
// claims what was queued for THIS window. The renderer holds no deep-link plugin permission of its own.
//
// Kept free of React. The bridge is injectable for tests; outside the desktop app nothing can arrive, so `claim`
// answers an empty list and `onReady` listens to nothing, rather than pretending.
import { invoke as tauriInvoke, isTauri } from '@tauri-apps/api/core';
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow';

/** The event Rust emits to the one window a link was queued for. */
export const LINK_READY_EVENT = 'deep-link://ready';

export type LinksBridge = {
  desktop: boolean;
  invoke<T>(command: string, args?: Record<string, unknown>): Promise<T>;
  /** Listens for events addressed to THIS window only. */
  listen(event: string, handler: () => void): Promise<() => void>;
};

function defaultBridge(): LinksBridge {
  return {
    desktop: isTauri(),
    invoke: (command, args) => tauriInvoke(command, args),
    // A global `listen` would also hear events emitted to other windows; a link belongs to the window it was queued for.
    listen: async (event, handler) => getCurrentWebviewWindow().listen(event, () => handler()),
  };
}

export type NativeLinks = ReturnType<typeof createNativeLinks>;

export function createNativeLinks(bridge: LinksBridge = defaultBridge()) {
  return {
    desktop: bridge.desktop,
    /** Every link queued for this window, oldest first, each handed over once. Rust has already refused non-links. */
    async claim(): Promise<string[]> {
      if (!bridge.desktop) return [];
      const links = await bridge.invoke<unknown>('link_claim');
      return Array.isArray(links) ? links.filter((link): link is string => typeof link === 'string') : [];
    },
    /** Calls back whenever a link has been queued for this window. */
    onReady(callback: () => void): Promise<() => void> {
      if (!bridge.desktop) return Promise.resolve(() => undefined);
      return bridge.listen(LINK_READY_EVENT, callback);
    },
  };
}

let shared: NativeLinks | undefined;
/** The one adapter for this window. */
export const nativeLinks = (): NativeLinks => (shared ??= createNativeLinks());
