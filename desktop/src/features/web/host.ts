// The seam between web panes and the workspace that owns tabs and
// conversations. The workspace (integrator-owned) registers a host once; a web
// pane never reaches into the tab model itself.
//
// Without a host, a page's request for a new window opens in the person's
// browser (the link chip's default), and "Start a conversation with this page"
// is absent rather than broken.

import { openUrl } from '../../design/native';
import type { PageContext } from './pageContext';

export type WebHost = {
  /** A page asked for a new window, or a link was opened as a web tab. */
  openWebTab: (url: string, opener?: string) => void;
  /** The person asked to talk about this page: open a focused conversation immediately after `fromPaneId`, with the page attached and unsent. */
  startConversationWithPage?: (page: PageContext, fromPaneId: string) => void;
};

let host: WebHost | null = null;
const listeners = new Set<() => void>();

export function setWebHost(next: WebHost | null): () => void {
  host = next;
  listeners.forEach(listener => listener());
  return () => {
    if (host === next) setWebHost(null);
  };
}

export const webHost = (): WebHost | null => host;

export function subscribeWebHost(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Where a page's new-window request goes. */
export function openFromPage(url: string, opener: string): void {
  if (host) host.openWebTab(url, opener);
  else void openUrl(url);
}
