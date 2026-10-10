// The window's memory of where focus has been. Pure (no React, no storage) so a node test can pin the
// record, back, forward and drop rules. The wire decides when focus actually moved and which tabs are
// still open; this module only keeps the stack.
//
// I2.4 records three moves: a tab change, a place switch, and a drill-in within a tab. Opening a tab
// in the background never took focus, and restoring the window on relaunch is putting back steps that
// were already recorded, so neither of those is a new step. ⌘[ and ⌘] move a cursor. A new step
// forgets whatever was forward of that cursor. The stack holds 100 steps (P-24).

/** The whole stack, back and forward together. A new step drops the oldest back steps once this is passed. */
export const FOCUS_HISTORY_CAP = 100;

/**
 * Why focus moved. `tab`, `place` and `drill` are the three moves I2.4 records.
 * `background` is an open that left the current tab in front. `relaunch` is the window
 * putting its saved tabs back; that restore is not a step.
 */
export type FocusReason = 'tab' | 'place' | 'drill' | 'background' | 'relaunch';

/**
 * Who moved focus. Next up, a notification and a link into another place are not the
 * person's own move, so the step they land on is marked `selfStarted: false` (I2.5).
 */
export type FocusCause = 'own' | 'next-up' | 'notification' | 'cross-place';

/**
 * One scroller's resting place, the same shape tab scroll memory already keeps.
 * `key` names the scroller (`conversation`, `task-page`). An entry with no spots
 * restores nothing: unknown scroll is not a position.
 */
export type FocusScrollSpot = { key: string; top: number; left: number; end: boolean };

/** One step. `drillPath` is root to leaf; an empty path is the tab's own page. */
export type FocusEntry = {
  /** The window's place: `now`, `root`, or a place id. */
  windowPlace: string;
  tabId: string;
  drillPath: readonly string[];
  scroll: readonly FocusScrollSpot[];
  /** Names the composer draft the tab already stores. Null when there is nothing to restore. */
  draftKey: string | null;
  /** False for Next up, a notification, or a cross-place link. */
  selfStarted: boolean;
};

export type FocusHistory = {
  entries: readonly FocusEntry[];
  /** Index of the step focus is on. -1 when the window has no history yet. */
  cursor: number;
};

/**
 * A move the wire observed. Place, tab and drill path are the step's identity and must be
 * the place focus is on now: omitting `drillPath` means the tab's own page, which is a
 * different step from a page drilled into. Omitting `scroll` or `draftKey` on a repeat of
 * the current step keeps the values already stored; a new step stores nothing for an
 * omitted field.
 */
export type FocusMove = {
  reason: FocusReason;
  cause: FocusCause;
  windowPlace: string;
  tabId: string;
  drillPath?: readonly string[];
  scroll?: readonly FocusScrollSpot[];
  draftKey?: string | null;
};

const CAUSES: readonly FocusCause[] = ['own', 'next-up', 'notification', 'cross-place'];

export function emptyFocusHistory(): FocusHistory {
  return { entries: [], cursor: -1 };
}

/** Next up, a notification and a cross-place link are jumps the person did not start. */
export function selfStartedFor(cause: FocusCause): boolean {
  return cause === 'own';
}

function isOffset(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0;
}

function cleanPath(value: readonly string[] | undefined): readonly string[] {
  if (!value) return [];
  return value.filter(step => typeof step === 'string' && step !== '');
}

function pathsEqual(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((step, index) => step === b[index]);
}

/** Drops a spot the window could not restore. A repeated key keeps the later spot. */
function cleanScroll(value: readonly FocusScrollSpot[] | undefined): readonly FocusScrollSpot[] {
  if (!value) return [];
  const byKey = new Map<string, FocusScrollSpot>();
  for (const spot of value) {
    if (!spot || typeof spot.key !== 'string' || spot.key === '') continue;
    if (!isOffset(spot.top) || !isOffset(spot.left) || typeof spot.end !== 'boolean') continue;
    byKey.set(spot.key, { key: spot.key, top: spot.top, left: spot.left, end: spot.end });
  }
  return [...byKey.values()];
}

function cleanDraft(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null;
}

function sameScroll(a: readonly FocusScrollSpot[], b: readonly FocusScrollSpot[]): boolean {
  return a.length === b.length && a.every((spot, index) => {
    const other = b[index];
    return spot.key === other.key && spot.top === other.top && spot.left === other.left && spot.end === other.end;
  });
}

function entryFromMove(move: FocusMove): FocusEntry | undefined {
  if (typeof move.windowPlace !== 'string' || move.windowPlace === '') return undefined;
  if (typeof move.tabId !== 'string' || move.tabId === '') return undefined;
  if (!CAUSES.includes(move.cause)) return undefined;
  return {
    windowPlace: move.windowPlace,
    tabId: move.tabId,
    drillPath: cleanPath(move.drillPath),
    scroll: cleanScroll(move.scroll),
    draftKey: cleanDraft(move.draftKey),
    selfStarted: selfStartedFor(move.cause),
  };
}

function sameFocus(entry: FocusEntry, next: FocusEntry): boolean {
  return entry.windowPlace === next.windowPlace && entry.tabId === next.tabId && pathsEqual(entry.drillPath, next.drillPath);
}

