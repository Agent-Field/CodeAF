// The integrator's adapter: one call in Workspace.tsx wires web tabs into the
// tab model. It registers the web host (new web tabs, a conversation about a
// page) and closes the native view of every pane that left the workspace.
//
//   useWorkspaceWeb(state.tabs, dispatch);
//
// Chat-plus opens a focused conversation immediately after that web tab. The
// page's address and title are an unsent link (`peekOfferedLink`), and the
// picture is handed to that tab's composer once (`peekOfferedFiles`, then
// `settleOfferedFiles` when it attached). Nothing is sent until the person sends.

import { useEffect, useRef, type Dispatch } from 'react';
import type { Tab, WorkspaceAction } from '../tabs/model';
import { panesOf } from '../tabs/model';
import { openUrl } from '../../design/native';
import { nativeWebAvailable } from '../../design/nativeWeb';
import { siteOf } from './address';
import { chatWithPage, type PageLink } from './chatWithPage';
import { setWebHost } from './host';
import { reap } from './views';

const offered = new Map<string, File[]>();
const offeredLinks = new Map<string, PageLink>();

/** The files offered to a pane's composer, left in place: a render may run twice (StrictMode) and must read the same answer. */
export function peekOfferedFiles(paneId: string): File[] {
  return offered.get(paneId) ?? [];
}

/** The composer attached them; the offer is spent and no later mount of the same pane receives it again. */
export function settleOfferedFiles(paneId: string): void {
  offered.delete(paneId);
}

/** Reads and spends an offer in one step (for callers that run exactly once). */
export function takeOfferedFiles(paneId: string): File[] {
  const files = peekOfferedFiles(paneId);
  settleOfferedFiles(paneId);
  return files;
}

/** The page link offered to a pane's composer. A render may run twice and must read the same answer. */
export function peekOfferedLink(paneId: string): PageLink | undefined {
  return offeredLinks.get(paneId);
}

/** The composer showed the link; a later mount of the same pane does not receive it again. */
export function settleOfferedLink(paneId: string): void {
  offeredLinks.delete(paneId);
}

/** A new web tab for an address that already passed `toAddress`. */
export function webTab(url: string): Tab {
  return { id: crypto.randomUUID(), kind: 'web', title: siteOf(url), titleSource: 'message', pinned: false, draft: '', target: { url } };
}

export function useWorkspaceWeb(tabs: Tab[], dispatch: Dispatch<WorkspaceAction>) {
  const send = useRef(dispatch);
  send.current = dispatch;
  const strip = useRef(tabs);
  strip.current = tabs;
  useEffect(() => setWebHost({
    // Without native web no web tab ever opens: the address goes to the default browser instead.
    openWebTab: url => { if (nativeWebAvailable()) send.current({ type: 'open', tab: webTab(url), background: false }); else void openUrl(url); },
    startConversationWithPage: (page, fromPaneId) => {
      const started = chatWithPage(strip.current, fromPaneId, page);
      if (started.files.length) offered.set(started.action.tab.id, started.files);
      offeredLinks.set(started.action.tab.id, started.link);
      send.current(started.action);
    },
  }), []);
  const live = tabs.flatMap(tab => panesOf(tab)).filter(pane => pane.kind === 'web').map(pane => pane.id).join('\n');
  useEffect(() => { reap(live ? live.split('\n') : []); }, [live]);
}
