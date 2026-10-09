// "Open the Inbox" from the rail means "show me what has waited longest". The reducer is pure and cannot move focus, so the
// request travels on this tiny channel: Workspace posts it when it sees `open-inbox`, and the Inbox pane answers once it
// is on screen, focusing the oldest question if there is one. With nothing waiting the request is simply spent.
let pending = false;
const listeners = new Set<() => void>();

export const inboxFocus = {
  /** Asks the Inbox to focus its oldest needs-you row the next time it is shown. */
  request() { pending = true; listeners.forEach(listener => listener()); },
  /** True once per request. */
  take(): boolean { const was = pending; pending = false; return was; },
  subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
};
