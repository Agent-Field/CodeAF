// The seam between web panes and the workspace that owns tabs and
// conversations. The workspace (integrator-owned) registers a host once; a web
// pane never reaches into the tab model itself.
//
// "Start a conversation with this page" is absent rather than broken when no
// host is registered. A page's own new window does not use the host: it is a
// background web tab after the opener (newWindow.ts), whether or not a host
// is registered, and it never opens the person's browser.

import type { PageContext } from './pageContext';
import { acceptPageNewWindow } from './newWindow.ts';

export type WebHost = {
  /** Opens an address as a web tab for a caller that holds the host. A page's new window does not use this. */
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

/** A page asked for a new window. The strip opens a background web tab after the opener. */
export function openFromPage(url: string, opener: string): void {
  acceptPageNewWindow({ opener, url });
}
