// The desktop's door to "open this file in that editor".
//
// The id comes from the engine's editors route. Rust asks that route again
// and refuses an id it did not just list, so this function never sends a
// command line. A browser build has no opener: the value stays undefined and
// Open in does not offer a chosen editor.

import { invoke, isTauri } from '@tauri-apps/api/core';

export type OpenWith = (path: string, editorId: string, session: string) => Promise<void>;

export type EditorsBridge = {
  desktop: boolean;
  invoke(command: string, args: { path: string; editorId: string; session: string }): Promise<void>;
};

/**
 * The opener, or undefined when this build cannot start a program. Callers
 * leave the chosen-editor item out when this is undefined. `path` is the
 * absolute path on this machine. `session` is the conversation whose editors
 * route listed `editorId`.
 */
export function createOpenWith(bridge: EditorsBridge): OpenWith | undefined {
  if (!bridge.desktop) return undefined;
  return (path, editorId, session) => bridge.invoke('open_with', { path, editorId, session });
}

/** Undefined in a browser, including unit tests that are not inside the desktop app. */
export const openWith: OpenWith | undefined = createOpenWith({
  desktop: isTauri(),
  invoke: (command, args) => invoke<void>(command, args),
});
