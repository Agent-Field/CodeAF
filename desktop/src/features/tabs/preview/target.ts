// Where a file, diff, terminal or web tab points. The kind lanes own these tabs; the preview reads one optional
// `target` on the pane, so a preview of those kinds fills in as soon as a lane sets it (and is title-only until then).
import type { Pane } from '../types';

export type PreviewTarget = {
  /** The engine session the file, diff or terminal belongs to. Falls back to the session the tab's summary came from. */
  sessionId?: string;
  /** Workspace-relative path of a file or diff tab. */
  path?: string;
  /** The terminal or job id of a terminal tab. */
  terminalId?: string;
  /** A web tab's address, and a screenshot of it when the browser surface has one. */
  url?: string;
  shot?: string;
};

export const targetOf = (pane: Pane): PreviewTarget => (pane as Pane & { target?: PreviewTarget }).target ?? {};

/** "pkg.go.dev/encoding/json" from a full address. */
export function addressOf(url: string): string {
  try {
    const parsed = new URL(url);
    return `${parsed.host}${parsed.pathname === '/' ? '' : parsed.pathname}`;
  } catch {
    return url;
  }
}
