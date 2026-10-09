// Where a file, diff, terminal or web tab points. The kind lanes own these tabs and write one optional `target`
// on the pane (typed and validated in view-state.ts, so it survives reload); a preview of those kinds fills in as
// soon as a lane sets it and is title-only until then.
import type { PaneTarget } from '../view-state';
import type { Pane } from '../types';

export type PreviewTarget = PaneTarget;

export const targetOf = (pane: Pane): PreviewTarget => ({ ...(pane.file ? { path: pane.file.path } : {}), ...pane.target });

/** "pkg.go.dev/encoding/json" from a full address. */
export function addressOf(url: string): string {
  try {
    const parsed = new URL(url);
    return `${parsed.host}${parsed.pathname === '/' ? '' : parsed.pathname}`;
  } catch {
    return url;
  }
}