/**
 * Records a move. Background opens and relaunch restores return the same history.
 * Landing again on the current place, tab and drill path refreshes scroll and draft
 * in place, so a scroll note does not become a step and does not drop the forward tail.
 * Any other recorded move truncates that tail, then drops the oldest steps past the cap.
 */
export function recordFocus(history: FocusHistory, move: FocusMove): FocusHistory {
  if (move.reason !== 'tab' && move.reason !== 'place' && move.reason !== 'drill') return history;
  const entry = entryFromMove(move);
  if (!entry) return history;
  const current = history.cursor >= 0 ? history.entries[history.cursor] : undefined;
  if (current && sameFocus(current, entry)) {
    const scroll = move.scroll === undefined ? current.scroll : entry.scroll;
    const draftKey = move.draftKey === undefined ? current.draftKey : entry.draftKey;
    if (sameScroll(current.scroll, scroll) && current.draftKey === draftKey) return history;
    const entries = history.entries.slice();
    entries[history.cursor] = { ...current, scroll, draftKey };
    return { entries, cursor: history.cursor };
  }
  const kept = history.entries.slice(0, history.cursor + 1);
  let entries: FocusEntry[] = [...kept, entry];
  let cursor = entries.length - 1;
  if (entries.length > FOCUS_HISTORY_CAP) {
    const drop = entries.length - FOCUS_HISTORY_CAP;
    entries = entries.slice(drop);
    cursor -= drop;
  }
  return { entries, cursor };
}

/** The nearest live step in `direction`, or the current cursor when every step that way is closed. */
function liveCursor(history: FocusHistory, direction: -1 | 1, aliveTabs: ReadonlySet<string>): number {
  let cursor = history.cursor + direction;
  while (cursor >= 0 && cursor < history.entries.length) {
    if (aliveTabs.has(history.entries[cursor].tabId)) return cursor;
    cursor += direction;
  }
  return history.cursor;
}

function moveCursor(history: FocusHistory, direction: -1 | 1, aliveTabs: ReadonlySet<string>): FocusHistory {
  const cursor = liveCursor(history, direction, aliveTabs);
  if (cursor === history.cursor) return history;
  return { entries: history.entries, cursor };
}

/** One step back, passing over entries whose tab is gone. Those entries stay in the stack. */
export function backFocus(history: FocusHistory, aliveTabs: ReadonlySet<string>): FocusHistory {
  return moveCursor(history, -1, aliveTabs);
}

/** One step forward, with the same skip for a tab that is gone. */
export function forwardFocus(history: FocusHistory, aliveTabs: ReadonlySet<string>): FocusHistory {
  return moveCursor(history, 1, aliveTabs);
}

export function canBackFocus(history: FocusHistory, aliveTabs: ReadonlySet<string>): boolean {
  return liveCursor(history, -1, aliveTabs) !== history.cursor;
}

export function canForwardFocus(history: FocusHistory, aliveTabs: ReadonlySet<string>): boolean {
  return liveCursor(history, 1, aliveTabs) !== history.cursor;
}

/** The step the cursor is on, including one whose tab has since closed. */
export function currentFocus(history: FocusHistory): FocusEntry | undefined {
  return history.cursor >= 0 ? history.entries[history.cursor] : undefined;
}

function cleanStoredEntry(value: unknown): FocusEntry | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const raw = value as Partial<FocusEntry>;
  if (typeof raw.windowPlace !== 'string' || raw.windowPlace === '') return undefined;
  if (typeof raw.tabId !== 'string' || raw.tabId === '') return undefined;
  if (typeof raw.selfStarted !== 'boolean') return undefined;
  if (raw.drillPath !== undefined && !Array.isArray(raw.drillPath)) return undefined;
  return {
    windowPlace: raw.windowPlace,
    tabId: raw.tabId,
    drillPath: cleanPath(Array.isArray(raw.drillPath) ? raw.drillPath : []),
    scroll: cleanScroll(Array.isArray(raw.scroll) ? raw.scroll as FocusScrollSpot[] : []),
    draftKey: cleanDraft(raw.draftKey),
    selfStarted: raw.selfStarted,
  };
}

/**
 * Installs a stack saved with the window. This is the relaunch restore itself, so it
 * replaces whatever is in memory and never appends. A snapshot past the cap keeps the
 * newest 100. Anything that is not a stack is no history.
 */
export function restoreFocusHistory(saved: unknown): FocusHistory {
  if (!saved || typeof saved !== 'object' || Array.isArray(saved)) return emptyFocusHistory();
  const raw = saved as { entries?: unknown; cursor?: unknown };
  if (!Array.isArray(raw.entries)) return emptyFocusHistory();
  let entries = raw.entries.flatMap(item => {
    const entry = cleanStoredEntry(item);
    return entry ? [entry] : [];
  });
  if (entries.length === 0) return emptyFocusHistory();
  let cursor = typeof raw.cursor === 'number' && Number.isInteger(raw.cursor) ? raw.cursor : entries.length - 1;
  if (entries.length > FOCUS_HISTORY_CAP) {
    const drop = entries.length - FOCUS_HISTORY_CAP;
    entries = entries.slice(drop);
    cursor -= drop;
  }
  if (cursor < 0) cursor = 0;
  if (cursor >= entries.length) cursor = entries.length - 1;
  return { entries, cursor };
}
