// The integrator's adapter: one call in Workspace.tsx wires web tabs into the
// tab model. It registers the web host (new web tabs, a conversation about a
// page) and closes the native view of every pane that left the workspace.
//
//   useWorkspaceWeb(state.tabs, dispatch);
//
// A conversation started from a page opens as a new tab with an unsent draft;
// the picture is handed to that tab's composer through `takeOfferedFiles`.
// Nothing is sent and no model is called until the person sends.

import { useEffect, useRef, type Dispatch } from 'react';
import type { Tab, WorkspaceAction } from '../tabs/model';
import { panesOf } from '../tabs/model';
import { siteOf } from './address';
import { setWebHost } from './host';
import { pageAttachment, type PageContext } from './pageContext';
import { reap } from './views';

const offered = new Map<string, File[]>();

/** The composer of a tab opened by "Start a conversation with this page" takes its picture once. */
export function takeOfferedFiles(paneId: string): File[] {
  const files = offered.get(paneId) ?? [];
  offered.delete(paneId);
  return files;
}

/** A new web tab for an address that already passed `toAddress`. */
export function webTab(url: string): Tab {
  return { id: crypto.randomUUID(), kind: 'web', title: siteOf(url), titleSource: 'message', pinned: false, draft: '', target: { url } };
}

export function conversationAbout(page: PageContext): Tab {
  const { draft, files } = pageAttachment(page);
  const tab: Tab = { id: crypto.randomUUID(), kind: 'conversation', title: page.title || siteOf(page.url), titleSource: 'message', pinned: false, draft };
  if (files.length) offered.set(tab.id, files);
  return tab;
}

export function useWorkspaceWeb(tabs: Tab[], dispatch: Dispatch<WorkspaceAction>) {
  const send = useRef(dispatch);
  send.current = dispatch;
  useEffect(() => setWebHost({
    openWebTab: url => send.current({ type: 'open', tab: webTab(url), background: false }),
    startConversationWithPage: page => send.current({ type: 'open', tab: conversationAbout(page), background: false }),
  }), []);
  const live = tabs.flatMap(tab => panesOf(tab)).filter(pane => pane.kind === 'web').map(pane => pane.id).join('\n');
  useEffect(() => { reap(live ? live.split('\n') : []); }, [live]);
}
