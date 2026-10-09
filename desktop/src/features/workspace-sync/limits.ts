// The engine's ceilings on one shared tab set (internal/workspacestore/store.go). A document over them is refused
// with a sentence; checking the size here first spares the request and gives the same sentence offline.
// CHANGE BOTH SIDES TOGETHER.
export const limits = {
  documentBytes: 256 * 1024,
  tabs: 400,
  closed: 50,
  groups: 100,
  panes: 4,
} as const;

/** A window waits this long after a change before saving, so a burst of typing is one write (Places Q-P10 says 500ms). */
export const saveDelayMs = 400;
/** The first wait before trying an unreachable engine again; it doubles to `retryCeilingMs`. */
export const retryFloorMs = 2_000;
export const retryCeilingMs = 30_000;
