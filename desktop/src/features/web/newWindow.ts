// A page's window.open or target=_blank never becomes a native window. The
// native side already denies the popup and emits a typed web://new-tab request
// (opener pane id + url). This turns that request into one background web tab
// placed immediately after the opener, through openWebTab, so the strip — not
// this module — owns tab order and focus.
//
// The design does not say how many a page may open. One page is capped at five
// http(s) opens inside any two seconds, so a script cannot fill the strip.
// A non-http(s) address is dropped and does not count. Nothing is said either way.

import type { Tab } from '../tabs/types.ts';
import { parseAddress } from './model.ts';
import { openWebTab } from './open.ts';

/** How many background tabs one page may add inside NEW_WINDOW_BURST_MS. */
export const NEW_WINDOW_BURST = 5;
/** The window those five share. A request exactly this old still counts. */
export const NEW_WINDOW_BURST_MS = 2000;

/** The typed new-tab request: `opener` is the pane that asked, `url` the address. */
export type PageNewWindow = { opener: string; url: string };

/** Times (ms) each opener's accepted opens landed. Tests pass their own map. */
export type NewWindowGate = { openedAt: Map<string, number[]> };

const shared: NewWindowGate = { openedAt: new Map() };

function allowed(opener: string, now: number, gate: NewWindowGate): boolean {
  const prior = (gate.openedAt.get(opener) ?? []).filter(at => now - at <= NEW_WINDOW_BURST_MS);
  if (prior.length >= NEW_WINDOW_BURST) {
    gate.openedAt.set(opener, prior);
    return false;
  }
  prior.push(now);
  gate.openedAt.set(opener, prior);
  return true;
}

/**
 * Opens one background web tab immediately after `opener`. Returns that tab, or
 * null when there is no opener, the address is not http(s), or this page already
 * opened five in the last two seconds. No native window and no browser open.
 */
export function acceptPageNewWindow(request: PageNewWindow, now: number = Date.now(), gate: NewWindowGate = shared): Tab | null {
  if (!request.opener || !request.url || !parseAddress(request.url)) return null;
  if (!allowed(request.opener, now, gate)) return null;
  return openWebTab(request.url, { background: true, afterId: request.opener });
}
